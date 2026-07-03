package search

import (
	"encoding/base64"
	"encoding/json"
	"file-indexer/internal/db"
	"fmt"
	"time"

	pb "file-indexer/internal/pb/service/v1"

	"github.com/jackc/pgx/v5/pgtype"
)

// cursor is the decoded form of a page token: the sort it was issued for and
// the sort value + id of the last row on the previous page. It is serialized
// as base64url(JSON) and treated as opaque by clients.
type cursor struct {
	SortField pb.SortField `json:"f"`
	SortOrder pb.SortOrder `json:"o"`
	LastID    int64        `json:"id"`
	Key       string       `json:"k,omitempty"`
	CreatedAt time.Time    `json:"c,omitzero"`
	Size      int64        `json:"s,omitempty"`
}

// newCursor captures the keyset position of the last returned row.
func newCursor(sortField pb.SortField, sortOrder pb.SortOrder, last db.File) *cursor {
	c := &cursor{
		SortField: sortField,
		SortOrder: sortOrder,
		LastID:    last.ID,
	}
	switch sortField {
	case pb.SortField_SORT_FIELD_KEY:
		c.Key = last.Key
	case pb.SortField_SORT_FIELD_CREATED_AT:
		c.CreatedAt = last.CreatedAt.Time
	case pb.SortField_SORT_FIELD_SIZE:
		// Matches COALESCE(size_bytes, 0) in the list queries: a NULL size
		// sorts as zero.
		c.Size = last.SizeBytes.Int64
	}
	return c
}

func encodeCursor(c *cursor) string {
	buf, err := json.Marshal(c)
	if err != nil {
		// A cursor of plain scalars cannot fail to marshal.
		panic(fmt.Sprintf("marshal cursor: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func decodeCursor(token string) (*cursor, error) {
	buf, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, fmt.Errorf("decode page token: %w", err)
	}
	c := &cursor{}
	if err := json.Unmarshal(buf, c); err != nil {
		return nil, fmt.Errorf("unmarshal page token: %w", err)
	}
	return c, nil
}

// The accessors below are nil-safe so the query dispatch can pass them
// unconditionally; when the cursor is nil, has_cursor is false and the
// database ignores these values.

func (c *cursor) lastID() int64 {
	if c == nil {
		return 0
	}
	return c.LastID
}

func (c *cursor) lastKey() string {
	if c == nil {
		return ""
	}
	return c.Key
}

func (c *cursor) lastCreatedAt() pgtype.Timestamptz {
	if c == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: c.CreatedAt, Valid: true}
}

func (c *cursor) lastSize() int64 {
	if c == nil {
		return 0
	}
	return c.Size
}
