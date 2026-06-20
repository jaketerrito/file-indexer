package crawler

import (
	"context"
	pb "file-indexer/internal/pb/service/v1"
	"log/slog"
)

type ObjectStore interface {
	Walk(ctx context.Context, fn func(*pb.FileRef) error) error
}

// Crawler walks an object store and sends discovered file references to the
// indexer service over gRPC.
type Crawler struct {
	store  ObjectStore
	client pb.IndexerServiceClient
}

// New constructs a Crawler with its dependencies already built by the caller
// (composition root). It does no I/O; call Run to start crawling.
func New(store ObjectStore, client pb.IndexerServiceClient) *Crawler {
	return &Crawler{store: store, client: client}
}

// Run walks the object store and emits an index request per discovered file.
func (c *Crawler) Run() error {
	return c.store.Walk(context.Background(), func(ref *pb.FileRef) error {
		response, err := c.client.Index(context.Background(), &pb.IndexRequest{
			Ref: ref,
		})
		if err != nil {
			return err
		}

		slog.Info("uploaded", "file", ref.GetKey(), "status", response.Status)
		return nil
	})
}
