package previewgc

import (
	"context"
	"errors"
	"file-indexer/internal/service/indexer"
	"file-indexer/internal/storage"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
)

var fixedNow = time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

func obj(key string, age time.Duration) storage.ObjectInfo {
	return storage.ObjectInfo{Key: key, Size: 1, LastModified: fixedNow.Add(age)}
}

func walkOver(infos ...storage.ObjectInfo) func(context.Context, func(storage.ObjectInfo) error) error {
	return func(_ context.Context, fn func(storage.ObjectInfo) error) error {
		for _, info := range infos {
			if err := fn(info); err != nil {
				return err
			}
		}
		return nil
	}
}

func TestGC_NoOrphans(t *testing.T) {
	store := NewMockObjectStore(t)
	previews := NewMockPreviewStore(t)

	previews.EXPECT().ListIndexPreviewKeys(mock.Anything).Return([]string{".index/previews/7"}, nil)
	store.EXPECT().Walk(mock.Anything, mock.Anything).
		RunAndReturn(walkOver(obj(".index/previews/7", -2*time.Hour)))

	gc := New(store, previews, ".index/")
	gc.now = func() time.Time { return fixedNow }
	if err := gc.Run(context.Background()); err != nil {
		t.Fatalf("Run = %v; want nil", err)
	}
}

func TestGC_OldOrphanDeleted(t *testing.T) {
	store := NewMockObjectStore(t)
	previews := NewMockPreviewStore(t)

	previews.EXPECT().ListIndexPreviewKeys(mock.Anything).Return(nil, nil)
	store.EXPECT().Walk(mock.Anything, mock.Anything).
		RunAndReturn(walkOver(obj(".index/previews/7", -2*time.Hour)))
	store.EXPECT().DeleteMany(mock.Anything, []string{".index/previews/7"}).Return(nil)

	gc := New(store, previews, ".index/")
	gc.now = func() time.Time { return fixedNow }
	if err := gc.Run(context.Background()); err != nil {
		t.Fatalf("Run = %v; want nil", err)
	}
}

func TestGC_YoungOrphanSkipped(t *testing.T) {
	store := NewMockObjectStore(t)
	previews := NewMockPreviewStore(t)

	previews.EXPECT().ListIndexPreviewKeys(mock.Anything).Return(nil, nil)
	store.EXPECT().Walk(mock.Anything, mock.Anything).
		RunAndReturn(walkOver(obj(".index/previews/7", -30*time.Minute)))

	gc := New(store, previews, ".index/")
	gc.now = func() time.Time { return fixedNow }
	if err := gc.Run(context.Background()); err != nil {
		t.Fatalf("Run = %v; want nil", err)
	}
}

func TestGC_OutsidePrefixIgnored(t *testing.T) {
	store := NewMockObjectStore(t)
	previews := NewMockPreviewStore(t)

	previews.EXPECT().ListIndexPreviewKeys(mock.Anything).Return(nil, nil)
	store.EXPECT().Walk(mock.Anything, mock.Anything).
		RunAndReturn(walkOver(obj("docs/a.jpg", -2*time.Hour)))

	gc := New(store, previews, ".index/")
	gc.now = func() time.Time { return fixedNow }
	if err := gc.Run(context.Background()); err != nil {
		t.Fatalf("Run = %v; want nil", err)
	}
}

func TestGC_DBErrorNoWalkNoDelete(t *testing.T) {
	store := NewMockObjectStore(t)
	previews := NewMockPreviewStore(t)

	previews.EXPECT().ListIndexPreviewKeys(mock.Anything).Return(nil, errors.New("db down"))

	gc := New(store, previews, ".index/")
	gc.now = func() time.Time { return fixedNow }
	if err := gc.Run(context.Background()); err == nil {
		t.Fatal("expected error from DB failure")
	}
}

func TestGC_EmptyDBDeletesAll(t *testing.T) {
	store := NewMockObjectStore(t)
	previews := NewMockPreviewStore(t)

	previews.EXPECT().ListIndexPreviewKeys(mock.Anything).Return(nil, nil)
	store.EXPECT().Walk(mock.Anything, mock.Anything).
		RunAndReturn(walkOver(obj(".index/previews/7", -2*time.Hour)))
	store.EXPECT().DeleteMany(mock.Anything, []string{".index/previews/7"}).Return(nil)

	gc := New(store, previews, ".index/")
	gc.now = func() time.Time { return fixedNow }
	if err := gc.Run(context.Background()); err != nil {
		t.Fatalf("Run = %v; want nil", err)
	}
}

func TestGC_WalkErrorNoDelete(t *testing.T) {
	store := NewMockObjectStore(t)
	previews := NewMockPreviewStore(t)

	previews.EXPECT().ListIndexPreviewKeys(mock.Anything).Return([]string{".index/previews/7"}, nil)
	store.EXPECT().Walk(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, fn func(storage.ObjectInfo) error) error {
			_ = fn(obj(".index/previews/1", -2*time.Hour))
			return errors.New("walk failed")
		})

	gc := New(store, previews, ".index/")
	gc.now = func() time.Time { return fixedNow }
	if err := gc.Run(context.Background()); err == nil {
		t.Fatal("expected error from walk failure")
	}
}

func TestGC_DeleteManyError(t *testing.T) {
	store := NewMockObjectStore(t)
	previews := NewMockPreviewStore(t)

	previews.EXPECT().ListIndexPreviewKeys(mock.Anything).Return(nil, nil)
	store.EXPECT().Walk(mock.Anything, mock.Anything).
		RunAndReturn(walkOver(obj(".index/previews/7", -2*time.Hour)))
	store.EXPECT().DeleteMany(mock.Anything, []string{".index/previews/7"}).Return(errors.New("s3 error"))

	gc := New(store, previews, ".index/")
	gc.now = func() time.Time { return fixedNow }
	if err := gc.Run(context.Background()); err == nil {
		t.Fatal("expected error from delete failure")
	}
}

func TestGC_MultipleOrphansSingleCall(t *testing.T) {
	store := NewMockObjectStore(t)
	previews := NewMockPreviewStore(t)

	previews.EXPECT().ListIndexPreviewKeys(mock.Anything).Return([]string{".index/previews/1"}, nil)
	store.EXPECT().Walk(mock.Anything, mock.Anything).
		RunAndReturn(walkOver(
			obj(".index/previews/1", -2*time.Hour),
			obj(".index/previews/2", -2*time.Hour),
			obj(".index/previews/3", -2*time.Hour),
		))
	store.EXPECT().DeleteMany(mock.Anything, []string{".index/previews/2", ".index/previews/3"}).Return(nil)

	gc := New(store, previews, ".index/")
	gc.now = func() time.Time { return fixedNow }
	if err := gc.Run(context.Background()); err != nil {
		t.Fatalf("Run = %v; want nil", err)
	}
}

func TestPreviewPrefixMatchesWriter(t *testing.T) {
	for _, prefix := range []string{"", ".index/", "previews/"} {
		p := indexer.PreviewPrefix(prefix)
		if !strings.HasSuffix(p, "/") {
			t.Errorf("PreviewPrefix(%q) = %q; want trailing slash", prefix, p)
		}
		want := path.Join(prefix, "previews", "42")
		if !strings.HasPrefix(want, p) {
			t.Errorf("preview key %q does not start with prefix %q", want, p)
		}
	}
}
