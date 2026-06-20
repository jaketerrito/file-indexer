package files

import (
	"context"
	"errors"
	"file-indexer/internal/db"
	"testing"
	"time"

	pb "file-indexer/internal/pb/service/v1"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
)

func TestGetFileInfo(t *testing.T) {
	now := time.Now()
	want := db.File{
		ID: 1, Key: "obj-key",
		ContentType: pgtype.Text{String: "text/plain", Valid: true},
		SizeBytes:   pgtype.Int8{Int64: 100, Valid: true},
		CreatedAt:   pgtype.Timestamptz{Time: now, Valid: true},
		UpdatedAt:   pgtype.Timestamptz{Time: now, Valid: true},
	}
	queries := NewMockFileIndex(t)
	queries.EXPECT().GetFile(mock.Anything, int64(1)).Return(want, nil)

	srv := FilesServer{queries: queries}

	resp, err := srv.GetFileInfo(context.Background(), &pb.GetFileInfoRequest{Id: 1})
	if err != nil {
		t.Fatal(err)
	}
	if resp.File.Id != 1 || resp.File.Key != "obj-key" {
		t.Errorf("GetFileInfo = %+v", resp.File)
	}
}

func TestGetFileInfoNotFound(t *testing.T) {
	queries := NewMockFileIndex(t)
	queries.EXPECT().GetFile(mock.Anything, int64(99)).Return(db.File{}, errors.New("not found"))

	srv := FilesServer{queries: queries}

	_, err := srv.GetFileInfo(context.Background(), &pb.GetFileInfoRequest{Id: 99})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDeleteFile(t *testing.T) {
	file := db.File{ID: 1, Key: "obj-key"}

	queries := NewMockFileIndex(t)
	queries.EXPECT().GetFile(mock.Anything, int64(1)).Return(file, nil)
	queries.EXPECT().DeleteFile(mock.Anything, int64(1)).Return(file, nil)

	storage := NewMockObjectStore(t)
	storage.EXPECT().Delete(mock.Anything, "obj-key").Return(nil)

	srv := FilesServer{queries: queries, storage: storage}

	_, err := srv.DeleteFile(context.Background(), &pb.DeleteFileRequest{Id: 1})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDeleteFileStorageError(t *testing.T) {
	file := db.File{ID: 1, Key: "obj-key"}

	queries := NewMockFileIndex(t)
	queries.EXPECT().GetFile(mock.Anything, int64(1)).Return(file, nil)
	// DeleteFile must never be called when storage deletion fails.

	storage := NewMockObjectStore(t)
	storage.EXPECT().Delete(mock.Anything, "obj-key").Return(errors.New("s3 error"))

	srv := FilesServer{queries: queries, storage: storage}

	_, err := srv.DeleteFile(context.Background(), &pb.DeleteFileRequest{Id: 1})
	if err == nil {
		t.Fatal("expected error")
	}
	queries.AssertNotCalled(t, "DeleteFile", mock.Anything, mock.Anything)
}

func TestDbFileToProto(t *testing.T) {
	now := time.Now()
	f := db.File{
		ID: 1, Key: "k",
		ContentType: pgtype.Text{String: "image/png", Valid: true},
		SizeBytes:   pgtype.Int8{Int64: 200, Valid: true},
		CreatedAt:   pgtype.Timestamptz{Time: now, Valid: true},
		UpdatedAt:   pgtype.Timestamptz{Time: now, Valid: true},
	}
	pf := dbFileToProto(f)
	if pf.Id != 1 || pf.Key != "k" || pf.ContentType != "image/png" || pf.SizeBytes != 200 {
		t.Errorf("dbFileToProto = %+v", pf)
	}
	if !pf.CreatedAt.AsTime().Equal(now) {
		t.Errorf("CreatedAt mismatch: got %v, want %v", pf.CreatedAt.AsTime(), now)
	}
	if !pf.UpdatedAt.AsTime().Equal(now) {
		t.Errorf("UpdatedAt mismatch")
	}
}

func TestDbFileToProtoNullFields(t *testing.T) {
	f := db.File{
		ID: 2, Key: "nulls",
	}
	pf := dbFileToProto(f)
	if pf.Id != 2 || pf.ContentType != "" || pf.SizeBytes != 0 {
		t.Errorf("dbFileToProto = %+v", pf)
	}
}

func TestGetDownloadURL(t *testing.T) {
	files := []db.File{
		{ID: 1, Key: "obj-1"},
		{ID: 2, Key: "obj-2"},
	}

	queries := NewMockFileIndex(t)
	queries.EXPECT().GetFilesByIDs(mock.Anything, []int64{1, 2}).Return(files, nil)

	storage := NewMockObjectStore(t)
	storage.EXPECT().GetURL(mock.Anything, "obj-1").Return("https://example.com/obj-1", nil)
	storage.EXPECT().GetURL(mock.Anything, "obj-2").Return("https://example.com/obj-2", nil)

	srv := FilesServer{queries: queries, storage: storage}

	resp, err := srv.GetDownloadURL(context.Background(), &pb.GetDownloadURLRequest{Ids: []int64{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.DownloadUrls) != 2 {
		t.Fatalf("got %d urls, want 2", len(resp.DownloadUrls))
	}
}
