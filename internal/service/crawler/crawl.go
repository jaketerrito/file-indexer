package crawler

import (
	"context"
	"file-indexer/internal/pb"
	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Crawler walks a filesystem and sends discovered file references to the
// indexer service over gRPC.
type Crawler struct {
	addr   string
	walker FileWalker
}

// New constructs a Crawler with its dependencies already built by the caller
// (composition root). It does no I/O; call Run to start crawling.
func New(addr string, walker FileWalker) *Crawler {
	return &Crawler{addr: addr, walker: walker}
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

	client := pb.NewIndexerClient(conn)

	return c.walker.Walk(func(ref *pb.FileRef) error {
		response, err := client.Index(context.Background(), &pb.IndexRequest{
			Ref: ref,
		})
		if err != nil {
			return err
		}

		slog.Info("uploaded", "file", ref.GetPath(), "status", response.Status)
		return nil
	})
}
