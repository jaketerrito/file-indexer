package indexer

import (
	"context"
	"file-indexer/internal/db/sqlc"
	"file-indexer/internal/pb"
	"io"
	"log/slog"
	"net"
	"net/http"

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

	res, err := s.db.Placeholder(stream.Context())
	if err != nil {
		return err
	}
	slog.Info("test", "result", res)

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			break // DONE
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

			// Should have something to create a buffer or something for reading the file
			continue
		}

		// Read file contents
		contentReq, ok := req.GetData().(*pb.IndexRequest_Content)
		if !ok {
			return status.Errorf(codes.InvalidArgument, "protocol violation: content missing")
		}

		if contentType == "" {
			// Content Type can be determined with first 512 bytes of a file
			limit := min(512, len(contentReq.Content))
			contentType = http.DetectContentType(contentReq.Content[:limit])
		}
		// Do something with the content
		// slog.Info("Data", "content", contentReq.Content)
	}

	slog.Info("handling", "name", metadata.Name, "content type", contentType)
	return stream.SendAndClose(&pb.IndexResponse{Status: "GOOD"})
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
