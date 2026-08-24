package search

import (
	"encoding/base64"
	"file-indexer/internal/db"
	"fmt"
	"time"

	cursorv1 "file-indexer/internal/pb/cursor/v1"
	pb "file-indexer/internal/pb/service/v1"

	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// cursor is the decoded form of a page token: the query it was issued for
// (sort and filters) and the sort value + id of the last row on the previous
// page. Per AIP-158 it is serialized as base64url(proto) and treated as
// opaque by clients.
type cursor = cursorv1.PageToken

// newCursor captures the query being paginated and the keyset position of
// the last returned row.
func newCursor(sortField pb.SortField, sortOrder pb.SortOrder, prefix, contentType string, last db.FileInfo) *cursor {
	c := &cursor{
		SortField:   sortField,
		SortOrder:   sortOrder,
		LastId:      last.ID,
		Prefix:      prefix,
		ContentType: contentType,
	}
	setSortValue(c, sortField, last)
	return c
}

// newDirFilesCursor is newCursor's ListDirectory counterpart: it records
// path/phase instead of prefix/content_type (see PageToken in cursor.proto).
// last may be the zero db.FileInfo — LastId 0 then signals "no file cursor
// yet, resume at the start of the files phase" (see listFiles' hasCursor).
func newDirFilesCursor(sortField pb.SortField, sortOrder pb.SortOrder, path string, last db.FileInfo) *cursor {
	c := &cursor{
		SortField: sortField,
		SortOrder: sortOrder,
		LastId:    last.ID,
		Path:      path,
		Phase:     cursorv1.ListPhase_LIST_PHASE_FILES,
	}
	setSortValue(c, sortField, last)
	return c
}

// newDirCursor records a ListDirectory page that stopped mid-way through the
// directories phase: lastDir is the last child prefix returned, which
// skipPastChild uses to resume the loose index scan past it.
func newDirCursor(sortField pb.SortField, sortOrder pb.SortOrder, path, lastDir string) *cursor {
	return &cursor{
		SortField: sortField,
		SortOrder: sortOrder,
		Path:      path,
		Phase:     cursorv1.ListPhase_LIST_PHASE_DIRECTORIES,
		LastDir:   lastDir,
	}
}

// setSortValue sets the cursor field matching sortField to last's value in
// that sort order, shared by newCursor and newDirFilesCursor.
func setSortValue(c *cursor, sortField pb.SortField, last db.FileInfo) {
	switch sortField {
	case pb.SortField_SORT_FIELD_KEY:
		c.Key = last.Key
	case pb.SortField_SORT_FIELD_LAST_MODIFIED:
		// Matches COALESCE(last_modified, 'epoch') in the list queries: a
		// file not yet stat-indexed sorts at the epoch.
		c.LastModified = timestamppb.New(coalesceLastModified(last))
	case pb.SortField_SORT_FIELD_SIZE:
		// Matches COALESCE(size_bytes, 0) in the list queries: a NULL size
		// sorts as zero.
		c.Size = last.SizeBytes.Int64
	}
}

// coalesceLastModified mirrors the list queries' COALESCE(last_modified,
// 'epoch'::timestamptz) so cursor comparisons see the same sort value the
// database used.
func coalesceLastModified(f db.FileInfo) time.Time {
	if f.LastModified.Valid {
		return f.LastModified.Time
	}
	return time.Unix(0, 0).UTC()
}

func encodeCursor(c *cursor) string {
	buf, err := proto.Marshal(c)
	if err != nil {
		// A message of plain scalars cannot fail to marshal.
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
	if err := proto.Unmarshal(buf, c); err != nil {
		return nil, fmt.Errorf("unmarshal page token: %w", err)
	}
	return c, nil
}

// lastModifiedCursor adapts the cursor's timestamp for the sqlc params. Like
// the generated proto getters it is nil-safe so the query dispatch can pass
// it unconditionally; when the cursor is nil, has_cursor is false and the
// database ignores the value.
func lastModifiedCursor(c *cursor) pgtype.Timestamptz {
	if c == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: c.GetLastModified().AsTime(), Valid: true}
}
