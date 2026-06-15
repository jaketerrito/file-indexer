package indexer

import (
	"context"
	"file-indexer/internal/db"
	"file-indexer/internal/pb"
	"file-indexer/internal/storage"
	"log/slog"
	"net"

	"google.golang.org/grpc"
)

type IndexerServer struct {
	pb.UnimplementedIndexerServer
	addr    string
	storage storage.Storage
	queries *db.Queries
}

// New constructs an IndexerServer with its dependencies already built by the
// caller (composition root). It does no I/O; call Serve to start listening.
func New(addr string, store storage.Storage, queries *db.Queries) *IndexerServer {
	return &IndexerServer{
		addr:    addr,
		storage: store,
		queries: queries,
	}
}

func (s *IndexerServer) Index(ctx context.Context, req *pb.IndexRequest) (*pb.IndexResponse, error) {
	ref := req.GetRef()
	bucket := ref.GetBucket()
	path := ref.GetPath()

	// TODO: fetch the object from storage (s.storage.Get(ctx, bucket, path)),
	// compute metadata (size, checksum, MIME, EXIF), and persist via s.queries.
	_ = bucket
	_ = path

	return &pb.IndexResponse{Status: "OK"}, nil
}

// Serve starts the gRPC server and blocks until it stops.
func (s *IndexerServer) Serve() error {
	lis, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	grpcServer := grpc.NewServer()
	pb.RegisterIndexerServer(grpcServer, s)
	slog.Info("listening", "addr", s.addr)
	return grpcServer.Serve(lis)
}
