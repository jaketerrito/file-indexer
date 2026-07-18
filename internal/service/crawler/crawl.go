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
	// InsertFiles registers discovered keys (identity only) and returns the
	// ids of files that are new.
	InsertFiles(ctx context.Context, keys []string) ([]int64, error)
	// ResetChangedIndexStat re-enqueues stat indexing for files whose
	// listing size/last-modified no longer matches the stored stat results,
	// returning how many were reset.
	ResetChangedIndexStat(ctx context.Context, arg db.ResetChangedIndexStatParams) (int64, error)
}

// Crawler reconciles the object store with the database: it walks the S3
// listing and registers each object's key — identity only, no metadata.
// Indexing is not triggered here; the indexer worker pools discover new
// files by seeding from the files table. The crawler's only other job is
// change detection: comparing the listing's size/last-modified against the
// stat index's stored results and re-enqueueing files that changed.
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

	var discovered, added, changed int64
	batch := db.ResetChangedIndexStatParams{}
	// inBatch guards against duplicate keys within one flush: InsertFiles is
	// a single INSERT ... ON CONFLICT statement, and postgres rejects a
	// statement that touches the same row twice ("cannot affect row a second
	// time"). S3 listings should never repeat a key, but a dropped duplicate
	// is strictly better than a failed crawl if one ever does.
	inBatch := make(map[string]struct{}, c.batchSize)

	flush := func() error {
		if len(batch.Keys) == 0 {
			return nil
		}
		newIDs, reset, err := c.flush(ctx, batch)
		if err != nil {
			return err
		}
		discovered += int64(len(batch.Keys))
		added += newIDs
		changed += reset
		batch = db.ResetChangedIndexStatParams{}
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

	slog.Info("crawl finished", "discovered", discovered, "new", added, "changed", changed)
	return nil
}

// flush registers one batch of keys and re-enqueues stat indexing for files
// the listing shows as changed. The two statements are deliberately
// independent: newly inserted files have no index_stat row yet and are
// enqueued by the stat worker's seeding, not here.
func (c *Crawler) flush(ctx context.Context, batch db.ResetChangedIndexStatParams) (newIDs, reset int64, err error) {
	ids, err := c.files.InsertFiles(ctx, batch.Keys)
	if err != nil {
		return 0, 0, err
	}
	reset, err = c.files.ResetChangedIndexStat(ctx, batch)
	if err != nil {
		return 0, 0, err
	}
	slog.Info("registered files", "batch", len(batch.Keys), "new", len(ids), "changed", reset)
	return int64(len(ids)), reset, nil
}
