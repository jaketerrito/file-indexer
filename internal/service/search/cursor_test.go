package search

import (
	"file-indexer/internal/db"
	"testing"
	"time"

	pb "file-indexer/internal/pb/service/v1"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestCursorRoundTripKey(t *testing.T) {
	f := db.File{ID: 42, Key: "docs/a.txt"}
	c := newCursor(pb.SortField_SORT_FIELD_KEY, pb.SortOrder_SORT_ORDER_DESC, f)

	got, err := decodeCursor(encodeCursor(c))
	if err != nil {
		t.Fatalf("decodeCursor: %v", err)
	}
	if *got != *c {
		t.Errorf("round trip = %+v, want %+v", got, c)
	}
	if got.lastKey() != "docs/a.txt" || got.lastID() != 42 {
		t.Errorf("accessors = (%q, %d), want (docs/a.txt, 42)", got.lastKey(), got.lastID())
	}
}

func TestCursorRoundTripCreatedAt(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	f := db.File{ID: 7, CreatedAt: pgtype.Timestamptz{Time: now, Valid: true}}
	c := newCursor(pb.SortField_SORT_FIELD_CREATED_AT, pb.SortOrder_SORT_ORDER_ASC, f)

	got, err := decodeCursor(encodeCursor(c))
	if err != nil {
		t.Fatalf("decodeCursor: %v", err)
	}
	if !got.CreatedAt.Equal(now) || got.lastID() != 7 {
		t.Errorf("round trip = %+v, want CreatedAt=%v LastID=7", got, now)
	}
	ts := got.lastCreatedAt()
	if !ts.Valid || !ts.Time.Equal(now) {
		t.Errorf("lastCreatedAt = %+v, want valid %v", ts, now)
	}
}

func TestCursorRoundTripSize(t *testing.T) {
	f := db.File{ID: 9, SizeBytes: pgtype.Int8{Int64: 1234, Valid: true}}
	c := newCursor(pb.SortField_SORT_FIELD_SIZE, pb.SortOrder_SORT_ORDER_ASC, f)

	got, err := decodeCursor(encodeCursor(c))
	if err != nil {
		t.Fatalf("decodeCursor: %v", err)
	}
	if got.lastSize() != 1234 || got.lastID() != 9 {
		t.Errorf("round trip = %+v, want Size=1234 LastID=9", got)
	}
}

func TestCursorNullSizeSortsAsZero(t *testing.T) {
	f := db.File{ID: 3} // size_bytes NULL
	c := newCursor(pb.SortField_SORT_FIELD_SIZE, pb.SortOrder_SORT_ORDER_ASC, f)

	if c.lastSize() != 0 {
		t.Errorf("lastSize = %d, want 0 for NULL size", c.lastSize())
	}
}

func TestCursorNilAccessors(t *testing.T) {
	var c *cursor

	if c.lastID() != 0 {
		t.Errorf("lastID = %d, want 0", c.lastID())
	}
	if c.lastKey() != "" {
		t.Errorf("lastKey = %q, want empty", c.lastKey())
	}
	if c.lastSize() != 0 {
		t.Errorf("lastSize = %d, want 0", c.lastSize())
	}
	if ts := c.lastCreatedAt(); ts.Valid {
		t.Errorf("lastCreatedAt = %+v, want invalid", ts)
	}
}

func TestDecodeCursorInvalid(t *testing.T) {
	if _, err := decodeCursor("!!!"); err == nil {
		t.Error("decodeCursor(bad base64): want error, got nil")
	}
	// Valid base64 but not JSON.
	if _, err := decodeCursor("bm90LWpzb24"); err == nil {
		t.Error("decodeCursor(bad json): want error, got nil")
	}
}
