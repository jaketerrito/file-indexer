// Package search implements the SearchService gRPC API: listing indexed
// files with prefix and content-type filters, configurable sorting, and
// keyset pagination via opaque page tokens.
package search

import (
	"context"
	"file-indexer/internal/db"
	cursorv1 "file-indexer/internal/pb/cursor/v1"
	pb "file-indexer/internal/pb/service/v1"
	"log/slog"
	"net"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	defaultPageSize = 50
	maxPageSize     = 200
)

// FileIndex is the database surface the search service depends on: one list
// query per (sort field, direction), all keyset-paginated on (value, id),
// plus ListChildDirectories for directory browsing.
type FileIndex interface {
	ListFilesByKeyAsc(ctx context.Context, arg db.ListFilesByKeyAscParams) ([]db.FileInfo, error)
	ListFilesByKeyDesc(ctx context.Context, arg db.ListFilesByKeyDescParams) ([]db.FileInfo, error)
	ListFilesByLastModifiedAsc(ctx context.Context, arg db.ListFilesByLastModifiedAscParams) ([]db.FileInfo, error)
	ListFilesByLastModifiedDesc(ctx context.Context, arg db.ListFilesByLastModifiedDescParams) ([]db.FileInfo, error)
	ListFilesBySizeAsc(ctx context.Context, arg db.ListFilesBySizeAscParams) ([]db.FileInfo, error)
	ListFilesBySizeDesc(ctx context.Context, arg db.ListFilesBySizeDescParams) ([]db.FileInfo, error)
	ListChildDirectories(ctx context.Context, arg db.ListChildDirectoriesParams) ([]string, error)
}

type SearchServer struct {
	pb.UnimplementedSearchServiceServer
	addr    string
	queries FileIndex
}

// New constructs a SearchServer with its dependencies already built by the
// caller (composition root). It does no I/O; call Serve to start listening.
func New(addr string, queries FileIndex) *SearchServer {
	return &SearchServer{
		addr:    addr,
		queries: queries,
	}
}

func (s *SearchServer) ListFiles(ctx context.Context, req *pb.ListFilesRequest) (*pb.ListFilesResponse, error) {
	sortField, sortOrder, err := normalizeSort(req.GetSortField(), req.GetSortOrder())
	if err != nil {
		return nil, err
	}

	limit, err := normalizePageSize(req.GetPageSize())
	if err != nil {
		return nil, err
	}

	var cur *cursor
	if req.GetPageToken() != "" {
		c, err := decodeCursor(req.GetPageToken())
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid page_token")
		}
		// AIP-158: all arguments other than page_size and page_token must
		// match the call that produced the token.
		if c.GetSortField() != sortField || c.GetSortOrder() != sortOrder ||
			c.GetPrefix() != req.GetPrefix() || c.GetContentType() != req.GetContentType() {
			return nil, status.Error(codes.InvalidArgument, "page_token was issued for a different query")
		}
		cur = c
	}

	// Fetch one extra row to detect whether another page exists.
	files, err := s.listFiles(ctx, sortField, sortOrder, req.GetPrefix(), req.GetContentType(), false, "", cur, limit+1)
	if err != nil {
		return nil, err
	}

	var nextPageToken string
	if len(files) > limit {
		files = files[:limit]
		nextPageToken = encodeCursor(newCursor(sortField, sortOrder, req.GetPrefix(), req.GetContentType(), files[len(files)-1]))
	}

	infos := make([]*pb.FileInfo, 0, len(files))
	for _, f := range files {
		infos = append(infos, dbFileToProto(f))
	}
	return &pb.ListFilesResponse{Files: infos, NextPageToken: nextPageToken}, nil
}

// listFiles dispatches to the sqlc query matching the requested sort.
// directOnly/dirPrefix restrict results to files directly inside dirPrefix
// (no further '/'), used by ListDirectory's files phase; search's ListFiles
// passes directOnly=false to search the whole subtree under prefix.
func (s *SearchServer) listFiles(ctx context.Context, sortField pb.SortField, sortOrder pb.SortOrder, prefix, contentType string, directOnly bool, dirPrefix string, cur *cursor, limit int) ([]db.FileInfo, error) {
	keyPattern := escapeLike(prefix) + "%"
	contentTypePattern := contentTypeToPattern(contentType)
	asc := sortOrder == pb.SortOrder_SORT_ORDER_ASC
	// A file's own row id is never 0 (BIGSERIAL starts at 1), so LastId == 0
	// unambiguously means "no cursor yet" even when cur is non-nil (see
	// ListDirectory: a page that exhausts directories without touching any
	// files still emits a FILES-phase token to resume into, with LastId 0).
	hasCursor := cur != nil && cur.GetLastId() != 0

	switch sortField {
	case pb.SortField_SORT_FIELD_KEY:
		if asc {
			return s.queries.ListFilesByKeyAsc(ctx, db.ListFilesByKeyAscParams{
				KeyPattern:         keyPattern,
				ContentTypePattern: contentTypePattern,
				DirectOnly:         directOnly,
				DirPrefix:          dirPrefix,
				HasCursor:          hasCursor,
				LastKey:            cur.GetKey(),
				LastID:             cur.GetLastId(),
				PageLimit:          int32(limit),
			})
		}
		return s.queries.ListFilesByKeyDesc(ctx, db.ListFilesByKeyDescParams{
			KeyPattern:         keyPattern,
			ContentTypePattern: contentTypePattern,
			DirectOnly:         directOnly,
			DirPrefix:          dirPrefix,
			HasCursor:          hasCursor,
			LastKey:            cur.GetKey(),
			LastID:             cur.GetLastId(),
			PageLimit:          int32(limit),
		})
	case pb.SortField_SORT_FIELD_LAST_MODIFIED:
		if asc {
			return s.queries.ListFilesByLastModifiedAsc(ctx, db.ListFilesByLastModifiedAscParams{
				KeyPattern:         keyPattern,
				ContentTypePattern: contentTypePattern,
				DirectOnly:         directOnly,
				DirPrefix:          dirPrefix,
				HasCursor:          hasCursor,
				CursorLastModified: lastModifiedCursor(cur),
				LastID:             cur.GetLastId(),
				PageLimit:          int32(limit),
			})
		}
		return s.queries.ListFilesByLastModifiedDesc(ctx, db.ListFilesByLastModifiedDescParams{
			KeyPattern:         keyPattern,
			ContentTypePattern: contentTypePattern,
			DirectOnly:         directOnly,
			DirPrefix:          dirPrefix,
			HasCursor:          hasCursor,
			CursorLastModified: lastModifiedCursor(cur),
			LastID:             cur.GetLastId(),
			PageLimit:          int32(limit),
		})
	case pb.SortField_SORT_FIELD_SIZE:
		if asc {
			return s.queries.ListFilesBySizeAsc(ctx, db.ListFilesBySizeAscParams{
				KeyPattern:         keyPattern,
				ContentTypePattern: contentTypePattern,
				DirectOnly:         directOnly,
				DirPrefix:          dirPrefix,
				HasCursor:          hasCursor,
				LastSize:           cur.GetSize(),
				LastID:             cur.GetLastId(),
				PageLimit:          int32(limit),
			})
		}
		return s.queries.ListFilesBySizeDesc(ctx, db.ListFilesBySizeDescParams{
			KeyPattern:         keyPattern,
			ContentTypePattern: contentTypePattern,
			DirectOnly:         directOnly,
			DirPrefix:          dirPrefix,
			HasCursor:          hasCursor,
			LastSize:           cur.GetSize(),
			LastID:             cur.GetLastId(),
			PageLimit:          int32(limit),
		})
	default:
		// normalizeSort only lets known fields through.
		return nil, status.Errorf(codes.InvalidArgument, "unsupported sort_field %v", sortField)
	}
}

// ListDirectory lists the immediate children of path: subdirectories
// (materialized in the directories table, always alphabetical) followed by
// the files directly in it (honoring sort_field/sort_order). A page's
// cursor records which of the two phases it stopped in (see PageToken.phase
// in cursor.proto) so resuming asks the right query.
func (s *SearchServer) ListDirectory(ctx context.Context, req *pb.ListDirectoryRequest) (*pb.ListDirectoryResponse, error) {
	path, err := normalizeDirPath(req.GetPath())
	if err != nil {
		return nil, err
	}
	sortField, sortOrder, err := normalizeSort(req.GetSortField(), req.GetSortOrder())
	if err != nil {
		return nil, err
	}
	limit, err := normalizePageSize(req.GetPageSize())
	if err != nil {
		return nil, err
	}

	var cur *cursor
	phase := cursorv1.ListPhase_LIST_PHASE_DIRECTORIES
	if req.GetPageToken() != "" {
		c, err := decodeCursor(req.GetPageToken())
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid page_token")
		}
		if c.GetPath() != path || c.GetSortField() != sortField || c.GetSortOrder() != sortOrder {
			return nil, status.Error(codes.InvalidArgument, "page_token was issued for a different query")
		}
		cur = c
		phase = c.GetPhase()
	}

	var dirs []string
	remaining := limit

	if phase == cursorv1.ListPhase_LIST_PHASE_DIRECTORIES {
		// ListChildDirectories is an exact keyset query over a dedicated
		// index (see directories' doc comment in migrations/001_initial.sql)
		// — no scan budget, no truncation, unlike the old files-table-
		// derived directory listing this replaced.
		fetched, err := s.queries.ListChildDirectories(ctx, db.ListChildDirectoriesParams{
			Parent:    path,
			HasCursor: cur != nil,
			After:     cur.GetLastDir(),
			PageLimit: int32(limit + 1),
		})
		if err != nil {
			return nil, err
		}
		if len(fetched) > limit {
			dirs = fetched[:limit]
			nextToken := encodeCursor(newDirCursor(sortField, sortOrder, path, dirs[len(dirs)-1]))
			return &pb.ListDirectoryResponse{Directories: dirs, NextPageToken: nextToken}, nil
		}
		dirs = fetched
		remaining = limit - len(dirs)
		// Directories are genuinely exhausted (ListChildDirectories is
		// exact); transition into the files phase fresh, with no file
		// cursor of its own yet.
		cur = nil
	}

	// Fetch one extra file to detect whether another page exists, even when
	// remaining is 0: that still tells us whether a FILES-phase token is
	// needed to resume into (see hasCursor's LastId==0 sentinel in listFiles).
	filesFetched, err := s.listFiles(ctx, sortField, sortOrder, path, "", true, path, cur, remaining+1)
	if err != nil {
		return nil, err
	}

	var files []db.FileInfo
	var nextToken string
	if len(filesFetched) > remaining {
		files = filesFetched[:remaining]
		var last db.FileInfo
		if remaining > 0 {
			last = files[remaining-1]
		}
		// When remaining == 0, last is the zero value: ID 0, which
		// newDirFilesCursor encodes as LastId 0 — the "no cursor yet, resume
		// at the start of the files phase" sentinel listFiles checks for.
		nextToken = encodeCursor(newDirFilesCursor(sortField, sortOrder, path, last))
	} else {
		files = filesFetched
	}

	infos := make([]*pb.FileInfo, 0, len(files))
	for _, f := range files {
		infos = append(infos, dbFileToProto(f))
	}
	return &pb.ListDirectoryResponse{Directories: dirs, Files: infos, NextPageToken: nextToken}, nil
}

// normalizeDirPath validates and normalizes a ListDirectory path: "" means
// the bucket root; anything else is trimmed of a leading "/" and given a
// trailing one. Rejects ".." path segments — not a real filesystem, but
// there is no legitimate reason for a browse path to contain one, and
// allowing it would make the directories-table lookup behave surprisingly.
func normalizeDirPath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if hasDotDotSegment(path) {
		return "", status.Errorf(codes.InvalidArgument, "invalid path %q", path)
	}
	path = strings.TrimPrefix(path, "/")
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	return path, nil
}

// hasDotDotSegment reports whether s contains ".." as a whole path segment,
// not merely as a substring. A copy of files.hasDotDotSegment; kept
// package-local for the same reason escapeLike is duplicated rather than
// shared across these two service packages.
func hasDotDotSegment(s string) bool {
	for _, segment := range strings.Split(s, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}

func (s *SearchServer) Serve() error {
	lis, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	grpcServer := grpc.NewServer()
	pb.RegisterSearchServiceServer(grpcServer, s)
	slog.Info("listening", "addr", s.addr)
	return grpcServer.Serve(lis)
}

// normalizeSort applies defaults (key ascending) and rejects enum values this
// server does not know about.
func normalizeSort(field pb.SortField, order pb.SortOrder) (pb.SortField, pb.SortOrder, error) {
	switch field {
	case pb.SortField_SORT_FIELD_UNSPECIFIED:
		field = pb.SortField_SORT_FIELD_KEY
	case pb.SortField_SORT_FIELD_KEY, pb.SortField_SORT_FIELD_LAST_MODIFIED, pb.SortField_SORT_FIELD_SIZE:
	default:
		return 0, 0, status.Errorf(codes.InvalidArgument, "unknown sort_field %d", field)
	}
	switch order {
	case pb.SortOrder_SORT_ORDER_UNSPECIFIED:
		order = pb.SortOrder_SORT_ORDER_ASC
	case pb.SortOrder_SORT_ORDER_ASC, pb.SortOrder_SORT_ORDER_DESC:
	default:
		return 0, 0, status.Errorf(codes.InvalidArgument, "unknown sort_order %d", order)
	}
	return field, order, nil
}

func normalizePageSize(pageSize int32) (int, error) {
	switch {
	case pageSize < 0:
		return 0, status.Error(codes.InvalidArgument, "page_size must not be negative")
	case pageSize == 0:
		return defaultPageSize, nil
	case pageSize > maxPageSize:
		return maxPageSize, nil
	default:
		return int(pageSize), nil
	}
}

// contentTypeToPattern turns the content_type filter into a LIKE pattern: an
// empty filter disables it, a category like "image/" becomes a prefix match,
// and anything else matches exactly.
func contentTypeToPattern(contentType string) string {
	if contentType == "" {
		return ""
	}
	pattern := escapeLike(contentType)
	if strings.HasSuffix(contentType, "/") {
		pattern += "%"
	}
	return pattern
}

// escapeLike escapes LIKE metacharacters in user input so it matches
// literally inside a pattern.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// dbFileToProto maps the file_infos read model to the API shape. Metadata
// fields are NULL until a file is stat-indexed; timestamps stay unset (nil)
// rather than encoding the zero time. created_at is discovery time and
// updated_at is the object's last-modified time from the stat index.
// preview_key/width/height come from the preview index and are empty/zero
// until it runs (or the file isn't an image).
func dbFileToProto(f db.FileInfo) *pb.FileInfo {
	info := &pb.FileInfo{
		Id:            f.ID,
		Key:           f.Key,
		ContentType:   f.ContentType.String,
		SizeBytes:     f.SizeBytes.Int64,
		PreviewKey:    f.PreviewKey.String,
		PreviewWidth:  f.PreviewWidth.Int32,
		PreviewHeight: f.PreviewHeight.Int32,
	}
	if f.CreatedAt.Valid {
		info.CreatedAt = timestamppb.New(f.CreatedAt.Time)
	}
	if f.LastModified.Valid {
		info.UpdatedAt = timestamppb.New(f.LastModified.Time)
	}
	return info
}
