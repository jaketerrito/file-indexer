package indexer

import (
	"context"
	"errors"
	"file-indexer/internal/db"
	"file-indexer/internal/storage"
	"testing"
	"time"

	pb "file-indexer/internal/pb/service/v1"

	"github.com/stretchr/testify/mock"
)

func TestIndex(t *testing.T) {
	now := time.Now()
	info := storage.ObjectInfo{
		Key:          "obj-key",
		Size:         100,
		ContentType:  "text/plain",
		LastModified: now,
	}

	store := NewMockObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "obj-key").Return(info, nil)

	queries := NewMockFileIndex(t)
	queries.EXPECT().
		CreateFile(mock.Anything, mock.MatchedBy(func(arg db.CreateFileParams) bool {
			return arg.Key == "obj-key" &&
				arg.ContentType.String == "text/plain" && arg.ContentType.Valid &&
				arg.SizeBytes.Int64 == 100 && arg.SizeBytes.Valid &&
				arg.CreatedAt.Time.Equal(now) && arg.CreatedAt.Valid &&
				arg.UpdatedAt.Time.Equal(now) && arg.UpdatedAt.Valid
		})).
		Return(db.File{ID: 1, Key: "obj-key"}, nil)

	srv := IndexerServer{storage: store, queries: queries}

	resp, err := srv.Index(context.Background(), &pb.IndexRequest{
		Ref: &pb.FileRef{Key: "obj-key"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != "OK" {
		t.Errorf("Status = %q, want %q", resp.Status, "OK")
	}
}

func TestIndexStatError(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "missing").Return(storage.ObjectInfo{}, errors.New("not found"))

	queries := NewMockFileIndex(t)
	// CreateFile must never be called when Stat fails.

	srv := IndexerServer{storage: store, queries: queries}

	_, err := srv.Index(context.Background(), &pb.IndexRequest{
		Ref: &pb.FileRef{Key: "missing"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	queries.AssertNotCalled(t, "CreateFile", mock.Anything, mock.Anything)
}

func TestIndexCreateFileError(t *testing.T) {
	info := storage.ObjectInfo{Key: "obj-key", Size: 1, ContentType: "text/plain", LastModified: time.Now()}

	store := NewMockObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "obj-key").Return(info, nil)

	queries := NewMockFileIndex(t)
	queries.EXPECT().CreateFile(mock.Anything, mock.Anything).Return(db.File{}, errors.New("db error"))

	srv := IndexerServer{storage: store, queries: queries}

	_, err := srv.Index(context.Background(), &pb.IndexRequest{
		Ref: &pb.FileRef{Key: "obj-key"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
