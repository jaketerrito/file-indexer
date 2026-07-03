// Package search implements the SearchService gRPC API: listing indexed
// files with prefix and content-type filters, configurable sorting, and
// keyset pagination via opaque page tokens.
package search

import (
	"context"
	"file-indexer/internal/db"
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
// query per (sort field, direction), all keyset-paginated on (value, id).
type FileIndex interface {
	ListFilesByKeyAsc(ctx context.Context, arg db.ListFilesByKeyAscParams) ([]db.File, error)
	ListFilesByKeyDesc(ctx context.Context, arg db.ListFilesByKeyDescParams) ([]db.File, error)
	ListFilesByCreatedAtAsc(ctx context.Context, arg db.ListFilesByCreatedAtAscParams) ([]db.File, error)
	ListFilesByCreatedAtDesc(ctx context.Context, arg db.ListFilesByCreatedAtDescParams) ([]db.File, error)
	ListFilesBySizeAsc(ctx context.Context, arg db.ListFilesBySizeAscParams) ([]db.File, error)
	ListFilesBySizeDesc(ctx context.Context, arg db.ListFilesBySizeDescParams) ([]db.File, error)
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
	files, err := s.listFiles(ctx, sortField, sortOrder, req.GetPrefix(), req.GetContentType(), cur, limit+1)
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
func (s *SearchServer) listFiles(ctx context.Context, sortField pb.SortField, sortOrder pb.SortOrder, prefix, contentType string, cur *cursor, limit int) ([]db.File, error) {
	keyPattern := escapeLike(prefix) + "%"
	contentTypePattern := contentTypeToPattern(contentType)
	asc := sortOrder == pb.SortOrder_SORT_ORDER_ASC

	switch sortField {
	case pb.SortField_SORT_FIELD_KEY:
		if asc {
			return s.queries.ListFilesByKeyAsc(ctx, db.ListFilesByKeyAscParams{
				KeyPattern:         keyPattern,
				ContentTypePattern: contentTypePattern,
				HasCursor:          cur != nil,
				LastKey:            cur.GetKey(),
				LastID:             cur.GetLastId(),
				PageLimit:          int32(limit),
			})
		}
		return s.queries.ListFilesByKeyDesc(ctx, db.ListFilesByKeyDescParams{
			KeyPattern:         keyPattern,
			ContentTypePattern: contentTypePattern,
			HasCursor:          cur != nil,
			LastKey:            cur.GetKey(),
			LastID:             cur.GetLastId(),
			PageLimit:          int32(limit),
		})
	case pb.SortField_SORT_FIELD_CREATED_AT:
		if asc {
			return s.queries.ListFilesByCreatedAtAsc(ctx, db.ListFilesByCreatedAtAscParams{
				KeyPattern:         keyPattern,
				ContentTypePattern: contentTypePattern,
				HasCursor:          cur != nil,
				LastCreatedAt:      lastCreatedAt(cur),
				LastID:             cur.GetLastId(),
				PageLimit:          int32(limit),
			})
		}
		return s.queries.ListFilesByCreatedAtDesc(ctx, db.ListFilesByCreatedAtDescParams{
			KeyPattern:         keyPattern,
			ContentTypePattern: contentTypePattern,
			HasCursor:          cur != nil,
			LastCreatedAt:      lastCreatedAt(cur),
			LastID:             cur.GetLastId(),
			PageLimit:          int32(limit),
		})
	case pb.SortField_SORT_FIELD_SIZE:
		if asc {
			return s.queries.ListFilesBySizeAsc(ctx, db.ListFilesBySizeAscParams{
				KeyPattern:         keyPattern,
				ContentTypePattern: contentTypePattern,
				HasCursor:          cur != nil,
				LastSize:           cur.GetSize(),
				LastID:             cur.GetLastId(),
				PageLimit:          int32(limit),
			})
		}
		return s.queries.ListFilesBySizeDesc(ctx, db.ListFilesBySizeDescParams{
			KeyPattern:         keyPattern,
			ContentTypePattern: contentTypePattern,
			HasCursor:          cur != nil,
			LastSize:           cur.GetSize(),
			LastID:             cur.GetLastId(),
			PageLimit:          int32(limit),
		})
	default:
		// normalizeSort only lets known fields through.
		return nil, status.Errorf(codes.InvalidArgument, "unsupported sort_field %v", sortField)
	}
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
	case pb.SortField_SORT_FIELD_KEY, pb.SortField_SORT_FIELD_CREATED_AT, pb.SortField_SORT_FIELD_SIZE:
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

func dbFileToProto(f db.File) *pb.FileInfo {
	return &pb.FileInfo{
		Id:          f.ID,
		Key:         f.Key,
		ContentType: f.ContentType.String,
		SizeBytes:   f.SizeBytes.Int64,
		CreatedAt:   timestamppb.New(f.CreatedAt.Time),
		UpdatedAt:   timestamppb.New(f.UpdatedAt.Time),
	}
}
