package indexer

import (
	"context"
	"errors"
	"file-indexer/internal/storage"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
)

func TestStatIndexerProcess(t *testing.T) {
	now := time.Now()
	info := storage.ObjectInfo{
		Key:          "obj-key",
		Size:         100,
		ContentType:  "text/plain",
		LastModified: now,
	}

	store := NewMockObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "obj-key").Return(info, nil)

	s := NewStatIndexer(store)
	got, err := s.Process(context.Background(), Job{FileID: 42, Key: "obj-key"})
	if err != nil {
		t.Fatal(err)
	}
	want := StatResult{ContentType: "text/plain", SizeBytes: 100, LastModified: now}
	if got != want {
		t.Errorf("Process = %+v, want %+v", got, want)
	}
}

func TestStatIndexerProcessStatError(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "missing").Return(storage.ObjectInfo{}, errors.New("not found"))

	s := NewStatIndexer(store)
	if _, err := s.Process(context.Background(), Job{FileID: 1, Key: "missing"}); err == nil {
		t.Fatal("expected error")
	}
}
