package search

import (
	"file-indexer/internal/db"
	"testing"
	"time"

	pb "file-indexer/internal/pb/service/v1"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestCursorRoundTripKey(t *testing.T) {
	f := db.FileInfo{ID: 42, Key: "docs/a.txt"}
	c := newCursor(pb.SortField_SORT_FIELD_KEY, pb.SortOrder_SORT_ORDER_DESC, "docs/", "text/plain", f)

	got, err := decodeCursor(encodeCursor(c))
	if err != nil {
		t.Fatalf("decodeCursor: %v", err)
	}
	if got.GetKey() != "docs/a.txt" || got.GetLastId() != 42 {
		t.Errorf("position = (%q, %d), want (docs/a.txt, 42)", got.GetKey(), got.GetLastId())
	}
	if got.GetSortField() != pb.SortField_SORT_FIELD_KEY || got.GetSortOrder() != pb.SortOrder_SORT_ORDER_DESC {
		t.Errorf("sort = (%v, %v), want (KEY, DESC)", got.GetSortField(), got.GetSortOrder())
	}
	if got.GetPrefix() != "docs/" || got.GetContentType() != "text/plain" {
		t.Errorf("filters = (%q, %q), want (docs/, text/plain)", got.GetPrefix(), got.GetContentType())
	}
}

func TestCursorRoundTripLastModified(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	f := db.FileInfo{ID: 7, LastModified: pgtype.Timestamptz{Time: now, Valid: true}}
	c := newCursor(pb.SortField_SORT_FIELD_LAST_MODIFIED, pb.SortOrder_SORT_ORDER_ASC, "", "", f)

	got, err := decodeCursor(encodeCursor(c))
	if err != nil {
		t.Fatalf("decodeCursor: %v", err)
	}
	if !got.GetLastModified().AsTime().Equal(now) || got.GetLastId() != 7 {
		t.Errorf("round trip = %+v, want LastModified=%v LastId=7", got, now)
	}
	ts := lastModifiedCursor(got)
	if !ts.Valid || !ts.Time.Equal(now) {
		t.Errorf("lastModifiedCursor = %+v, want valid %v", ts, now)
	}
}

func TestCursorNullLastModifiedSortsAsEpoch(t *testing.T) {
	f := db.FileInfo{ID: 4} // last_modified NULL: not yet stat-indexed
	c := newCursor(pb.SortField_SORT_FIELD_LAST_MODIFIED, pb.SortOrder_SORT_ORDER_ASC, "", "", f)

	if got := c.GetLastModified().AsTime(); !got.Equal(time.Unix(0, 0)) {
		t.Errorf("LastModified = %v, want epoch for NULL", got)
	}
}

func TestCursorRoundTripSize(t *testing.T) {
	f := db.FileInfo{ID: 9, SizeBytes: pgtype.Int8{Int64: 1234, Valid: true}}
	c := newCursor(pb.SortField_SORT_FIELD_SIZE, pb.SortOrder_SORT_ORDER_ASC, "", "", f)

	got, err := decodeCursor(encodeCursor(c))
	if err != nil {
		t.Fatalf("decodeCursor: %v", err)
	}
	if got.GetSize() != 1234 || got.GetLastId() != 9 {
		t.Errorf("round trip = %+v, want Size=1234 LastId=9", got)
	}
}

func TestCursorNullSizeSortsAsZero(t *testing.T) {
	f := db.FileInfo{ID: 3} // size_bytes NULL
	c := newCursor(pb.SortField_SORT_FIELD_SIZE, pb.SortOrder_SORT_ORDER_ASC, "", "", f)

	if c.GetSize() != 0 {
		t.Errorf("Size = %d, want 0 for NULL size", c.GetSize())
	}
}

func TestCursorNilAccessors(t *testing.T) {
	var c *cursor

	if c.GetLastId() != 0 {
		t.Errorf("GetLastId = %d, want 0", c.GetLastId())
	}
	if c.GetKey() != "" {
		t.Errorf("GetKey = %q, want empty", c.GetKey())
	}
	if c.GetSize() != 0 {
		t.Errorf("GetSize = %d, want 0", c.GetSize())
	}
	if ts := lastModifiedCursor(c); ts.Valid {
		t.Errorf("lastModifiedCursor = %+v, want invalid", ts)
	}
}

func TestDecodeCursorInvalid(t *testing.T) {
	if _, err := decodeCursor("!!!"); err == nil {
		t.Error("decodeCursor(bad base64): want error, got nil")
	}
	// Valid base64 but not a valid PageToken message ("not-proto").
	if _, err := decodeCursor("bm90LXByb3Rv"); err == nil {
		t.Error("decodeCursor(bad proto): want error, got nil")
	}
}
