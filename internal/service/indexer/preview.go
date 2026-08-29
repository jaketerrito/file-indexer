package indexer

import (
	"bytes"
	"context"
	"errors"
	"file-indexer/internal/db"
	"file-indexer/internal/storage"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"math"
	"path"
	"strconv"
	"strings"

	"golang.org/x/image/draw"

	// Decoder registration for image.Decode / image.DecodeConfig. jpeg and
	// png register via the imports above. Only pure-Go decoders are
	// available: the binary is built CGO_ENABLED=0 on scratch, which rules
	// out the libvips/ImageMagick-backed formats (HEIC, AVIF).
	_ "image/gif"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// PreviewResult is the preview index type's output: the object key of a
// generated preview image plus its dimensions. Skipped marks files that have
// no preview and will not get one at their current content — non-images,
// oversized sources, undecodable bytes. StorePreviewResult writes no row for
// those, while the queue still records the job as done, so they are not
// retried until the source changes.
type PreviewResult struct {
	Skipped bool
	Key     string
	Width   int
	Height  int
}

// PreviewObjectStore is the slice of storage.Storage the preview index type
// depends on. Deliberately separate from ObjectStore, which the stat index
// type uses and which needs Stat alone.
type PreviewObjectStore interface {
	Stat(ctx context.Context, key string) (storage.ObjectInfo, error)
	Get(ctx context.Context, key string) (*storage.Object, error)
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
}

// PreviewConfig mirrors config.PreviewConfig so this package stays decoupled
// from application config; the composition root converts by direct cast, as
// cmd/index-stat does for Config.
type PreviewConfig struct {
	MaxDim          int
	Quality         int
	MaxSourceBytes  int64
	MaxSourcePixels int64
}

// PreviewIndexer generates downscaled preview images for image files and
// writes them back to object storage beneath indexPrefix.
type PreviewIndexer struct {
	storage     PreviewObjectStore
	indexPrefix string
	cfg         PreviewConfig
}

// NewPreviewIndexer constructs a PreviewIndexer with its dependencies already
// built by the caller (composition root).
func NewPreviewIndexer(store PreviewObjectStore, indexPrefix string, cfg PreviewConfig) *PreviewIndexer {
	return &PreviewIndexer{storage: store, indexPrefix: indexPrefix, cfg: cfg}
}

// previewKey is the object key a file's preview is written to. It carries no
// file extension on purpose: the output format depends on whether the source
// has an alpha channel, and that can change when a source is replaced. A
// stable, extensionless key means re-indexing always overwrites in place
// rather than stranding the old preview under a different name. Clients learn
// the format from the object's Content-Type, which is what browsers honour.
func (p *PreviewIndexer) previewKey(fileID int64) string {
	return path.Join(p.indexPrefix, "previews", strconv.FormatInt(fileID, 10))
}

// PreviewPrefix returns the object key prefix beneath indexPrefix that all
// previewKey outputs live under (e.g. ".index/previews/"). Exported for the
// preview GC, which scans exactly this prefix; the trailing slash keeps the
// match on a path boundary.
func PreviewPrefix(indexPrefix string) string {
	return path.Join(indexPrefix, "previews") + "/"
}

// Process implements ProcessFunc[PreviewResult]. Only I/O failures return an
// error, so only those are retried with backoff. Anything inherent to the
// file's content returns a skipped result instead: retrying cannot change the
// outcome, and failing would burn every attempt plus its backoff on each
// non-image in the bucket. Safe to run concurrently across any number of pods
// — the output key is a pure function of the file id, so a duplicate run
// overwrites identical bytes.
func (p *PreviewIndexer) Process(ctx context.Context, job Job) (PreviewResult, error) {
	// Defensive: a preview of a preview is never wanted. The crawler already
	// skips this prefix, so this only fires for rows that predate the ignore
	// rule.
	if p.indexPrefix != "" && strings.HasPrefix(job.Key, p.indexPrefix) {
		return skipPreview(job, "key is under the index prefix"), nil
	}

	info, err := p.storage.Stat(ctx, job.Key)
	if err != nil {
		return PreviewResult{}, err
	}
	if !strings.HasPrefix(info.ContentType, "image/") {
		return skipPreview(job, "content type "+info.ContentType+" is not an image"), nil
	}
	if info.Size > p.cfg.MaxSourceBytes {
		return skipPreview(job, fmt.Sprintf("source %d bytes exceeds limit %d", info.Size, p.cfg.MaxSourceBytes)), nil
	}

	obj, err := p.storage.Get(ctx, job.Key)
	if err != nil {
		return PreviewResult{}, err
	}
	defer func() {
		if cerr := obj.Reader.Close(); cerr != nil {
			slog.Warn("close source object", "key", job.Key, "error", cerr)
		}
	}()

	// Bounded read: Stat's size is a hint, not a guarantee. The whole image
	// has to be in memory regardless, since DecodeConfig and Decode are two
	// passes over the same bytes.
	src, err := io.ReadAll(io.LimitReader(obj.Reader, p.cfg.MaxSourceBytes))
	if err != nil {
		return PreviewResult{}, err
	}

	img, err := p.decode(src)
	if err != nil {
		return skipPreview(job, err.Error()), nil
	}

	out, width, height, contentType, err := p.render(img)
	if err != nil {
		return skipPreview(job, err.Error()), nil
	}

	key := p.previewKey(job.FileID)
	if err := p.storage.Put(ctx, key, bytes.NewReader(out), int64(len(out)), contentType); err != nil {
		return PreviewResult{}, err
	}
	return PreviewResult{Key: key, Width: width, Height: height}, nil
}

// skipPreview records why a file gets no preview and returns the sentinel
// result. Logged at warn because a skip is a silent absence in the UI.
func skipPreview(job Job, reason string) PreviewResult {
	slog.Warn("preview skipped", "key", job.Key, "fileID", job.FileID, "reason", reason)
	return PreviewResult{Skipped: true}
}

// decode turns source bytes into an image, rejecting anything too large to
// decode safely. A decoded image costs roughly 4 bytes per pixel, so the pixel
// cap — read from the header before anything is allocated — is what stops a
// maliciously dimensioned file from exhausting memory.
func (p *PreviewIndexer) decode(src []byte) (image.Image, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	if int64(cfg.Width)*int64(cfg.Height) > p.cfg.MaxSourcePixels {
		return nil, fmt.Errorf("source %dx%d exceeds pixel limit %d", cfg.Width, cfg.Height, p.cfg.MaxSourcePixels)
	}
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return img, nil
}

// render scales img to fit inside a MaxDim box and encodes it, returning the
// bytes, the preview's dimensions and its content type.
//
// Opaque sources become JPEG, which is far smaller for photographs. Sources
// with an alpha channel become PNG: JPEG has no alpha, and flattening it would
// bake a background colour into the preview that the frontend could not undo.
func (p *PreviewIndexer) render(img image.Image) ([]byte, int, int, string, error) {
	bounds := img.Bounds()
	width, height := fitWithin(bounds.Dx(), bounds.Dy(), p.cfg.MaxDim)
	if width == 0 || height == 0 {
		return nil, 0, 0, "", errors.New("source has zero width or height")
	}

	var buf bytes.Buffer
	if isOpaque(img) {
		dst := image.NewRGBA(image.Rect(0, 0, width, height))
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, draw.Src, nil)
		if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: p.cfg.Quality}); err != nil {
			return nil, 0, 0, "", err
		}
		return buf.Bytes(), width, height, "image/jpeg", nil
	}

	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, draw.Src, nil)
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, dst); err != nil {
		return nil, 0, 0, "", err
	}
	return buf.Bytes(), width, height, "image/png", nil
}

// opaqueImage is implemented by every concrete image type stdlib and
// golang.org/x/image decoders produce (RGBA, NRGBA, YCbCr, Paletted, Gray,
// ...), but is not part of the image.Image interface itself.
type opaqueImage interface {
	Opaque() bool
}

// isOpaque reports whether img has no transparency. Decoded images always
// satisfy opaqueImage in practice; the true fallback favors the smaller JPEG
// encoding on the rare image.Image that does not.
func isOpaque(img image.Image) bool {
	if o, ok := img.(opaqueImage); ok {
		return o.Opaque()
	}
	return true
}

// fitWithin returns the largest width and height with the same aspect ratio as
// srcW x srcH that fit inside a maxDim box. Sources already inside the box come
// back unchanged: previews are never upscaled and never padded, so the frontend
// receives the true dimensions and decides how to lay them out.
func fitWithin(srcW, srcH, maxDim int) (int, int) {
	if srcW <= 0 || srcH <= 0 || maxDim <= 0 {
		return 0, 0
	}
	if srcW <= maxDim && srcH <= maxDim {
		return srcW, srcH
	}
	if srcW >= srcH {
		return maxDim, max(1, int(math.Round(float64(srcH)*float64(maxDim)/float64(srcW))))
	}
	return max(1, int(math.Round(float64(srcW)*float64(maxDim)/float64(srcH)))), maxDim
}

// StorePreviewResult is the preview index type's StoreFunc: persists the
// result row inside Complete's transaction. Skipped results write nothing —
// the queue row still flips to done, so the file is not reprocessed until its
// marked_at changes.
func StorePreviewResult(ctx context.Context, q *db.Queries, fileID int64, result PreviewResult) error {
	if result.Skipped {
		return nil
	}
	return q.UpsertIndexPreviewResult(ctx, db.UpsertIndexPreviewResultParams{
		FileID:     fileID,
		PreviewKey: result.Key,
		Width:      int32(result.Width),
		Height:     int32(result.Height),
	})
}
