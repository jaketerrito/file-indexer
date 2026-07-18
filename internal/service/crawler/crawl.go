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
	// UpsertFiles registers discovered objects and returns the ids of files
	// that are new or whose size/last-modified changed.
	UpsertFiles(ctx context.Context, arg db.UpsertFilesParams) ([]int64, error)
	// ResetIndexStat re-enqueues the given files for stat indexing.
	ResetIndexStat(ctx context.Context, fileIds []int64) (int64, error)
}

// Crawler reconciles the object store with the database: it walks the S3
// listing and upserts a files row per object. Indexing itself is not
// triggered here — the indexer worker pools discover new files by seeding
// from the files table; the crawler only resets index state for files whose
// content changed so they get re-indexed.
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

// Run walks the object store and registers discovered files in batches.
func (c *Crawler) Run(ctx context.Context) error {
	slog.Info("crawl starting", "batchSize", c.batchSize)

	var discovered, changed int64
	batch := db.UpsertFilesParams{}
	// inBatch guards against duplicate keys within one flush: UpsertFiles is
	// a single INSERT ... ON CONFLICT statement, and postgres rejects a
	// statement that touches the same row twice ("cannot affect row a second
	// time"). S3 listings should never repeat a key, but a dropped duplicate
	// is strictly better than a failed crawl if one ever does.
	inBatch := make(map[string]struct{}, c.batchSize)

	flush := func() error {
		if len(batch.Keys) == 0 {
			return nil
		}
		n, err := c.flush(ctx, batch)
		if err != nil {
			return err
		}
		discovered += int64(len(batch.Keys))
		changed += n
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
		batch.Sizes = append(batch.Sizes, info.Size)
		batch.LastModifieds = append(batch.LastModifieds,
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

	slog.Info("crawl finished", "discovered", discovered, "newOrChanged", changed)
	return nil
}

// flush upserts one batch and re-enqueues stat indexing for files that were
// new or changed. UpsertFiles returns ids for both; resetting a file that
// has no index_stat row yet is a harmless no-op (seeding will enqueue it).
func (c *Crawler) flush(ctx context.Context, batch db.UpsertFilesParams) (int64, error) {
	ids, err := c.files.UpsertFiles(ctx, batch)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	if _, err := c.files.ResetIndexStat(ctx, ids); err != nil {
		return 0, err
	}
	slog.Info("registered files", "batch", len(batch.Keys), "newOrChanged", len(ids))
	return int64(len(ids)), nil
}
