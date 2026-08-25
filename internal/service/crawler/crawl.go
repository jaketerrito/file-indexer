package crawler

import (
	"context"
	"file-indexer/internal/db"
	"file-indexer/internal/storage"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

// defaultBatchSize bounds how many discovered objects are buffered before
// being flushed to the database in one round trip.
const defaultBatchSize = 500

// ObjectStore is the slice of storage.Storage the crawler depends on.
type ObjectStore interface {
	Walk(ctx context.Context, fn func(storage.ObjectInfo) error) error
}

// FileStore is deliberately narrower than db.Store: it exposes only the
// directory-index-maintaining UpsertFilesWithDirectories, never the bare
// UpsertFiles, so the crawler cannot write a files row without also keeping
// the directories table in sync (see directories' doc comment in
// migrations/001_initial.sql). PruneOrphanDirectories backs the
// end-of-crawl reconcile pass — see Run's doc comment on why only the
// prune half of that pass exists.
type FileStore interface {
	UpsertFilesWithDirectories(ctx context.Context, arg db.UpsertFilesParams) (int64, error)
	PruneOrphanDirectories(ctx context.Context) error
}

// Crawler reconciles the object store with the database: it walks the S3
// listing and upserts each object's key and last-modified time. The
// last-modified time acts as a staleness mark — index worker seed steps
// compare their stored mark against files.marked_at to detect edits and
// re-enqueue stale results automatically.
type Crawler struct {
	store        ObjectStore
	files        FileStore
	batchSize    int
	ignorePrefix string
}

// New constructs a Crawler. ignorePrefix is the key prefix holding
// index-generated objects (see config.IndexPrefix); objects beneath it are
// skipped so derived artifacts never become files rows — without this the
// preview index type would generate previews of its own previews, forever.
// Pass "" to disable skipping.
func New(store ObjectStore, files FileStore, ignorePrefix string) *Crawler {
	return &Crawler{
		store:        store,
		files:        files,
		batchSize:    defaultBatchSize,
		ignorePrefix: ignorePrefix,
	}
}

func (c *Crawler) Run(ctx context.Context) error {
	slog.Info("crawl starting", "batchSize", c.batchSize)

	var discovered, skipped int64
	batch := db.UpsertFilesParams{}

	flush := func() error {
		if len(batch.Keys) == 0 {
			return nil
		}
		if _, err := c.files.UpsertFilesWithDirectories(ctx, batch); err != nil {
			return err
		}
		slog.Info("registered files", "batch", len(batch.Keys))
		discovered += int64(len(batch.Keys))
		batch = db.UpsertFilesParams{}
		return nil
	}

	err := c.store.Walk(ctx, func(info storage.ObjectInfo) error {
		if c.ignorePrefix != "" && strings.HasPrefix(info.Key, c.ignorePrefix) {
			skipped++
			return nil
		}

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

	// PruneOrphanDirectories is a no-op today: nothing creates an orphan
	// directory row currently — every files write goes through Store's
	// WithDirectories methods, which keep directories in sync inline, in
	// the same transaction. There is deliberately no RebuildDirectories
	// call to pair with it: UpsertFilesWithDirectories already inserted
	// every directory this crawl could produce, per batch, above (ON
	// CONFLICT DO NOTHING) — a separate unscoped rebuild pass afterward
	// would only re-derive the same rows from the same keys via the same
	// SQL, so it could never find anything the per-batch insert missed.
	//
	// This call earns its keep once the crawler gains the ability to
	// remove files rows for objects deleted from S3 out of band (see
	// NOTES.md): "delete whatever files rows we didn't just see" is a set
	// difference with no natural per-key list to hand PruneDirectoriesForKeys,
	// so the unscoped scan becomes the practical way to prune what that
	// leaves behind. Until then, it's cheap insurance against drift from
	// any other cause (manual SQL, a future write path that skips Store).
	if err := c.files.PruneOrphanDirectories(ctx); err != nil {
		return err
	}

	slog.Info("crawl finished", "discovered", discovered, "skipped", skipped)
	return nil
}
