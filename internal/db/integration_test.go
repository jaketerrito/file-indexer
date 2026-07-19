//go:build integration

package db

import (
	"context"
	"errors"
	"file-indexer/internal/config"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Integration tests require a running Postgres, configured via the same DB_*
// environment variables as config.Load. The -tags=integration build tag is
// the opt-in; once opted in, a missing database is a test failure, not a
// skip. Run them with:
//
//	just test-integration
//
// or against the Tilt dev environment manually:
//
//	export DB_HOST=localhost DB_PORT=5432 DB_USER=postgres \
//	       DB_PASSWORD=mysecretpassword DB_NAME=postgres
//	go test -tags=integration -race ./internal/db/
func testDSN(t *testing.T) string {
	t.Helper()
	if os.Getenv("DB_HOST") == "" {
		t.Fatal("integration tests require DB_HOST to be set (see just test-integration)")
	}
	return config.Load().Database.URL()
}

// testConn runs migrations and returns a connection to the test database.
// Running migrations here keeps the suite hermetic (runnable against a bare
// postgres); under Tilt the migrate Job has already run, making this a no-op,
// and the session lock in RunMigrations makes concurrent runs safe.
func testConn(t *testing.T) *pgx.Conn {
	t.Helper()
	dsn := testDSN(t)

	ctx := context.Background()
	if err := RunMigrations(ctx, "pgx", dsn); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("pgx.Connect: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(context.Background()); err != nil {
			t.Errorf("close conn: %v", err)
		}
	})
	return conn
}

// uniqueKey returns a key that is unique across test runs so tests can run
// repeatedly against a persistent dev database (files.key has a UNIQUE
// constraint).
func uniqueKey(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("it/%s/%d", t.Name(), time.Now().UnixNano())
}

// insertTestFile upserts a file key via the crawler path and returns the id.
func insertTestFile(t *testing.T, conn *pgx.Conn, key string) int64 {
	t.Helper()
	q := New(conn)
	now := time.Now().UTC()
	_, err := q.UpsertFiles(context.Background(), UpsertFilesParams{
		Keys:      []string{key},
		MarkedAts: []pgtype.Timestamptz{{Time: now, Valid: true}},
	})
	if err != nil {
		t.Fatalf("UpsertFiles: %v", err)
	}
	var id int64
	if err := conn.QueryRow(context.Background(), `SELECT id FROM files WHERE key = $1`, key).Scan(&id); err != nil {
		t.Fatalf("get file id for %q: %v", key, err)
	}
	t.Cleanup(func() { _, _ = q.DeleteFile(context.Background(), id) })
	return id
}

// indexTestFile records stat results for a file, making its file_infos row
// fully populated. Seed + complete via production queries.
func indexTestFile(t *testing.T, q *Queries, fileID int64, contentType string, size int64, lastModified time.Time) {
	t.Helper()
	ctx := context.Background()
	if _, err := q.SeedIndexStat(ctx); err != nil {
		t.Fatalf("SeedIndexStat: %v", err)
	}
	if err := q.CompleteIndexStat(ctx, CompleteIndexStatParams{
		FileID:       fileID,
		ContentType:  pgtype.Text{String: contentType, Valid: contentType != ""},
		SizeBytes:    pgtype.Int8{Int64: size, Valid: size != 0},
		LastModified: pgtype.Timestamptz{Time: lastModified, Valid: true},
	}); err != nil {
		t.Fatalf("CompleteIndexStat: %v", err)
	}
}

// createTestFile inserts and stat-indexes a file, returning the file_infos row.
func createTestFile(t *testing.T, conn *pgx.Conn, key string) FileInfo {
	t.Helper()
	id := insertTestFile(t, conn, key)
	indexTestFile(t, New(conn), id, "text/plain", 42, time.Now().UTC())
	file, err := New(conn).GetFile(context.Background(), id)
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	return file
}

func TestRunMigrations(t *testing.T) {
	dsn := testDSN(t)

	// Running migrations twice must be idempotent (goose tracks versions).
	for range 2 {
		if err := RunMigrations(context.Background(), "pgx", dsn); err != nil {
			t.Fatalf("RunMigrations: %v", err)
		}
	}
}

func TestRunMigrationsUnknownDriver(t *testing.T) {
	testDSN(t)

	err := RunMigrations(context.Background(), "no-such-driver", "dsn")
	if err == nil {
		t.Fatal("RunMigrations with unknown driver: want error, got nil")
	}
}

func TestRunMigrationsUnreachableDB(t *testing.T) {
	testDSN(t)

	dsn := "host=127.0.0.1 port=1 user=nobody password=nope dbname=none sslmode=disable connect_timeout=1"
	err := RunMigrations(context.Background(), "pgx", dsn)
	if err == nil {
		t.Fatal("RunMigrations against unreachable db: want error, got nil")
	}
}

func TestCreateAndGetFile(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	key := uniqueKey(t)
	created := createTestFile(t, conn, key)

	if created.ID == 0 {
		t.Error("createTestFile returned zero ID")
	}
	if created.Key != key {
		t.Errorf("Key = %q, want %q", created.Key, key)
	}
	if created.ContentType.String != "text/plain" || !created.ContentType.Valid {
		t.Errorf("ContentType = %+v, want text/plain", created.ContentType)
	}
	if created.SizeBytes.Int64 != 42 || !created.SizeBytes.Valid {
		t.Errorf("SizeBytes = %+v, want 42", created.SizeBytes)
	}

	got, err := q.GetFile(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if got != created {
		t.Errorf("GetFile = %+v, want %+v", got, created)
	}
}

func TestGetFileNotFound(t *testing.T) {
	conn := testConn(t)
	q := New(conn)

	_, err := q.GetFile(context.Background(), -1)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("GetFile(-1) error = %v, want pgx.ErrNoRows", err)
	}
}

func TestGetFilesByIDs(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	a := createTestFile(t, conn, uniqueKey(t)+"-a")
	b := createTestFile(t, conn, uniqueKey(t)+"-b")

	files, err := q.GetFilesByIDs(ctx, []int64{a.ID, b.ID})
	if err != nil {
		t.Fatalf("GetFilesByIDs: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("GetFilesByIDs returned %d files, want 2", len(files))
	}
	got := map[int64]FileInfo{files[0].ID: files[0], files[1].ID: files[1]}
	if got[a.ID] != a || got[b.ID] != b {
		t.Errorf("GetFilesByIDs = %+v, want %+v and %+v", files, a, b)
	}

	// Unknown IDs yield an empty result, not an error.
	none, err := q.GetFilesByIDs(ctx, []int64{-1, -2})
	if err != nil {
		t.Fatalf("GetFilesByIDs(unknown): %v", err)
	}
	if len(none) != 0 {
		t.Errorf("GetFilesByIDs(unknown) returned %d files, want 0", len(none))
	}
}

func TestDeleteFile(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	created := createTestFile(t, conn, uniqueKey(t))

	deleted, err := q.DeleteFile(ctx, created.ID)
	if err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	// DeleteFile returns the files row (identity), not the read model.
	if deleted.ID != created.ID || deleted.Key != created.Key {
		t.Errorf("DeleteFile = %+v, want id %d key %q", deleted, created.ID, created.Key)
	}

	if _, err := q.GetFile(ctx, created.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("GetFile after delete error = %v, want pgx.ErrNoRows", err)
	}

	if _, err := q.DeleteFile(ctx, created.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("DeleteFile(deleted) error = %v, want pgx.ErrNoRows", err)
	}
}

func TestWithTxRollback(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	key := uniqueKey(t)
	now := time.Now().UTC()
	_, err = q.WithTx(tx).UpsertFiles(ctx, UpsertFilesParams{
		Keys:      []string{key},
		MarkedAts: []pgtype.Timestamptz{{Time: now, Valid: true}},
	})
	if err != nil {
		t.Fatalf("UpsertFiles in tx: %v", err)
	}
	// Grab the id within the same transaction.
	var txID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM files WHERE key = $1`, key).Scan(&txID); err != nil {
		t.Fatalf("scan id in tx: %v", err)
	}

	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	if _, err := q.GetFile(ctx, txID); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("GetFile after rollback error = %v, want pgx.ErrNoRows", err)
	}
}
