package crawler

import (
	"context"
	"file-indexer/internal/db"
	"file-indexer/internal/storage"
	"log/slog"
	"strings"
	"time"

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
// directory-index-maintaining WithDirectories variants, never the bare
// UpsertFiles/DeleteUnseenFiles, so the crawler cannot write or delete a
// files row without also keeping the directories table in sync (see
// directories' doc comment in migrations/001_initial.sql). DatabaseNow and
// DeleteUnseenFilesWithDirectories together back Run's end-of-crawl
// out-of-band-delete sweep — see Run's doc comment.
type FileStore interface {
	UpsertFilesWithDirectories(ctx context.Context, arg db.UpsertFilesParams) (int64, error)
	// DatabaseNow returns the database's clock. Must be called before Walk
	// starts (see Run) so the cutoff it produces predates every row Walk
	// will re-stamp — see DeleteUnseenFilesWithDirectories's doc comment
	// for the race argument this depends on.
	DatabaseNow(ctx context.Context) (pgtype.Timestamptz, error)
	// DeleteUnseenFilesWithDirectories deletes every files row whose
	// seen_at predates cutoff and prunes the directories it orphans.
	DeleteUnseenFilesWithDirectories(ctx context.Context, cutoff time.Time) (int64, error)
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

	// cutoff is read before Walk starts, and used only after Walk and every
	// flush succeed — see DeleteUnseenFilesWithDirectories's doc comment for
	// why that ordering is what makes the sweep below race-free: any row
	// still in S3 gets re-stamped past cutoff by this crawl's own listing,
	// so only genuinely gone objects are ever swept. A failed Walk or flush
	// means an incomplete listing, which must never be read as "everything
	// unstamped is gone" — so the sweep is skipped entirely on any error
	// above it, not just guarded by the cutoff.
	cutoff, err := c.files.DatabaseNow(ctx)
	if err != nil {
		return err
	}

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

	err = c.store.Walk(ctx, func(info storage.ObjectInfo) error {
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

	// Sweep: files rows deleted from S3 out of band never get re-stamped by
	// the Walk above, so anything still marked seen before cutoff is gone.
	// No guardrail on how much this deletes (e.g. a fraction-of-total
	// circuit breaker) — S3 remains the source of truth, so a sweep run
	// against a misconfigured bucket or a genuinely large out-of-band
	// deletion is not a correctness problem, just a cost one: a subsequent
	// correct crawl re-populates files (and directories, second-order) from
	// scratch and re-indexing picks up from there, same as any other
	// re-crawl. See queries/files.sql's DeleteUnseenFiles and
	// store.go's DeleteUnseenFilesWithDirectories for the race argument and
	// why directory pruning stays unscoped rather than keyed off the swept
	// keys.
	deleted, err := c.files.DeleteUnseenFilesWithDirectories(ctx, cutoff.Time)
	if err != nil {
		return err
	}

	slog.Info("crawl finished", "discovered", discovered, "skipped", skipped, "deleted", deleted)
	return nil
}
