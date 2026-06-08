package indexer

import (
	"context"
	"file-indexer/internal/db/sqlc"
	"file-indexer/internal/pb"
	"io"
	"log/slog"
	"net"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type IndexerServer struct {
	pb.UnimplementedIndexerServer
	db *sqlc.Queries
}

func (s *IndexerServer) Index(stream grpc.ClientStreamingServer[pb.IndexRequest, pb.IndexResponse]) error {
	var metadata *pb.FileMetadata
	var contentType string
	var totalBytes int64

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if metadata == nil {
			metaReq, ok := req.GetData().(*pb.IndexRequest_Metadata)
			if !ok {
				return status.Errorf(codes.InvalidArgument, "protocol violation: the first stream message must be 'metadata'")
			}
			metadata = metaReq.Metadata
			continue
		}

		contentReq, ok := req.GetData().(*pb.IndexRequest_Content)
		if !ok {
			return status.Errorf(codes.InvalidArgument, "protocol violation: content missing")
		}

		if contentType == "" && len(contentReq.Content) > 0 {
			limit := min(512, len(contentReq.Content))
			contentType = http.DetectContentType(contentReq.Content[:limit])
		}

		totalBytes += int64(len(contentReq.Content))
	}

	file, err := s.db.CreateFile(stream.Context(), sqlc.CreateFileParams{
		Source:      metadata.Source,
		Path:        metadata.Name,
		ContentType: pgtype.Text{String: contentType, Valid: contentType != ""},
		SizeBytes:   pgtype.Int8{Int64: totalBytes, Valid: true},
	})
	if err != nil {
		return err
	}

	slog.Info("Indexed", "file", file, "content_type", contentType, "size_bytes", totalBytes)
	return stream.SendAndClose(&pb.IndexResponse{Status: "OK"})
}

func (s *IndexerServer) Run(addr, databaseURL string) error {
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	s.db = sqlc.New(pool)

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	grpcServer := grpc.NewServer()
	pb.RegisterIndexerServer(grpcServer, s)
	slog.Info("listening", "addr", addr)
	return grpcServer.Serve(lis)
}
