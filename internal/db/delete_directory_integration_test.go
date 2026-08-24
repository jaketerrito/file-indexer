//go:build integration

package db

import (
	"context"
	"testing"
	"time"
)

func TestGetDirectoryStats(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := seedListFiles(t, conn)

	stats, err := q.GetDirectoryStats(ctx, prefix+"%")
	if err != nil {
		t.Fatalf("GetDirectoryStats: %v", err)
	}
	// seedListFiles: a.txt=300, b.png=100, c.jpg=200.
	if stats.FileCount != 3 {
		t.Errorf("FileCount = %d, want 3", stats.FileCount)
	}
	if stats.TotalBytes != 600 {
		t.Errorf("TotalBytes = %d, want 600", stats.TotalBytes)
	}
}

func TestGetDirectoryStatsEmpty(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := uniqueKey(t) + "/"

	stats, err := q.GetDirectoryStats(ctx, prefix+"%")
	if err != nil {
		t.Fatalf("GetDirectoryStats: %v", err)
	}
	if stats.FileCount != 0 || stats.TotalBytes != 0 {
		t.Errorf("stats = %+v, want zero for an empty directory", stats)
	}
}

func TestGetDirectoryStatsCountsUnindexedFiles(t *testing.T) {
	// A file with no index_stat_result row (not yet stat-indexed) must
	// still count toward file_count, contributing 0 to total_bytes via
	// COALESCE — same absent-row convention as the ListFilesBy* queries.
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := uniqueKey(t) + "/"
	insertTestFile(t, conn, prefix+"unindexed.txt")

	stats, err := q.GetDirectoryStats(ctx, prefix+"%")
	if err != nil {
		t.Fatalf("GetDirectoryStats: %v", err)
	}
	if stats.FileCount != 1 {
		t.Errorf("FileCount = %d, want 1", stats.FileCount)
	}
	if stats.TotalBytes != 0 {
		t.Errorf("TotalBytes = %d, want 0", stats.TotalBytes)
	}
}

func TestListFilesForDelete(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := seedListFiles(t, conn)

	rows, err := q.ListFilesForDelete(ctx, ListFilesForDeleteParams{
		KeyPattern: prefix + "%",
		PageLimit:  10,
	})
	if err != nil {
		t.Fatalf("ListFilesForDelete: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	// Ordered by id, not key: ids were assigned in insertion order
	// (a.txt, b.png, c.jpg).
	if rows[0].Key != prefix+"a.txt" || rows[1].Key != prefix+"b.png" || rows[2].Key != prefix+"c.jpg" {
		t.Errorf("keys = %v, want insertion order", []string{rows[0].Key, rows[1].Key, rows[2].Key})
	}
}

func TestListFilesForDeleteKeysetPagination(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := seedListFiles(t, conn)

	page1, err := q.ListFilesForDelete(ctx, ListFilesForDeleteParams{
		KeyPattern: prefix + "%",
		PageLimit:  2,
	})
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page 1: got %d rows, want 2", len(page1))
	}

	page2, err := q.ListFilesForDelete(ctx, ListFilesForDeleteParams{
		KeyPattern: prefix + "%",
		HasCursor:  true,
		LastID:     page1[len(page1)-1].ID,
		PageLimit:  2,
	})
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(page2) != 1 {
		t.Fatalf("page 2: got %d rows, want 1", len(page2))
	}
	if page1[len(page1)-1].ID >= page2[0].ID {
		t.Errorf("pages not monotonic by id: page1 last=%d, page2 first=%d", page1[len(page1)-1].ID, page2[0].ID)
	}
}

func TestListFilesForDeleteIncludesPreviewKey(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := uniqueKey(t) + "/"
	id := insertTestFile(t, conn, prefix+"photo.jpg")
	indexTestFile(t, q, id, "image/jpeg", 100, time.Now().UTC())
	if _, err := q.SeedIndexQueue(ctx, "preview"); err != nil {
		t.Fatalf("SeedIndexQueue: %v", err)
	}
	if err := q.CompleteIndexQueue(ctx, CompleteIndexQueueParams{IndexType: "preview", FileID: id}); err != nil {
		t.Fatalf("CompleteIndexQueue: %v", err)
	}
	if err := q.UpsertIndexPreviewResult(ctx, UpsertIndexPreviewResultParams{
		FileID:     id,
		PreviewKey: ".index/previews/" + prefix,
		Width:      320,
		Height:     160,
	}); err != nil {
		t.Fatalf("UpsertIndexPreviewResult: %v", err)
	}

	rows, err := q.ListFilesForDelete(ctx, ListFilesForDeleteParams{
		KeyPattern: prefix + "%",
		PageLimit:  10,
	})
	if err != nil {
		t.Fatalf("ListFilesForDelete: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if !rows[0].PreviewKey.Valid || rows[0].PreviewKey.String != ".index/previews/"+prefix {
		t.Errorf("PreviewKey = %+v, want valid %q", rows[0].PreviewKey, ".index/previews/"+prefix)
	}
}

func TestDeleteFilesByIDs(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := seedListFiles(t, conn)

	rows, err := q.ListFilesForDelete(ctx, ListFilesForDeleteParams{
		KeyPattern: prefix + "%",
		PageLimit:  10,
	})
	if err != nil {
		t.Fatalf("ListFilesForDelete: %v", err)
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}

	n, err := q.DeleteFilesByIDs(ctx, ids)
	if err != nil {
		t.Fatalf("DeleteFilesByIDs: %v", err)
	}
	if n != 3 {
		t.Errorf("DeleteFilesByIDs rows affected = %d, want 3", n)
	}

	remaining, err := q.ListFilesForDelete(ctx, ListFilesForDeleteParams{
		KeyPattern: prefix + "%",
		PageLimit:  10,
	})
	if err != nil {
		t.Fatalf("ListFilesForDelete after delete: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("got %d rows after delete, want 0", len(remaining))
	}
}

func TestDeleteFilesByIDsPartialSet(t *testing.T) {
	// Only the ids passed are removed; siblings under the same prefix
	// survive.
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := seedListFiles(t, conn)

	rows, err := q.ListFilesForDelete(ctx, ListFilesForDeleteParams{
		KeyPattern: prefix + "%",
		PageLimit:  10,
	})
	if err != nil {
		t.Fatalf("ListFilesForDelete: %v", err)
	}

	n, err := q.DeleteFilesByIDs(ctx, []int64{rows[0].ID})
	if err != nil {
		t.Fatalf("DeleteFilesByIDs: %v", err)
	}
	if n != 1 {
		t.Errorf("rows affected = %d, want 1", n)
	}

	remaining, err := q.ListFilesForDelete(ctx, ListFilesForDeleteParams{
		KeyPattern: prefix + "%",
		PageLimit:  10,
	})
	if err != nil {
		t.Fatalf("ListFilesForDelete after delete: %v", err)
	}
	if len(remaining) != 2 {
		t.Errorf("got %d rows after partial delete, want 2", len(remaining))
	}
}

func TestDeleteFilesByIDsEmpty(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	n, err := q.DeleteFilesByIDs(ctx, nil)
	if err != nil {
		t.Fatalf("DeleteFilesByIDs(nil): %v", err)
	}
	if n != 0 {
		t.Errorf("rows affected = %d, want 0", n)
	}
}
