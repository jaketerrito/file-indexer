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

// createTestFile inserts a file row and registers cleanup to remove it.
func createTestFile(t *testing.T, q *Queries, key string) File {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	file, err := q.UpsertFile(ctx, UpsertFileParams{
		Key:         key,
		ContentType: pgtype.Text{String: "text/plain", Valid: true},
		SizeBytes:   pgtype.Int8{Int64: 42, Valid: true},
		CreatedAt:   pgtype.Timestamptz{Time: now, Valid: true},
		UpdatedAt:   pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		t.Fatalf("UpsertFile: %v", err)
	}
	t.Cleanup(func() {
		// Best effort: the row may already be deleted by the test itself.
		_, _ = q.DeleteFile(context.Background(), file.ID)
	})
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
	created := createTestFile(t, q, key)

	if created.ID == 0 {
		t.Error("UpsertFile returned zero ID")
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

func TestUpsertFileDuplicateKey(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	key := uniqueKey(t)
	created := createTestFile(t, q, key)

	// Re-indexing the same key must update in place, not error.
	later := time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour)
	updated, err := q.UpsertFile(ctx, UpsertFileParams{
		Key:         key,
		ContentType: pgtype.Text{String: "image/png", Valid: true},
		SizeBytes:   pgtype.Int8{Int64: 99, Valid: true},
		CreatedAt:   pgtype.Timestamptz{Time: later, Valid: true},
		UpdatedAt:   pgtype.Timestamptz{Time: later, Valid: true},
	})
	if err != nil {
		t.Fatalf("UpsertFile with duplicate key: %v", err)
	}
	if updated.ID != created.ID {
		t.Errorf("ID = %d, want %d (same row updated)", updated.ID, created.ID)
	}
	if updated.ContentType.String != "image/png" || !updated.ContentType.Valid {
		t.Errorf("ContentType = %+v, want image/png", updated.ContentType)
	}
	if updated.SizeBytes.Int64 != 99 || !updated.SizeBytes.Valid {
		t.Errorf("SizeBytes = %+v, want 99", updated.SizeBytes)
	}
	if !updated.UpdatedAt.Time.Equal(later) {
		t.Errorf("UpdatedAt = %v, want %v", updated.UpdatedAt.Time, later)
	}
	if !updated.CreatedAt.Time.Equal(created.CreatedAt.Time) {
		t.Errorf("CreatedAt = %v, want original %v (preserved on conflict)", updated.CreatedAt.Time, created.CreatedAt.Time)
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

	a := createTestFile(t, q, uniqueKey(t)+"-a")
	b := createTestFile(t, q, uniqueKey(t)+"-b")

	files, err := q.GetFilesByIDs(ctx, []int64{a.ID, b.ID})
	if err != nil {
		t.Fatalf("GetFilesByIDs: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("GetFilesByIDs returned %d files, want 2", len(files))
	}
	got := map[int64]File{files[0].ID: files[0], files[1].ID: files[1]}
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

	created := createTestFile(t, q, uniqueKey(t))

	deleted, err := q.DeleteFile(ctx, created.ID)
	if err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	if deleted != created {
		t.Errorf("DeleteFile = %+v, want %+v", deleted, created)
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
	now := time.Now().UTC().Truncate(time.Microsecond)
	created, err := q.WithTx(tx).UpsertFile(ctx, UpsertFileParams{
		Key:       key,
		CreatedAt: pgtype.Timestamptz{Time: now, Valid: true},
		UpdatedAt: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		t.Fatalf("UpsertFile in tx: %v", err)
	}

	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	if _, err := q.GetFile(ctx, created.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("GetFile after rollback error = %v, want pgx.ErrNoRows", err)
	}
}
