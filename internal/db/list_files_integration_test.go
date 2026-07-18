//go:build integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// createListFile inserts a file row with explicit content type, size, and
// created_at so list ordering and filtering can be asserted. A zero size
// stores NULL to exercise the COALESCE(size_bytes, 0) sort behaviour.
func createListFile(t *testing.T, q *Queries, key, contentType string, size int64, createdAt time.Time) File {
	t.Helper()
	ctx := context.Background()

	file, err := q.UpsertFile(ctx, UpsertFileParams{
		Key:         key,
		ContentType: pgtype.Text{String: contentType, Valid: contentType != ""},
		SizeBytes:   pgtype.Int8{Int64: size, Valid: size != 0},
		CreatedAt:   pgtype.Timestamptz{Time: createdAt, Valid: true},
		UpdatedAt:   pgtype.Timestamptz{Time: createdAt, Valid: true},
	})
	if err != nil {
		t.Fatalf("UpsertFile: %v", err)
	}
	t.Cleanup(func() {
		_, _ = q.DeleteFile(context.Background(), file.ID)
	})
	return file
}

// seedListFiles creates a fixed fixture set under a unique prefix so the
// tests are isolated from other rows in a persistent dev database. Returns
// the prefix pattern for the list queries.
func seedListFiles(t *testing.T, q *Queries) string {
	t.Helper()
	prefix := uniqueKey(t) + "/"
	base := time.Now().UTC().Truncate(time.Microsecond)

	createListFile(t, q, prefix+"a.txt", "text/plain", 300, base.Add(2*time.Second))
	createListFile(t, q, prefix+"b.png", "image/png", 100, base.Add(3*time.Second))
	createListFile(t, q, prefix+"c.jpg", "image/jpeg", 200, base.Add(1*time.Second))
	return prefix
}

func keysOf(files []File) []string {
	keys := make([]string, 0, len(files))
	for _, f := range files {
		keys = append(keys, f.Key)
	}
	return keys
}

func assertKeys(t *testing.T, got []File, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d files %v, want %d %v", len(got), keysOf(got), len(want), want)
	}
	for i, k := range want {
		if got[i].Key != k {
			t.Fatalf("files[%d].Key = %q, want %q (all: %v)", i, got[i].Key, k, keysOf(got))
		}
	}
}

func TestListFilesByKeyOrder(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := seedListFiles(t, q)

	asc, err := q.ListFilesByKeyAsc(ctx, ListFilesByKeyAscParams{
		KeyPattern: prefix + "%",
		PageLimit:  10,
	})
	if err != nil {
		t.Fatalf("ListFilesByKeyAsc: %v", err)
	}
	assertKeys(t, asc, prefix+"a.txt", prefix+"b.png", prefix+"c.jpg")

	desc, err := q.ListFilesByKeyDesc(ctx, ListFilesByKeyDescParams{
		KeyPattern: prefix + "%",
		PageLimit:  10,
	})
	if err != nil {
		t.Fatalf("ListFilesByKeyDesc: %v", err)
	}
	assertKeys(t, desc, prefix+"c.jpg", prefix+"b.png", prefix+"a.txt")
}

func TestListFilesByCreatedAtOrder(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := seedListFiles(t, q)

	asc, err := q.ListFilesByCreatedAtAsc(ctx, ListFilesByCreatedAtAscParams{
		KeyPattern: prefix + "%",
		PageLimit:  10,
	})
	if err != nil {
		t.Fatalf("ListFilesByCreatedAtAsc: %v", err)
	}
	assertKeys(t, asc, prefix+"c.jpg", prefix+"a.txt", prefix+"b.png")

	desc, err := q.ListFilesByCreatedAtDesc(ctx, ListFilesByCreatedAtDescParams{
		KeyPattern: prefix + "%",
		PageLimit:  10,
	})
	if err != nil {
		t.Fatalf("ListFilesByCreatedAtDesc: %v", err)
	}
	assertKeys(t, desc, prefix+"b.png", prefix+"a.txt", prefix+"c.jpg")
}

func TestListFilesBySizeOrder(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := seedListFiles(t, q)
	// NULL size must sort as zero: first ascending, last descending.
	now := time.Now().UTC().Truncate(time.Microsecond)
	createListFile(t, q, prefix+"d.bin", "application/octet-stream", 0, now)

	asc, err := q.ListFilesBySizeAsc(ctx, ListFilesBySizeAscParams{
		KeyPattern: prefix + "%",
		PageLimit:  10,
	})
	if err != nil {
		t.Fatalf("ListFilesBySizeAsc: %v", err)
	}
	assertKeys(t, asc, prefix+"d.bin", prefix+"b.png", prefix+"c.jpg", prefix+"a.txt")

	desc, err := q.ListFilesBySizeDesc(ctx, ListFilesBySizeDescParams{
		KeyPattern: prefix + "%",
		PageLimit:  10,
	})
	if err != nil {
		t.Fatalf("ListFilesBySizeDesc: %v", err)
	}
	assertKeys(t, desc, prefix+"a.txt", prefix+"c.jpg", prefix+"b.png", prefix+"d.bin")
}

func TestListFilesContentTypeFilter(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := seedListFiles(t, q)

	// Category prefix pattern matches every image.
	images, err := q.ListFilesByKeyAsc(ctx, ListFilesByKeyAscParams{
		KeyPattern:         prefix + "%",
		ContentTypePattern: "image/%",
		PageLimit:          10,
	})
	if err != nil {
		t.Fatalf("ListFilesByKeyAsc(image/%%): %v", err)
	}
	assertKeys(t, images, prefix+"b.png", prefix+"c.jpg")

	// Exact pattern matches a single type.
	pngs, err := q.ListFilesByKeyAsc(ctx, ListFilesByKeyAscParams{
		KeyPattern:         prefix + "%",
		ContentTypePattern: "image/png",
		PageLimit:          10,
	})
	if err != nil {
		t.Fatalf("ListFilesByKeyAsc(image/png): %v", err)
	}
	assertKeys(t, pngs, prefix+"b.png")

	// Empty pattern disables the filter.
	all, err := q.ListFilesByKeyAsc(ctx, ListFilesByKeyAscParams{
		KeyPattern:         prefix + "%",
		ContentTypePattern: "",
		PageLimit:          10,
	})
	if err != nil {
		t.Fatalf("ListFilesByKeyAsc(no filter): %v", err)
	}
	if len(all) != 3 {
		t.Errorf("got %d files, want 3: %v", len(all), keysOf(all))
	}
}

func TestListFilesPrefixIsolation(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := seedListFiles(t, q)
	other := uniqueKey(t) + "-other/"
	createListFile(t, q, other+"x.txt", "text/plain", 1, time.Now().UTC())

	files, err := q.ListFilesByKeyAsc(ctx, ListFilesByKeyAscParams{
		KeyPattern: prefix + "%",
		PageLimit:  10,
	})
	if err != nil {
		t.Fatalf("ListFilesByKeyAsc: %v", err)
	}
	for _, f := range files {
		if f.Key == other+"x.txt" {
			t.Errorf("prefix filter leaked file %q", f.Key)
		}
	}
	if len(files) != 3 {
		t.Errorf("got %d files, want 3: %v", len(files), keysOf(files))
	}
}

func TestListFilesKeysetPagination(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := seedListFiles(t, q)

	// Walk key-ascending two at a time; pages must not overlap or skip.
	page1, err := q.ListFilesByKeyAsc(ctx, ListFilesByKeyAscParams{
		KeyPattern: prefix + "%",
		PageLimit:  2,
	})
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	assertKeys(t, page1, prefix+"a.txt", prefix+"b.png")

	last := page1[len(page1)-1]
	page2, err := q.ListFilesByKeyAsc(ctx, ListFilesByKeyAscParams{
		KeyPattern: prefix + "%",
		HasCursor:  true,
		LastKey:    last.Key,
		LastID:     last.ID,
		PageLimit:  2,
	})
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	assertKeys(t, page2, prefix+"c.jpg")
}

func TestListFilesKeysetPaginationDescBySize(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := seedListFiles(t, q)

	page1, err := q.ListFilesBySizeDesc(ctx, ListFilesBySizeDescParams{
		KeyPattern: prefix + "%",
		PageLimit:  2,
	})
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	assertKeys(t, page1, prefix+"a.txt", prefix+"c.jpg")

	last := page1[len(page1)-1]
	page2, err := q.ListFilesBySizeDesc(ctx, ListFilesBySizeDescParams{
		KeyPattern: prefix + "%",
		HasCursor:  true,
		LastSize:   last.SizeBytes.Int64,
		LastID:     last.ID,
		PageLimit:  2,
	})
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	assertKeys(t, page2, prefix+"b.png")
}
