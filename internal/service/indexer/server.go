// Package indexer implements the gRPC Indexer service.
package indexer

import (
	"context"
	"file-indexer/internal/db"
	pb "file-indexer/internal/pb/service/v1"
	"file-indexer/internal/storage"
	"log/slog"
	"net"

	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/grpc"
)

type ObjectStore interface {
	Stat(ctx context.Context, key string) (storage.ObjectInfo, error)
}

type FileIndex interface {
	CreateFile(ctx context.Context, arg db.CreateFileParams) (db.File, error)
}

type IndexerServer struct {
	pb.UnimplementedIndexerServiceServer
	addr    string
	storage ObjectStore
	queries FileIndex
}

// New constructs an IndexerServer with its dependencies already built by the
// caller (composition root). It does no I/O; call Serve to start listening.
func New(addr string, store ObjectStore, queries FileIndex) *IndexerServer {
	return &IndexerServer{
		addr:    addr,
		storage: store,
		queries: queries,
	}
}

func (s *IndexerServer) Index(ctx context.Context, req *pb.IndexRequest) (*pb.IndexResponse, error) {
	ref := req.GetRef()
	key := ref.GetKey()

	info, err := s.storage.Stat(ctx, key)
	if err != nil {
		return nil, err
	}

	if _, err := s.queries.CreateFile(ctx, db.CreateFileParams{
		Key:         info.Key,
		ContentType: pgtype.Text{String: info.ContentType, Valid: true},
		SizeBytes:   pgtype.Int8{Int64: info.Size, Valid: true},
		CreatedAt:   pgtype.Timestamptz{Time: info.LastModified, Valid: true},
		UpdatedAt:   pgtype.Timestamptz{Time: info.LastModified, Valid: true},
	}); err != nil {
		return nil, err
	}

	return &pb.IndexResponse{Status: "OK"}, nil
}

// Serve starts the gRPC server and blocks until it stops.
func (s *IndexerServer) Serve() error {
	lis, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	grpcServer := grpc.NewServer()
	pb.RegisterIndexerServiceServer(grpcServer, s)
	slog.Info("listening", "addr", s.addr)
	return grpcServer.Serve(lis)
}
