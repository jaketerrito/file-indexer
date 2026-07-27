package indexer

import (
	"bytes"
	"context"
	"errors"
	"file-indexer/internal/storage"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"testing"

	"github.com/stretchr/testify/mock"
)

// solidPNG encodes a w x h PNG. When opaque is false the image keeps a
// transparent pixel, which is what routes it to the PNG output path instead
// of JPEG.
func solidPNG(t *testing.T, w, h int, opaque bool) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fill := color.RGBA{R: 10, G: 200, B: 90, A: 255}
	draw.Draw(img, img.Bounds(), &image.Uniform{C: fill}, image.Point{}, draw.Src)
	if !opaque {
		img.Set(0, 0, color.RGBA{})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	return buf.Bytes()
}

func sourceObject(body []byte, contentType string) *storage.Object {
	return &storage.Object{
		ObjectInfo: storage.ObjectInfo{ContentType: contentType, Size: int64(len(body))},
		Reader:     io.NopCloser(bytes.NewReader(body)),
	}
}

func defaultPreviewConfig() PreviewConfig {
	return PreviewConfig{
		MaxDim:          320,
		Quality:         80,
		MaxSourceBytes:  64 << 20,
		MaxSourcePixels: 40_000_000,
	}
}

func TestPreviewProcessOpaqueSourceEncodesJPEG(t *testing.T) {
	body := solidPNG(t, 800, 400, true)

	store := NewMockPreviewObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "img.png").
		Return(storage.ObjectInfo{ContentType: "image/png", Size: int64(len(body))}, nil)
	store.EXPECT().Get(mock.Anything, "img.png").Return(sourceObject(body, "image/png"), nil)
	store.EXPECT().
		Put(mock.Anything, ".index/previews/42", mock.Anything, mock.Anything, "image/jpeg").
		RunAndReturn(func(_ context.Context, _ string, r io.Reader, size int64, _ string) error {
			out, err := io.ReadAll(r)
			if err != nil {
				t.Fatalf("read Put body: %v", err)
			}
			if int64(len(out)) != size {
				t.Errorf("Put size = %d, want %d", size, len(out))
			}
			cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
			if err != nil {
				t.Fatalf("decode preview: %v", err)
			}
			if cfg.Width != 320 || cfg.Height != 160 {
				t.Errorf("preview dims = %dx%d, want 320x160", cfg.Width, cfg.Height)
			}
			return nil
		})

	p := NewPreviewIndexer(store, ".index/", defaultPreviewConfig())
	got, err := p.Process(context.Background(), Job{FileID: 42, Key: "img.png"})
	if err != nil {
		t.Fatal(err)
	}
	want := PreviewResult{Key: ".index/previews/42", Width: 320, Height: 160}
	if got != want {
		t.Errorf("Process = %+v, want %+v", got, want)
	}
}

func TestPreviewProcessTransparentSourceEncodesPNG(t *testing.T) {
	body := solidPNG(t, 400, 800, false)

	store := NewMockPreviewObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "img.png").
		Return(storage.ObjectInfo{ContentType: "image/png", Size: int64(len(body))}, nil)
	store.EXPECT().Get(mock.Anything, "img.png").Return(sourceObject(body, "image/png"), nil)
	store.EXPECT().
		Put(mock.Anything, ".index/previews/7", mock.Anything, mock.Anything, "image/png").
		Return(nil)

	p := NewPreviewIndexer(store, ".index/", defaultPreviewConfig())
	got, err := p.Process(context.Background(), Job{FileID: 7, Key: "img.png"})
	if err != nil {
		t.Fatal(err)
	}
	want := PreviewResult{Key: ".index/previews/7", Width: 160, Height: 320}
	if got != want {
		t.Errorf("Process = %+v, want %+v", got, want)
	}
}

func TestPreviewProcessDoesNotUpscale(t *testing.T) {
	body := solidPNG(t, 100, 50, true)

	store := NewMockPreviewObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "small.png").
		Return(storage.ObjectInfo{ContentType: "image/png", Size: int64(len(body))}, nil)
	store.EXPECT().Get(mock.Anything, "small.png").Return(sourceObject(body, "image/png"), nil)
	store.EXPECT().
		Put(mock.Anything, mock.Anything, mock.Anything, mock.Anything, "image/jpeg").
		Return(nil)

	p := NewPreviewIndexer(store, ".index/", defaultPreviewConfig())
	got, err := p.Process(context.Background(), Job{FileID: 1, Key: "small.png"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != 100 || got.Height != 50 {
		t.Errorf("dims = %dx%d, want 100x50 (no upscale)", got.Width, got.Height)
	}
}

func TestPreviewProcessSkipsNonImage(t *testing.T) {
	store := NewMockPreviewObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "doc.pdf").
		Return(storage.ObjectInfo{ContentType: "application/pdf", Size: 10}, nil)

	p := NewPreviewIndexer(store, ".index/", defaultPreviewConfig())
	got, err := p.Process(context.Background(), Job{FileID: 1, Key: "doc.pdf"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Skipped {
		t.Error("expected Skipped = true")
	}
	store.AssertNotCalled(t, "Get", mock.Anything, mock.Anything)
	store.AssertNotCalled(t, "Put", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestPreviewProcessSkipsOversizeSource(t *testing.T) {
	cfg := defaultPreviewConfig()
	cfg.MaxSourceBytes = 100

	store := NewMockPreviewObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "big.png").
		Return(storage.ObjectInfo{ContentType: "image/png", Size: 101}, nil)

	p := NewPreviewIndexer(store, ".index/", cfg)
	got, err := p.Process(context.Background(), Job{FileID: 1, Key: "big.png"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Skipped {
		t.Error("expected Skipped = true")
	}
	store.AssertNotCalled(t, "Get", mock.Anything, mock.Anything)
}

func TestPreviewProcessSkipsPixelBomb(t *testing.T) {
	body := solidPNG(t, 800, 400, true)
	cfg := defaultPreviewConfig()
	cfg.MaxSourcePixels = 100

	store := NewMockPreviewObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "bomb.png").
		Return(storage.ObjectInfo{ContentType: "image/png", Size: int64(len(body))}, nil)
	store.EXPECT().Get(mock.Anything, "bomb.png").Return(sourceObject(body, "image/png"), nil)

	p := NewPreviewIndexer(store, ".index/", cfg)
	got, err := p.Process(context.Background(), Job{FileID: 1, Key: "bomb.png"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Skipped {
		t.Error("expected Skipped = true")
	}
	store.AssertNotCalled(t, "Put", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestPreviewProcessSkipsUndecodableBytes(t *testing.T) {
	body := []byte("not an image")

	store := NewMockPreviewObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "fake.png").
		Return(storage.ObjectInfo{ContentType: "image/png", Size: int64(len(body))}, nil)
	store.EXPECT().Get(mock.Anything, "fake.png").Return(sourceObject(body, "image/png"), nil)

	p := NewPreviewIndexer(store, ".index/", defaultPreviewConfig())
	got, err := p.Process(context.Background(), Job{FileID: 1, Key: "fake.png"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Skipped {
		t.Error("expected Skipped = true")
	}
	store.AssertNotCalled(t, "Put", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestPreviewProcessSkipsKeyUnderIndexPrefix(t *testing.T) {
	store := NewMockPreviewObjectStore(t)

	p := NewPreviewIndexer(store, ".index/", defaultPreviewConfig())
	got, err := p.Process(context.Background(), Job{FileID: 1, Key: ".index/previews/7"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Skipped {
		t.Error("expected Skipped = true")
	}
	store.AssertNotCalled(t, "Stat", mock.Anything, mock.Anything)
}

func TestPreviewProcessStatError(t *testing.T) {
	store := NewMockPreviewObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "missing").Return(storage.ObjectInfo{}, errors.New("not found"))

	p := NewPreviewIndexer(store, ".index/", defaultPreviewConfig())
	got, err := p.Process(context.Background(), Job{FileID: 1, Key: "missing"})
	if err == nil {
		t.Fatal("expected error")
	}
	if got.Skipped {
		t.Error("Skipped should be false on I/O error")
	}
}

func TestPreviewProcessGetError(t *testing.T) {
	store := NewMockPreviewObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "img.png").
		Return(storage.ObjectInfo{ContentType: "image/png", Size: 10}, nil)
	store.EXPECT().Get(mock.Anything, "img.png").Return(nil, errors.New("get failed"))

	p := NewPreviewIndexer(store, ".index/", defaultPreviewConfig())
	if _, err := p.Process(context.Background(), Job{FileID: 1, Key: "img.png"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestPreviewProcessPutError(t *testing.T) {
	body := solidPNG(t, 100, 100, true)

	store := NewMockPreviewObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "img.png").
		Return(storage.ObjectInfo{ContentType: "image/png", Size: int64(len(body))}, nil)
	store.EXPECT().Get(mock.Anything, "img.png").Return(sourceObject(body, "image/png"), nil)
	store.EXPECT().
		Put(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(errors.New("put failed"))

	p := NewPreviewIndexer(store, ".index/", defaultPreviewConfig())
	if _, err := p.Process(context.Background(), Job{FileID: 1, Key: "img.png"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestFitWithin(t *testing.T) {
	cases := []struct {
		name         string
		srcW, srcH   int
		maxDim       int
		wantW, wantH int
	}{
		{"landscape", 800, 400, 320, 320, 160},
		{"portrait", 400, 800, 320, 160, 320},
		{"square", 500, 500, 320, 320, 320},
		{"already small", 100, 50, 320, 100, 50},
		{"zero width", 0, 50, 320, 0, 0},
		{"zero height", 50, 0, 320, 0, 0},
		{"extreme aspect ratio", 10000, 1, 320, 320, 1},
		{"extreme aspect ratio tall", 1, 10000, 320, 1, 320},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotW, gotH := fitWithin(c.srcW, c.srcH, c.maxDim)
			if gotW != c.wantW || gotH != c.wantH {
				t.Errorf("fitWithin(%d, %d, %d) = (%d, %d), want (%d, %d)",
					c.srcW, c.srcH, c.maxDim, gotW, gotH, c.wantW, c.wantH)
			}
		})
	}
}

func TestStorePreviewResultSkipped(t *testing.T) {
	// A nil *db.Queries would panic if StorePreviewResult tried to use it;
	// passing one proves the skip path returns before that ever happens.
	if err := StorePreviewResult(context.Background(), nil, 1, PreviewResult{Skipped: true}); err != nil {
		t.Errorf("StorePreviewResult(skipped) = %v, want nil", err)
	}
}
