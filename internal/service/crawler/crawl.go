package crawler

import (
	"context"
	pb "file-indexer/internal/pb/service/v1"
	"file-indexer/internal/storage"
	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Crawler walks an object store and sends discovered file references to the
// indexer service over gRPC.
type Crawler struct {
	addr  string
	store storage.Storage
}

// New constructs a Crawler with its dependencies already built by the caller
// (composition root). It does no I/O; call Run to start crawling.
func New(addr string, store storage.Storage) *Crawler {
	return &Crawler{addr: addr, store: store}
}

// Run dials the indexer and walks the filesystem, emitting a reference per file.
func (c *Crawler) Run() error {
	slog.Info("connecting to indexer", "addr", c.addr)
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	conn, err := grpc.NewClient(c.addr, opts...)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	client := pb.NewIndexerServiceClient(conn)

	return c.store.Walk(context.Background(), func(ref *pb.FileRef) error {
		response, err := client.Index(context.Background(), &pb.IndexRequest{
			Ref: ref,
		})
		if err != nil {
			return err
		}

		slog.Info("uploaded", "file", ref.GetKey(), "status", response.Status)
		return nil
	})
}
