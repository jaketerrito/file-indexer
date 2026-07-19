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

// FileStore is the slice of db.Queries the crawler depends on.
type FileStore interface {
	// UpsertFiles registers discovered keys with their listing last-modified
	// times (the staleness mark). Existing rows are updated only when the
	// new mtime is strictly newer.
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

// New constructs a Crawler with its dependencies already built by the caller
// (composition root). It does no I/O; call Run to start crawling.
func New(store ObjectStore, files FileStore) *Crawler {
	return &Crawler{store: store, files: files, batchSize: defaultBatchSize}
}

// Run walks the object store and upserts discovered files in batches.
func (c *Crawler) Run(ctx context.Context) error {
	slog.Info("crawl starting", "batchSize", c.batchSize)

	var discovered int64
	batch := db.UpsertFilesParams{}
	// inBatch guards against duplicate keys within one flush: the upsert is
	// a single INSERT ... ON CONFLICT statement, and postgres rejects a
	// statement that touches the same row twice ("cannot affect row a second
	// time"). S3 listings should never repeat a key, but a dropped duplicate
	// is strictly better than a failed crawl if one ever does.
	inBatch := make(map[string]struct{}, c.batchSize)

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
		clear(inBatch)
		return nil
	}

	err := c.store.Walk(ctx, func(info storage.ObjectInfo) error {
		if _, dup := inBatch[info.Key]; dup {
			slog.Warn("duplicate key in listing, skipping", "key", info.Key)
			return nil
		}
		inBatch[info.Key] = struct{}{}
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
