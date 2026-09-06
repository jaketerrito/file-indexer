//go:build integration

package search

import (
	"context"
	"file-indexer/internal/db"
	"file-indexer/internal/db/dbtest"
	pb "file-indexer/internal/pb/service/v1"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// These exercise SearchService.ListFiles over a real gRPC server backed by
// a real Postgres, covering the parts unit tests (which mock the FileIndex
// interface) cannot: cursor encode/decode, sort/page-size normalization, and
// LIKE-pattern construction meeting the real sqlc queries on a real wire.
// See internal/db's integration suite for the underlying SQL query behavior
// (keyset pagination, sort tie-breaking, category content-type patterns).
//
// Run via `just test-integration` (Postgres must be reachable; the recipe
// port-forwards to this checkout's Tilt stack unless DB_HOST is set).
//
// TestMain gives this package its own temporary database (see
// internal/db/dbtest); fixtures still use unique key prefixes so the file
// can also run repeatedly against a persistent dev database.
func TestMain(m *testing.M) {
	os.Exit(dbtest.Main(m, db.RunMigrations))
}

// testPool returns a pgxpool.Pool to the test database. Migrations already
// ran once in TestMain.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), dbtest.DSN())
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// uniqueKey returns a key that is unique across test runs so tests can run
// repeatedly against a persistent dev database (files.key has a UNIQUE
// constraint).
func uniqueKey(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("it/%s/%d", t.Name(), time.Now().UnixNano())
}

// createListFile inserts a file and stat-indexes it with explicit content
// type, size, and last-modified so list ordering and filtering can be
// asserted, using the production write paths (crawler insert + worker
// complete) — the same fixture sequence as internal/db's integration tests,
// over a pool. The queue seed+complete mirrors production state even though
// the file_infos LEFT JOIN only needs the stat row.
func createListFile(t *testing.T, pool *pgxpool.Pool, key, contentType string, size int64, lastModified time.Time) {
	t.Helper()
	ctx := context.Background()
	q := db.New(pool)
	if _, err := q.UpsertFiles(ctx, db.UpsertFilesParams{
		Keys:      []string{key},
		MarkedAts: []pgtype.Timestamptz{{Time: time.Now().UTC(), Valid: true}},
	}); err != nil {
		t.Fatalf("UpsertFiles: %v", err)
	}
	var id int64
	if err := pool.QueryRow(ctx, `SELECT id FROM files WHERE key = $1`, key).Scan(&id); err != nil {
		t.Fatalf("get file id for %q: %v", key, err)
	}
	// Queue status flip and result write are a transaction in production
	// (indexer.PGQueue.Complete) but run back-to-back here since nothing
	// else is racing them.
	if _, err := q.SeedIndexQueue(ctx, "stat"); err != nil {
		t.Fatalf("SeedIndexQueue: %v", err)
	}
	if err := q.CompleteIndexQueue(ctx, db.CompleteIndexQueueParams{IndexType: "stat", FileID: id}); err != nil {
		t.Fatalf("CompleteIndexQueue: %v", err)
	}
	if err := q.UpsertIndexStatResult(ctx, db.UpsertIndexStatResultParams{
		FileID:       id,
		ContentType:  contentType,
		SizeBytes:    size,
		LastModified: pgtype.Timestamptz{Time: lastModified, Valid: true},
	}); err != nil {
		t.Fatalf("UpsertIndexStatResult: %v", err)
	}
	t.Cleanup(func() { _, _ = q.DeleteFile(context.Background(), id) })
}

// seedListFiles creates a fixed fixture set under a unique prefix so the
// tests are isolated from other rows in a persistent dev database. Returns
// the prefix for the list requests.
func seedListFiles(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	prefix := uniqueKey(t) + "/"
	// last_modified offsets keep the fixture ordering distinct.
	base := time.Now().UTC().Truncate(time.Second)

	createListFile(t, pool, prefix+"a.txt", "text/plain", 300, base.Add(2*time.Second))
	createListFile(t, pool, prefix+"b.png", "image/png", 100, base.Add(3*time.Second))
	createListFile(t, pool, prefix+"c.jpg", "image/jpeg", 200, base.Add(1*time.Second))
	return prefix
}

// searchClient serves a real SearchServer (the same composition as
// cmd/search/main.go) on an ephemeral port and returns a client connected
// to it. Serve has no shutdown; the leaked goroutine dies with the test
// binary, same as TestServe. Callers must pass grpc.WaitForReady(true) on
// calls since the server starts asynchronously.
func searchClient(t *testing.T, pool *pgxpool.Pool) pb.SearchServiceClient {
	t.Helper()
	addr := freeAddr(t)
	srv := New(addr, db.New(pool))
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve() }()
	t.Cleanup(func() {
		select {
		case err := <-errCh:
			t.Errorf("Serve exited unexpectedly: %v", err)
		default:
		}
	})

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close conn: %v", err)
		}
	})
	return pb.NewSearchServiceClient(conn)
}

// assertInfoKeys is db's assertKeys over the proto shape.
func assertInfoKeys(t *testing.T, files []*pb.FileInfo, want ...string) {
	t.Helper()
	keys := make([]string, 0, len(files))
	for _, f := range files {
		keys = append(keys, f.GetKey())
	}
	if len(files) != len(want) {
		t.Fatalf("got %d files %v, want %d %v", len(files), keys, len(want), want)
	}
	for i, k := range want {
		if keys[i] != k {
			t.Fatalf("files[%d].Key = %q, want %q (all: %v)", i, keys[i], k, keys)
		}
	}
}

// mustListFiles calls ListFiles over the wire and fails on any error.
func mustListFiles(t *testing.T, client pb.SearchServiceClient, req *pb.ListFilesRequest) *pb.ListFilesResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := client.ListFiles(ctx, req, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("ListFiles(%+v): %v", req, err)
	}
	return resp
}

// listFilesErr calls ListFiles expecting a gRPC status error.
func listFilesErr(t *testing.T, client pb.SearchServiceClient, req *pb.ListFilesRequest) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := client.ListFiles(ctx, req, grpc.WaitForReady(true))
	if err == nil {
		t.Fatalf("ListFiles(%+v): want error, got nil", req)
	}
	return err
}

func TestListFilesIntegrationSorting(t *testing.T) {
	pool := testPool(t)
	prefix := seedListFiles(t, pool)
	client := searchClient(t, pool)

	// Unspecified sort field/order defaults to KEY/ASC end-to-end.
	resp := mustListFiles(t, client, &pb.ListFilesRequest{Prefix: prefix})
	assertInfoKeys(t, resp.GetFiles(), prefix+"a.txt", prefix+"b.png", prefix+"c.jpg")

	// The proto mapping lands from real columns: stat results populate the
	// metadata fields, and preview status derives from the content type
	// (no preview queue rows exist).
	first := resp.GetFiles()[0]
	if got := first.GetContentType(); got != "text/plain" {
		t.Errorf("files[0].ContentType = %q, want %q", got, "text/plain")
	}
	if got := first.GetSizeBytes(); got != 300 {
		t.Errorf("files[0].SizeBytes = %d, want 300", got)
	}
	if first.GetUpdatedAt() == nil {
		t.Error("files[0].UpdatedAt = nil, want the stat last-modified timestamp")
	}
	wantStatus := map[string]pb.PreviewStatus{
		prefix + "a.txt": pb.PreviewStatus_PREVIEW_STATUS_NONE,
		prefix + "b.png": pb.PreviewStatus_PREVIEW_STATUS_PENDING,
		prefix + "c.jpg": pb.PreviewStatus_PREVIEW_STATUS_PENDING,
	}
	for _, f := range resp.GetFiles() {
		if got, want := f.GetPreviewStatus(), wantStatus[f.GetKey()]; got != want {
			t.Errorf("%s PreviewStatus = %v, want %v", f.GetKey(), got, want)
		}
	}

	// Size descending: 300, 200, 100.
	resp = mustListFiles(t, client, &pb.ListFilesRequest{
		Prefix:    prefix,
		SortField: pb.SortField_SORT_FIELD_SIZE,
		SortOrder: pb.SortOrder_SORT_ORDER_DESC,
	})
	assertInfoKeys(t, resp.GetFiles(), prefix+"a.txt", prefix+"c.jpg", prefix+"b.png")

	// Last-modified ascending: base+1s, base+2s, base+3s.
	resp = mustListFiles(t, client, &pb.ListFilesRequest{
		Prefix:    prefix,
		SortField: pb.SortField_SORT_FIELD_LAST_MODIFIED,
		SortOrder: pb.SortOrder_SORT_ORDER_ASC,
	})
	assertInfoKeys(t, resp.GetFiles(), prefix+"c.jpg", prefix+"a.txt", prefix+"b.png")
}

func TestListFilesIntegrationPaging(t *testing.T) {
	pool := testPool(t)
	prefix := seedListFiles(t, pool)
	client := searchClient(t, pool)

	page1 := mustListFiles(t, client, &pb.ListFilesRequest{Prefix: prefix, PageSize: 2})
	assertInfoKeys(t, page1.GetFiles(), prefix+"a.txt", prefix+"b.png")
	if page1.GetNextPageToken() == "" {
		t.Fatal("page 1 NextPageToken is empty, want a token for page 2")
	}

	page2 := mustListFiles(t, client, &pb.ListFilesRequest{
		Prefix:    prefix,
		PageSize:  2,
		PageToken: page1.GetNextPageToken(),
	})
	assertInfoKeys(t, page2.GetFiles(), prefix+"c.jpg")
	if page2.GetNextPageToken() != "" {
		t.Errorf("page 2 NextPageToken = %q, want empty", page2.GetNextPageToken())
	}

	// AIP-158: reusing a token with different arguments (other than
	// page_size/page_token) is invalid — here over the wire, not just
	// against decodeCursor.
	err := listFilesErr(t, client, &pb.ListFilesRequest{
		Prefix:    uniqueKey(t) + "/",
		PageSize:  2,
		PageToken: page1.GetNextPageToken(),
	})
	if code := status.Code(err); code != codes.InvalidArgument {
		t.Errorf("mismatched token: code = %v, want InvalidArgument (err=%v)", code, err)
	}

	err = listFilesErr(t, client, &pb.ListFilesRequest{Prefix: prefix, PageToken: "not-a-token"})
	if code := status.Code(err); code != codes.InvalidArgument {
		t.Errorf("garbage token: code = %v, want InvalidArgument (err=%v)", code, err)
	}
}

func TestListFilesIntegrationFilters(t *testing.T) {
	pool := testPool(t)
	prefix := seedListFiles(t, pool)
	// A stray file under a different prefix must not leak into the results.
	createListFile(t, pool, uniqueKey(t)+"/stray.txt", "text/plain", 1, time.Now().UTC())
	client := searchClient(t, pool)

	resp := mustListFiles(t, client, &pb.ListFilesRequest{Prefix: prefix})
	assertInfoKeys(t, resp.GetFiles(), prefix+"a.txt", prefix+"b.png", prefix+"c.jpg")

	// A category ("image/") is a prefix match.
	resp = mustListFiles(t, client, &pb.ListFilesRequest{Prefix: prefix, ContentType: "image/"})
	assertInfoKeys(t, resp.GetFiles(), prefix+"b.png", prefix+"c.jpg")

	// Anything else matches exactly.
	resp = mustListFiles(t, client, &pb.ListFilesRequest{Prefix: prefix, ContentType: "image/png"})
	assertInfoKeys(t, resp.GetFiles(), prefix+"b.png")
}
