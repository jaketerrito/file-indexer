//go:build integration

package db

import (
	"context"
	"slices"
	"testing"
)

// TestUpsertAndListIndexPreviewKeys round-trips preview result rows and
// exercises ListIndexPreviewKeys, the query preview-gc set-diffs a bucket
// listing against (see internal/service/previewgc): a listed key absent
// here is an orphan and is deleted from storage. The indexer package's own
// integration tests cover the Upsert path only as a side effect of the
// queue flow; this covers the List query and the ON CONFLICT key swap
// directly.
func TestUpsertAndListIndexPreviewKeys(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	fileA := insertTestFile(t, conn, uniqueKey(t))
	fileB := insertTestFile(t, conn, uniqueKey(t))
	keyA := uniqueKey(t)
	keyB := uniqueKey(t)

	if err := q.UpsertIndexPreviewResult(ctx, UpsertIndexPreviewResultParams{
		FileID: fileA, PreviewKey: keyA, Width: 320, Height: 240,
	}); err != nil {
		t.Fatalf("UpsertIndexPreviewResult (a): %v", err)
	}
	if err := q.UpsertIndexPreviewResult(ctx, UpsertIndexPreviewResultParams{
		FileID: fileB, PreviewKey: keyB, Width: 160, Height: 120,
	}); err != nil {
		t.Fatalf("UpsertIndexPreviewResult (b): %v", err)
	}

	keys, err := q.ListIndexPreviewKeys(ctx)
	if err != nil {
		t.Fatalf("ListIndexPreviewKeys: %v", err)
	}
	if !slices.Contains(keys, keyA) || !slices.Contains(keys, keyB) {
		t.Errorf("ListIndexPreviewKeys missing fixture keys %q, %q (got %v)", keyA, keyB, keys)
	}

	// Re-upserting must swap the key in place (ON CONFLICT DO UPDATE), not
	// duplicate the row.
	keyA2 := uniqueKey(t)
	if err := q.UpsertIndexPreviewResult(ctx, UpsertIndexPreviewResultParams{
		FileID: fileA, PreviewKey: keyA2, Width: 640, Height: 480,
	}); err != nil {
		t.Fatalf("UpsertIndexPreviewResult (re-upsert): %v", err)
	}
	keys, err = q.ListIndexPreviewKeys(ctx)
	if err != nil {
		t.Fatalf("ListIndexPreviewKeys after re-upsert: %v", err)
	}
	if !slices.Contains(keys, keyA2) {
		t.Errorf("ListIndexPreviewKeys missing re-upserted key %q (got %v)", keyA2, keys)
	}
	if slices.Contains(keys, keyA) {
		t.Errorf("ListIndexPreviewKeys still lists superseded key %q (got %v)", keyA, keys)
	}
}
