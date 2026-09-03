// Package previewgc reclaims orphaned preview objects. Previews are derived
// blobs written back to the same bucket under the index prefix's previews/
// directory, with ownership recorded in index_preview_result. Rows vanish
// without touching storage whenever a files row is deleted — out-of-band S3
// deletes via the crawler sweep, or DeleteFile's best-effort preview cleanup
// failing — so the prefix grows without bound unless a job set-diffs the
// bucket listing against the table and deletes what no row claims.
package previewgc

import (
	"context"
	"file-indexer/internal/service/indexer"
	"file-indexer/internal/storage"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// minAge is how old an unclaimed object must be before it counts as an
// orphan. A preview PUT lands before its index_preview_result row (the row
// is written by the queue's Complete step after processing finishes), so a
// freshly written preview is legitimately row-less for up to one processing
// round trip. An hour bounds that window with wide margin; anything older
// with no row will never gain one.
const minAge = time.Hour

// ObjectStore is the slice of storage.Storage the GC depends on. Walk lists
// the whole bucket (there is no prefix-scoped listing); the GC filters by
// prefix itself. DeleteMany self-batches past S3's 1000-key multi-delete
// limit, so orphans are handed over in one call.
type ObjectStore interface {
	Walk(ctx context.Context, fn func(storage.ObjectInfo) error) error
	DeleteMany(ctx context.Context, keys []string) error
}

// PreviewStore is the slice of db.Queries the GC depends on.
type PreviewStore interface {
	ListIndexPreviewKeys(ctx context.Context) ([]string, error)
}

// GC deletes orphaned preview objects. Safe to re-run at any cadence: it
// only ever removes keys no index_preview_result row claims, so a run over
// a clean bucket deletes nothing.
type GC struct {
	store    ObjectStore
	previews PreviewStore
	prefix   string
	// now is a clock seam for tests; production leaves it as time.Now.
	now func() time.Time
}

// New constructs a GC. indexPrefix is the same value the crawler and
// preview indexer receive (config.IndexPrefix); the scanned prefix is
// derived with the same join the preview writer uses.
func New(store ObjectStore, previews PreviewStore, indexPrefix string) *GC {
	return &GC{store: store, previews: previews, prefix: indexer.PreviewPrefix(indexPrefix), now: time.Now}
}

// Run lists claimed preview keys, walks the bucket, and deletes every object
// under the previews/ prefix that is unclaimed and older than minAge. A
// database or listing failure aborts before anything is deleted.
func (g *GC) Run(ctx context.Context) error {
	keys, err := g.previews.ListIndexPreviewKeys(ctx)
	if err != nil {
		return fmt.Errorf("list claimed preview keys: %w", err)
	}
	claimed := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		claimed[k] = struct{}{}
	}

	cutoff := g.now().Add(-minAge)
	var orphans []string
	if err := g.store.Walk(ctx, func(info storage.ObjectInfo) error {
		if !strings.HasPrefix(info.Key, g.prefix) {
			return nil
		}
		if _, ok := claimed[info.Key]; ok {
			return nil
		}
		if info.LastModified.After(cutoff) {
			return nil // in-flight write; a later run reconsiders it
		}
		orphans = append(orphans, info.Key)
		return nil
	}); err != nil {
		return fmt.Errorf("walk bucket: %w", err)
	}

	if len(orphans) == 0 {
		slog.Info("preview gc: no orphans", "claimed", len(claimed))
		return nil
	}
	if err := g.store.DeleteMany(ctx, orphans); err != nil {
		return fmt.Errorf("delete orphaned previews: %w", err)
	}
	slog.Info("preview gc: deleted orphans", "deleted", len(orphans), "claimed", len(claimed))
	return nil
}
