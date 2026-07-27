package crawler

import (
	"context"
	"file-indexer/internal/db"
	"file-indexer/internal/storage"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
)

// defaultBatchSize bounds how many discovered objects are buffered before
// being flushed to the database in one round trip.
const defaultBatchSize = 500

// ObjectStore is the slice of storage.Storage the crawler depends on.
type ObjectStore interface {
	Walk(ctx context.Context, fn func(storage.ObjectInfo) error) error
}

type FileStore interface {
	UpsertFiles(ctx context.Context, arg db.UpsertFilesParams) (int64, error)
}

// Crawler reconciles the object store with the database: it walks the S3
// listing and upserts each object's key and last-modified time. The
// last-modified time acts as a staleness mark — index worker seed steps
// compare their stored mark against files.marked_at to detect edits and
// re-enqueue stale results automatically.
type Crawler struct {
	store     ObjectStore
	files     FileStore
	batchSize int
}

func New(store ObjectStore, files FileStore) *Crawler {
	return &Crawler{store: store, files: files, batchSize: defaultBatchSize}
}

func (c *Crawler) Run(ctx context.Context) error {
	slog.Info("crawl starting", "batchSize", c.batchSize)

	var discovered int64
	batch := db.UpsertFilesParams{}

	flush := func() error {
		if len(batch.Keys) == 0 {
			return nil
		}
		if _, err := c.files.UpsertFiles(ctx, batch); err != nil {
			return err
		}
		slog.Info("registered files", "batch", len(batch.Keys))
		discovered += int64(len(batch.Keys))
		batch = db.UpsertFilesParams{}
		return nil
	}

	err := c.store.Walk(ctx, func(info storage.ObjectInfo) error {
		batch.Keys = append(batch.Keys, info.Key)
		batch.MarkedAts = append(batch.MarkedAts,
			pgtype.Timestamptz{Time: info.LastModified, Valid: true})

		if len(batch.Keys) >= c.batchSize {
			return flush()
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := flush(); err != nil {
		return err
	}

	slog.Info("crawl finished", "discovered", discovered)
	return nil
}
