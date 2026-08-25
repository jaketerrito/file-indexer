package files

import (
	"context"
	"errors"
	"file-indexer/internal/db"
	"fmt"
	"net"
	"testing"
	"time"

	pb "file-indexer/internal/pb/service/v1"
	objstore "file-indexer/internal/storage"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestNew(t *testing.T) {
	queries := NewMockFileIndex(t)
	store := NewMockObjectStore(t)

	srv := New(":1234", store, queries, ".index/")

	if srv == nil {
		t.Fatal("New returned nil")
	}
	if srv.addr != ":1234" {
		t.Errorf("addr = %q, want %q", srv.addr, ":1234")
	}
	if srv.storage != store || srv.queries != queries {
		t.Error("New did not wire dependencies")
	}
	if srv.indexPrefix != ".index/" {
		t.Errorf("indexPrefix = %q, want %q", srv.indexPrefix, ".index/")
	}
}

func TestGetFileInfo(t *testing.T) {
	now := time.Now()
	want := db.FileInfo{
		ID: 1, Key: "obj-key",
		ContentType:  pgtype.Text{String: "text/plain", Valid: true},
		SizeBytes:    pgtype.Int8{Int64: 100, Valid: true},
		CreatedAt:    pgtype.Timestamptz{Time: now, Valid: true},
		LastModified: pgtype.Timestamptz{Time: now, Valid: true},
	}
	queries := NewMockFileIndex(t)
	queries.EXPECT().GetFile(mock.Anything, int64(1)).Return(want, nil)
	queries.EXPECT().GetIndexExifResult(mock.Anything, int64(1)).Return(db.IndexExifResult{}, pgx.ErrNoRows)

	srv := FilesServer{queries: queries}

	resp, err := srv.GetFileInfo(context.Background(), &pb.GetFileInfoRequest{Id: 1})
	if err != nil {
		t.Fatal(err)
	}
	if resp.File.Id != 1 || resp.File.Key != "obj-key" {
		t.Errorf("GetFileInfo = %+v", resp.File)
	}
	if resp.File.Exif != nil {
		t.Errorf("Exif = %+v, want nil (no exif row)", resp.File.Exif)
	}
}

func TestGetFileInfoWithExif(t *testing.T) {
	now := time.Now()
	want := db.FileInfo{
		ID: 1, Key: "obj-key",
		ContentType: pgtype.Text{String: "image/jpeg", Valid: true},
	}
	exif := db.IndexExifResult{
		FileID:      1,
		CameraMake:  pgtype.Text{String: "Canon", Valid: true},
		CameraModel: pgtype.Text{String: "EOS R5", Valid: true},
		Iso:         pgtype.Int4{Int32: 400, Valid: true},
		TakenAt:     pgtype.Timestamp{Time: now, Valid: true},
		HasExif:     true,
	}
	queries := NewMockFileIndex(t)
	queries.EXPECT().GetFile(mock.Anything, int64(1)).Return(want, nil)
	queries.EXPECT().GetIndexExifResult(mock.Anything, int64(1)).Return(exif, nil)

	srv := FilesServer{queries: queries}

	resp, err := srv.GetFileInfo(context.Background(), &pb.GetFileInfoRequest{Id: 1})
	if err != nil {
		t.Fatal(err)
	}
	if resp.File.Exif == nil {
		t.Fatal("Exif = nil, want populated")
	}
	if resp.File.Exif.GetCameraMake() != "Canon" || resp.File.Exif.GetCameraModel() != "EOS R5" {
		t.Errorf("Exif = %+v", resp.File.Exif)
	}
	if resp.File.Exif.GetIso() != 400 {
		t.Errorf("Iso = %d, want 400", resp.File.Exif.GetIso())
	}
	if !resp.File.Exif.GetTakenAt().AsTime().Equal(now) {
		t.Errorf("TakenAt mismatch")
	}
}

func TestGetFileInfoExifQueryError(t *testing.T) {
	queries := NewMockFileIndex(t)
	queries.EXPECT().GetFile(mock.Anything, int64(1)).Return(db.FileInfo{ID: 1, Key: "obj-key"}, nil)
	queries.EXPECT().GetIndexExifResult(mock.Anything, int64(1)).Return(db.IndexExifResult{}, errors.New("db error"))

	srv := FilesServer{queries: queries}

	_, err := srv.GetFileInfo(context.Background(), &pb.GetFileInfoRequest{Id: 1})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGetFileInfoNotFound(t *testing.T) {
	queries := NewMockFileIndex(t)
	queries.EXPECT().GetFile(mock.Anything, int64(99)).Return(db.FileInfo{}, errors.New("not found"))

	srv := FilesServer{queries: queries}

	_, err := srv.GetFileInfo(context.Background(), &pb.GetFileInfoRequest{Id: 99})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDeleteFile(t *testing.T) {
	queries := NewMockFileIndex(t)
	queries.EXPECT().GetFile(mock.Anything, int64(1)).Return(db.FileInfo{ID: 1, Key: "obj-key"}, nil)
	queries.EXPECT().DeleteFileWithDirectories(mock.Anything, int64(1)).Return(db.File{ID: 1, Key: "obj-key"}, nil)

	storage := NewMockObjectStore(t)
	storage.EXPECT().Delete(mock.Anything, "obj-key").Return(nil)

	srv := FilesServer{queries: queries, storage: storage}

	_, err := srv.DeleteFile(context.Background(), &pb.DeleteFileRequest{Id: 1})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDeleteFileRemovesPreview(t *testing.T) {
	queries := NewMockFileIndex(t)
	queries.EXPECT().GetFile(mock.Anything, int64(1)).Return(db.FileInfo{
		ID: 1, Key: "obj-key",
		PreviewKey: pgtype.Text{String: ".index/previews/1", Valid: true},
	}, nil)
	queries.EXPECT().DeleteFileWithDirectories(mock.Anything, int64(1)).Return(db.File{ID: 1, Key: "obj-key"}, nil)

	storage := NewMockObjectStore(t)
	storage.EXPECT().Delete(mock.Anything, "obj-key").Return(nil)
	storage.EXPECT().Delete(mock.Anything, ".index/previews/1").Return(nil)

	srv := FilesServer{queries: queries, storage: storage}

	_, err := srv.DeleteFile(context.Background(), &pb.DeleteFileRequest{Id: 1})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDeleteFileSurvivesPreviewDeleteFailure(t *testing.T) {
	queries := NewMockFileIndex(t)
	queries.EXPECT().GetFile(mock.Anything, int64(1)).Return(db.FileInfo{
		ID: 1, Key: "obj-key",
		PreviewKey: pgtype.Text{String: ".index/previews/1", Valid: true},
	}, nil)
	// The file delete must still proceed even though the preview delete fails.
	queries.EXPECT().DeleteFileWithDirectories(mock.Anything, int64(1)).Return(db.File{ID: 1, Key: "obj-key"}, nil)

	storage := NewMockObjectStore(t)
	storage.EXPECT().Delete(mock.Anything, "obj-key").Return(nil)
	storage.EXPECT().Delete(mock.Anything, ".index/previews/1").Return(errors.New("s3 error"))

	srv := FilesServer{queries: queries, storage: storage}

	_, err := srv.DeleteFile(context.Background(), &pb.DeleteFileRequest{Id: 1})
	if err != nil {
		t.Fatalf("DeleteFile should survive a preview delete failure, got: %v", err)
	}
}

func TestDeleteFileStorageError(t *testing.T) {
	queries := NewMockFileIndex(t)
	queries.EXPECT().GetFile(mock.Anything, int64(1)).Return(db.FileInfo{ID: 1, Key: "obj-key"}, nil)
	// DeleteFile must never be called when storage deletion fails.

	storage := NewMockObjectStore(t)
	storage.EXPECT().Delete(mock.Anything, "obj-key").Return(errors.New("s3 error"))

	srv := FilesServer{queries: queries, storage: storage}

	_, err := srv.DeleteFile(context.Background(), &pb.DeleteFileRequest{Id: 1})
	if err == nil {
		t.Fatal("expected error")
	}
	queries.AssertNotCalled(t, "DeleteFileWithDirectories", mock.Anything, mock.Anything)
}

func TestDeleteFileGetFileError(t *testing.T) {
	queries := NewMockFileIndex(t)
	queries.EXPECT().GetFile(mock.Anything, int64(7)).Return(db.FileInfo{}, errors.New("not found"))

	storage := NewMockObjectStore(t)

	srv := FilesServer{queries: queries, storage: storage}

	_, err := srv.DeleteFile(context.Background(), &pb.DeleteFileRequest{Id: 7})
	if err == nil {
		t.Fatal("expected error")
	}
	storage.AssertNotCalled(t, "Delete", mock.Anything, mock.Anything)
}

func TestDeleteFileDBDeleteError(t *testing.T) {
	queries := NewMockFileIndex(t)
	queries.EXPECT().GetFile(mock.Anything, int64(1)).Return(db.FileInfo{ID: 1, Key: "obj-key"}, nil)
	queries.EXPECT().DeleteFileWithDirectories(mock.Anything, int64(1)).Return(db.File{}, errors.New("db error"))

	storage := NewMockObjectStore(t)
	storage.EXPECT().Delete(mock.Anything, "obj-key").Return(nil)

	srv := FilesServer{queries: queries, storage: storage}

	_, err := srv.DeleteFile(context.Background(), &pb.DeleteFileRequest{Id: 1})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDbFileToProto(t *testing.T) {
	now := time.Now()
	f := db.FileInfo{
		ID: 1, Key: "k",
		ContentType:   pgtype.Text{String: "image/png", Valid: true},
		SizeBytes:     pgtype.Int8{Int64: 200, Valid: true},
		CreatedAt:     pgtype.Timestamptz{Time: now, Valid: true},
		LastModified:  pgtype.Timestamptz{Time: now, Valid: true},
		PreviewKey:    pgtype.Text{String: ".index/previews/1", Valid: true},
		PreviewWidth:  pgtype.Int4{Int32: 320, Valid: true},
		PreviewHeight: pgtype.Int4{Int32: 160, Valid: true},
	}
	pf := dbFileToProto(f)
	if pf.Id != 1 || pf.Key != "k" || pf.ContentType != "image/png" || pf.SizeBytes != 200 {
		t.Errorf("dbFileToProto = %+v", pf)
	}
	if pf.PreviewKey != ".index/previews/1" || pf.PreviewWidth != 320 || pf.PreviewHeight != 160 {
		t.Errorf("preview fields = %+v", pf)
	}
	if !pf.CreatedAt.AsTime().Equal(now) {
		t.Errorf("CreatedAt mismatch: got %v, want %v", pf.CreatedAt.AsTime(), now)
	}
	if !pf.UpdatedAt.AsTime().Equal(now) {
		t.Errorf("UpdatedAt mismatch")
	}
}

func TestDbFileToProtoNullFields(t *testing.T) {
	f := db.FileInfo{
		ID: 2, Key: "nulls",
	}
	pf := dbFileToProto(f)
	if pf.Id != 2 || pf.ContentType != "" || pf.SizeBytes != 0 {
		t.Errorf("dbFileToProto = %+v", pf)
	}
	if pf.PreviewKey != "" || pf.PreviewWidth != 0 || pf.PreviewHeight != 0 {
		t.Errorf("preview fields = %+v, want zero values for NULLs", pf)
	}
	if pf.CreatedAt != nil || pf.UpdatedAt != nil {
		t.Errorf("timestamps = (%v, %v), want unset for NULLs", pf.CreatedAt, pf.UpdatedAt)
	}
}

func TestGetDownloadURL(t *testing.T) {
	files := []db.FileInfo{
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

func TestGetDownloadURLQueryError(t *testing.T) {
	queries := NewMockFileIndex(t)
	queries.EXPECT().GetFilesByIDs(mock.Anything, []int64{1}).Return(nil, errors.New("db error"))

	storage := NewMockObjectStore(t)

	srv := FilesServer{queries: queries, storage: storage}

	_, err := srv.GetDownloadURL(context.Background(), &pb.GetDownloadURLRequest{Ids: []int64{1}})
	if err == nil {
		t.Fatal("expected error")
	}
	storage.AssertNotCalled(t, "GetURL", mock.Anything, mock.Anything)
}

func TestGetDownloadURLStorageError(t *testing.T) {
	files := []db.FileInfo{{ID: 1, Key: "obj-1"}}

	queries := NewMockFileIndex(t)
	queries.EXPECT().GetFilesByIDs(mock.Anything, []int64{1}).Return(files, nil)

	storage := NewMockObjectStore(t)
	storage.EXPECT().GetURL(mock.Anything, "obj-1").Return("", errors.New("s3 error"))

	srv := FilesServer{queries: queries, storage: storage}

	_, err := srv.GetDownloadURL(context.Background(), &pb.GetDownloadURLRequest{Ids: []int64{1}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGetPreviewURL(t *testing.T) {
	files := []db.FileInfo{
		{ID: 1, Key: "obj-1", PreviewKey: pgtype.Text{String: ".index/previews/1", Valid: true}},
		{ID: 2, Key: "obj-2"}, // no preview
	}

	queries := NewMockFileIndex(t)
	queries.EXPECT().GetFilesByIDs(mock.Anything, []int64{1, 2}).Return(files, nil)

	storage := NewMockObjectStore(t)
	storage.EXPECT().GetInlineURL(mock.Anything, ".index/previews/1").Return("https://example.com/preview-1", nil)

	srv := FilesServer{queries: queries, storage: storage}

	resp, err := srv.GetPreviewURL(context.Background(), &pb.GetPreviewURLRequest{Ids: []int64{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.PreviewUrls) != 1 {
		t.Fatalf("got %d preview urls, want 1", len(resp.PreviewUrls))
	}
	if resp.PreviewUrls[0].Id != 1 || resp.PreviewUrls[0].Url != "https://example.com/preview-1" {
		t.Errorf("GetPreviewURL = %+v", resp.PreviewUrls[0])
	}
}

func TestGetPreviewURLSkipsFilesWithoutPreview(t *testing.T) {
	files := []db.FileInfo{{ID: 1, Key: "obj-1"}}

	queries := NewMockFileIndex(t)
	queries.EXPECT().GetFilesByIDs(mock.Anything, []int64{1}).Return(files, nil)

	storage := NewMockObjectStore(t)

	srv := FilesServer{queries: queries, storage: storage}

	resp, err := srv.GetPreviewURL(context.Background(), &pb.GetPreviewURLRequest{Ids: []int64{1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.PreviewUrls) != 0 {
		t.Errorf("got %d preview urls, want 0", len(resp.PreviewUrls))
	}
	storage.AssertNotCalled(t, "GetInlineURL", mock.Anything, mock.Anything)
}

func TestGetPreviewURLStorageError(t *testing.T) {
	files := []db.FileInfo{
		{ID: 1, Key: "obj-1", PreviewKey: pgtype.Text{String: ".index/previews/1", Valid: true}},
	}

	queries := NewMockFileIndex(t)
	queries.EXPECT().GetFilesByIDs(mock.Anything, []int64{1}).Return(files, nil)

	storage := NewMockObjectStore(t)
	storage.EXPECT().GetInlineURL(mock.Anything, ".index/previews/1").Return("", errors.New("s3 error"))

	srv := FilesServer{queries: queries, storage: storage}

	_, err := srv.GetPreviewURL(context.Background(), &pb.GetPreviewURLRequest{Ids: []int64{1}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGetUploadURL(t *testing.T) {
	storage := NewMockObjectStore(t)
	storage.EXPECT().PutURL(mock.Anything, "photos/cat.jpg").Return("https://example.com/put-url", nil)

	srv := FilesServer{storage: storage, indexPrefix: ".index/"}

	resp, err := srv.GetUploadURL(context.Background(), &pb.GetUploadURLRequest{Key: "photos/cat.jpg"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Url != "https://example.com/put-url" {
		t.Errorf("Url = %q", resp.Url)
	}
}

func TestGetUploadURLRejectsReservedPrefix(t *testing.T) {
	storage := NewMockObjectStore(t)

	srv := FilesServer{storage: storage, indexPrefix: ".index/"}

	_, err := srv.GetUploadURL(context.Background(), &pb.GetUploadURLRequest{Key: ".index/previews/1"})
	if err == nil {
		t.Fatal("expected error")
	}
	storage.AssertNotCalled(t, "PutURL", mock.Anything, mock.Anything)
}

func TestGetUploadURLRejectsEmptyKey(t *testing.T) {
	srv := FilesServer{storage: NewMockObjectStore(t), indexPrefix: ".index/"}

	_, err := srv.GetUploadURL(context.Background(), &pb.GetUploadURLRequest{Key: ""})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGetUploadURLRejectsPathEscape(t *testing.T) {
	srv := FilesServer{storage: NewMockObjectStore(t), indexPrefix: ".index/"}

	for _, key := range []string{"/etc/passwd", "../secret", "a/../../b"} {
		if _, err := srv.GetUploadURL(context.Background(), &pb.GetUploadURLRequest{Key: key}); err == nil {
			t.Errorf("key %q: expected error, got nil", key)
		}
	}
}

func TestGetUploadURLAcceptsDoubleDotsWithinAFilename(t *testing.T) {
	// ".." as a substring (not a whole path segment) is a legitimate
	// filename, e.g. a date range in the name — only ".." as its own
	// segment means "parent directory".
	storage := NewMockObjectStore(t)
	storage.EXPECT().PutURL(mock.Anything, "archive..2026.zip").Return("https://example.com/put-url", nil)

	srv := FilesServer{storage: storage, indexPrefix: ".index/"}

	_, err := srv.GetUploadURL(context.Background(), &pb.GetUploadURLRequest{Key: "archive..2026.zip"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHasDotDotSegment(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"", false},
		{"a", false},
		{"archive..2026.zip", false},
		{"a/archive..zip", false},
		{"..", true},
		{"../a", true},
		{"a/..", true},
		{"a/../b", true},
	}
	for _, tt := range tests {
		if got := hasDotDotSegment(tt.s); got != tt.want {
			t.Errorf("hasDotDotSegment(%q) = %v, want %v", tt.s, got, tt.want)
		}
	}
}

func TestCommitUpload(t *testing.T) {
	now := time.Now()
	queries := NewMockFileIndex(t)
	queries.EXPECT().UpsertFilesWithDirectories(mock.Anything, db.UpsertFilesParams{
		Keys:      []string{"photos/cat.jpg"},
		MarkedAts: []pgtype.Timestamptz{{Time: now, Valid: true}},
	}).Return(int64(1), nil)
	queries.EXPECT().GetFileByKey(mock.Anything, "photos/cat.jpg").
		Return(db.FileInfo{ID: 5, Key: "photos/cat.jpg"}, nil)

	storage := NewMockObjectStore(t)
	storage.EXPECT().Stat(mock.Anything, "photos/cat.jpg").
		Return(objstore.ObjectInfo{Key: "photos/cat.jpg", LastModified: now}, nil)

	srv := FilesServer{queries: queries, storage: storage, indexPrefix: ".index/"}

	resp, err := srv.CommitUpload(context.Background(), &pb.CommitUploadRequest{Key: "photos/cat.jpg"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.File.Id != 5 || resp.File.Key != "photos/cat.jpg" {
		t.Errorf("CommitUpload = %+v", resp.File)
	}
}

func TestCommitUploadStatMiss(t *testing.T) {
	storage := NewMockObjectStore(t)
	storage.EXPECT().Stat(mock.Anything, "missing.jpg").Return(objstore.ObjectInfo{}, errors.New("not found"))

	queries := NewMockFileIndex(t)

	srv := FilesServer{queries: queries, storage: storage, indexPrefix: ".index/"}

	_, err := srv.CommitUpload(context.Background(), &pb.CommitUploadRequest{Key: "missing.jpg"})
	if err == nil {
		t.Fatal("expected error")
	}
	queries.AssertNotCalled(t, "UpsertFilesWithDirectories", mock.Anything, mock.Anything)
}

func TestCommitUploadRejectsReservedPrefix(t *testing.T) {
	storage := NewMockObjectStore(t)

	srv := FilesServer{storage: storage, indexPrefix: ".index/"}

	_, err := srv.CommitUpload(context.Background(), &pb.CommitUploadRequest{Key: ".index/previews/1"})
	if err == nil {
		t.Fatal("expected error")
	}
	storage.AssertNotCalled(t, "Stat", mock.Anything, mock.Anything)
}

func TestGetDirectoryStats(t *testing.T) {
	queries := NewMockFileIndex(t)
	queries.EXPECT().GetDirectoryStats(mock.Anything, "docs/%").
		Return(db.GetDirectoryStatsRow{FileCount: 3, TotalBytes: 1024}, nil)

	srv := FilesServer{queries: queries}

	resp, err := srv.GetDirectoryStats(context.Background(), &pb.GetDirectoryStatsRequest{Path: "docs/"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.FileCount != 3 || resp.TotalBytes != 1024 {
		t.Errorf("GetDirectoryStats = %+v, want {3 1024}", resp)
	}
}

func TestGetDirectoryStatsEscapesPattern(t *testing.T) {
	queries := NewMockFileIndex(t)
	queries.EXPECT().GetDirectoryStats(mock.Anything, `docs\%1/%`).
		Return(db.GetDirectoryStatsRow{}, nil)

	srv := FilesServer{queries: queries}

	if _, err := srv.GetDirectoryStats(context.Background(), &pb.GetDirectoryStatsRequest{Path: `docs%1/`}); err != nil {
		t.Fatal(err)
	}
}

func TestGetDirectoryStatsRejectsMissingTrailingSlash(t *testing.T) {
	srv := FilesServer{queries: NewMockFileIndex(t)}

	_, err := srv.GetDirectoryStats(context.Background(), &pb.GetDirectoryStatsRequest{Path: "docs"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGetDirectoryStatsRejectsEmptyPath(t *testing.T) {
	srv := FilesServer{queries: NewMockFileIndex(t)}

	_, err := srv.GetDirectoryStats(context.Background(), &pb.GetDirectoryStatsRequest{Path: ""})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGetDirectoryStatsRejectsReservedPrefix(t *testing.T) {
	srv := FilesServer{queries: NewMockFileIndex(t), indexPrefix: ".index/"}

	_, err := srv.GetDirectoryStats(context.Background(), &pb.GetDirectoryStatsRequest{Path: ".index/"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGetDirectoryStatsRejectsPathEscape(t *testing.T) {
	srv := FilesServer{queries: NewMockFileIndex(t)}

	_, err := srv.GetDirectoryStats(context.Background(), &pb.GetDirectoryStatsRequest{Path: "a/../b/"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDeleteDirectorySingleBatch(t *testing.T) {
	batch := []db.ListFilesForDeleteRow{
		{ID: 1, Key: "docs/a.txt"},
		{ID: 2, Key: "docs/b.jpg", PreviewKey: pgtype.Text{String: ".index/previews/2", Valid: true}},
	}

	queries := NewMockFileIndex(t)
	queries.EXPECT().ListFilesForDelete(mock.Anything, db.ListFilesForDeleteParams{
		KeyPattern: "docs/%",
		PageLimit:  deleteDirectoryBatchSize,
	}).Return(batch, nil)
	queries.EXPECT().DeleteFilesByIDsWithDirectories(mock.Anything, []int64{1, 2}, []string{"docs/a.txt", "docs/b.jpg"}).Return(int64(2), nil)

	storage := NewMockObjectStore(t)
	storage.EXPECT().DeleteMany(mock.Anything, []string{"docs/a.txt", "docs/b.jpg", ".index/previews/2"}).Return(nil)

	srv := FilesServer{queries: queries, storage: storage}

	resp, err := srv.DeleteDirectory(context.Background(), &pb.DeleteDirectoryRequest{Path: "docs/"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.DeletedCount != 2 {
		t.Errorf("DeletedCount = %d, want 2", resp.DeletedCount)
	}
}

func TestDeleteDirectoryEmpty(t *testing.T) {
	queries := NewMockFileIndex(t)
	queries.EXPECT().ListFilesForDelete(mock.Anything, mock.Anything).Return(nil, nil)

	srv := FilesServer{queries: queries, storage: NewMockObjectStore(t)}

	resp, err := srv.DeleteDirectory(context.Background(), &pb.DeleteDirectoryRequest{Path: "empty/"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.DeletedCount != 0 {
		t.Errorf("DeletedCount = %d, want 0", resp.DeletedCount)
	}
}

func TestDeleteDirectoryMultipleBatches(t *testing.T) {
	// A full first batch (== deleteDirectoryBatchSize) must trigger a second
	// ListFilesForDelete call using the previous batch's last id as cursor.
	first := make([]db.ListFilesForDeleteRow, deleteDirectoryBatchSize)
	for i := range first {
		first[i] = db.ListFilesForDeleteRow{ID: int64(i + 1), Key: fmt.Sprintf("docs/%d.txt", i+1)}
	}
	second := []db.ListFilesForDeleteRow{{ID: int64(deleteDirectoryBatchSize + 1), Key: "docs/last.txt"}}

	queries := NewMockFileIndex(t)
	queries.EXPECT().ListFilesForDelete(mock.Anything, db.ListFilesForDeleteParams{
		KeyPattern: "docs/%",
		PageLimit:  deleteDirectoryBatchSize,
	}).Return(first, nil)
	queries.EXPECT().ListFilesForDelete(mock.Anything, db.ListFilesForDeleteParams{
		KeyPattern: "docs/%",
		HasCursor:  true,
		LastID:     int64(deleteDirectoryBatchSize),
		PageLimit:  deleteDirectoryBatchSize,
	}).Return(second, nil)
	queries.EXPECT().DeleteFilesByIDsWithDirectories(mock.Anything, mock.Anything, mock.Anything).Return(int64(deleteDirectoryBatchSize), nil).Once()
	queries.EXPECT().DeleteFilesByIDsWithDirectories(mock.Anything, []int64{int64(deleteDirectoryBatchSize + 1)}, []string{"docs/last.txt"}).Return(int64(1), nil).Once()

	storage := NewMockObjectStore(t)
	storage.EXPECT().DeleteMany(mock.Anything, mock.Anything).Return(nil).Times(2)

	srv := FilesServer{queries: queries, storage: storage}

	resp, err := srv.DeleteDirectory(context.Background(), &pb.DeleteDirectoryRequest{Path: "docs/"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.DeletedCount != int64(deleteDirectoryBatchSize+1) {
		t.Errorf("DeletedCount = %d, want %d", resp.DeletedCount, deleteDirectoryBatchSize+1)
	}
}

func TestDeleteDirectoryStorageErrorStopsBeforeDBDelete(t *testing.T) {
	batch := []db.ListFilesForDeleteRow{{ID: 1, Key: "docs/a.txt"}}

	queries := NewMockFileIndex(t)
	queries.EXPECT().ListFilesForDelete(mock.Anything, mock.Anything).Return(batch, nil)
	// DeleteFilesByIDsWithDirectories must never be called: this batch's DB
	// rows must not be dropped when we don't know whether their S3 objects
	// actually went.

	storage := NewMockObjectStore(t)
	storage.EXPECT().DeleteMany(mock.Anything, []string{"docs/a.txt"}).Return(errors.New("s3 error"))

	srv := FilesServer{queries: queries, storage: storage}

	resp, err := srv.DeleteDirectory(context.Background(), &pb.DeleteDirectoryRequest{Path: "docs/"})
	if err == nil {
		t.Fatal("expected error")
	}
	if resp.DeletedCount != 0 {
		t.Errorf("DeletedCount = %d, want 0", resp.DeletedCount)
	}
	queries.AssertNotCalled(t, "DeleteFilesByIDsWithDirectories", mock.Anything, mock.Anything, mock.Anything)
}

func TestDeleteDirectoryPartialProgressReturnedOnError(t *testing.T) {
	// A full first batch (forcing a second ListFilesForDelete call, since a
	// short batch is this loop's only "no more rows" signal) succeeds
	// entirely; the second batch then fails. The first batch's count must
	// still be reported, not zeroed out.
	first := make([]db.ListFilesForDeleteRow, deleteDirectoryBatchSize)
	firstKeys := make([]string, deleteDirectoryBatchSize)
	for i := range first {
		key := fmt.Sprintf("docs/%d.txt", i+1)
		first[i] = db.ListFilesForDeleteRow{ID: int64(i + 1), Key: key}
		firstKeys[i] = key
	}
	second := []db.ListFilesForDeleteRow{{ID: int64(deleteDirectoryBatchSize + 1), Key: "docs/last.txt"}}

	queries := NewMockFileIndex(t)
	queries.EXPECT().ListFilesForDelete(mock.Anything, db.ListFilesForDeleteParams{
		KeyPattern: "docs/%",
		PageLimit:  deleteDirectoryBatchSize,
	}).Return(first, nil)
	queries.EXPECT().ListFilesForDelete(mock.Anything, db.ListFilesForDeleteParams{
		KeyPattern: "docs/%",
		HasCursor:  true,
		LastID:     int64(deleteDirectoryBatchSize),
		PageLimit:  deleteDirectoryBatchSize,
	}).Return(second, nil)
	// .Once(): DeleteFilesByIDsWithDirectories must not be called again for
	// the second (failed) batch.
	queries.EXPECT().DeleteFilesByIDsWithDirectories(mock.Anything, mock.Anything, mock.Anything).Return(int64(deleteDirectoryBatchSize), nil).Once()

	storage := NewMockObjectStore(t)
	storage.EXPECT().DeleteMany(mock.Anything, firstKeys).Return(nil)
	storage.EXPECT().DeleteMany(mock.Anything, []string{"docs/last.txt"}).Return(errors.New("s3 error"))

	srv := FilesServer{queries: queries, storage: storage}

	resp, err := srv.DeleteDirectory(context.Background(), &pb.DeleteDirectoryRequest{Path: "docs/"})
	if err == nil {
		t.Fatal("expected error")
	}
	if resp.DeletedCount != int64(deleteDirectoryBatchSize) {
		t.Errorf("DeletedCount = %d, want %d (first batch's progress preserved)", resp.DeletedCount, deleteDirectoryBatchSize)
	}
}

func TestDeleteDirectoryDBDeleteError(t *testing.T) {
	batch := []db.ListFilesForDeleteRow{{ID: 1, Key: "docs/a.txt"}}

	queries := NewMockFileIndex(t)
	queries.EXPECT().ListFilesForDelete(mock.Anything, mock.Anything).Return(batch, nil)
	queries.EXPECT().DeleteFilesByIDsWithDirectories(mock.Anything, []int64{1}, []string{"docs/a.txt"}).Return(int64(0), errors.New("db error"))

	storage := NewMockObjectStore(t)
	storage.EXPECT().DeleteMany(mock.Anything, []string{"docs/a.txt"}).Return(nil)

	srv := FilesServer{queries: queries, storage: storage}

	_, err := srv.DeleteDirectory(context.Background(), &pb.DeleteDirectoryRequest{Path: "docs/"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDeleteDirectoryRejectsEmptyPath(t *testing.T) {
	srv := FilesServer{queries: NewMockFileIndex(t)}

	_, err := srv.DeleteDirectory(context.Background(), &pb.DeleteDirectoryRequest{Path: ""})
	if err == nil {
		t.Fatal("expected error, empty path would match the entire bucket")
	}
}

func TestDeleteDirectoryRejectsMissingTrailingSlash(t *testing.T) {
	srv := FilesServer{queries: NewMockFileIndex(t)}

	_, err := srv.DeleteDirectory(context.Background(), &pb.DeleteDirectoryRequest{Path: "docs"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDeleteDirectoryRejectsReservedPrefix(t *testing.T) {
	srv := FilesServer{queries: NewMockFileIndex(t), indexPrefix: ".index/"}

	_, err := srv.DeleteDirectory(context.Background(), &pb.DeleteDirectoryRequest{Path: ".index/"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateDirPath(t *testing.T) {
	srv := FilesServer{indexPrefix: ".index/"}
	tests := []struct {
		path    string
		wantErr bool
	}{
		{"", true},
		{"docs", true},
		{"docs/", false},
		{"/docs/", true},
		{"a/../b/", true},
		{".index/", true},
		{".index/previews/", true},
	}
	for _, tt := range tests {
		_, err := srv.validateDirPath(tt.path)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateDirPath(%q): err = %v, wantErr %v", tt.path, err, tt.wantErr)
		}
	}
}

// freeAddr reserves an ephemeral port and returns its address. There is a
// small window between closing the probe listener and Serve re-binding it,
// which is acceptable for tests.
func freeAddr(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := lis.Addr().String()
	if err := lis.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}
	return addr
}

func TestServe(t *testing.T) {
	now := time.Now()
	file := db.FileInfo{
		ID: 7, Key: "obj-key",
		ContentType:  pgtype.Text{String: "text/plain", Valid: true},
		SizeBytes:    pgtype.Int8{Int64: 100, Valid: true},
		CreatedAt:    pgtype.Timestamptz{Time: now, Valid: true},
		LastModified: pgtype.Timestamptz{Time: now, Valid: true},
	}

	queries := NewMockFileIndex(t)
	queries.EXPECT().GetFile(mock.Anything, int64(7)).Return(file, nil)
	queries.EXPECT().GetIndexExifResult(mock.Anything, int64(7)).Return(db.IndexExifResult{}, pgx.ErrNoRows)

	store := NewMockObjectStore(t)

	addr := freeAddr(t)
	srv := New(addr, store, queries, ".index/")

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve() }()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close conn: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := pb.NewFilesServiceClient(conn)
	resp, err := client.GetFileInfo(ctx, &pb.GetFileInfoRequest{Id: 7},
		grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("GetFileInfo over gRPC: %v", err)
	}
	if resp.GetFile().GetId() != 7 || resp.GetFile().GetKey() != "obj-key" {
		t.Errorf("GetFileInfo = %+v, want id=7 key=obj-key", resp.GetFile())
	}

	select {
	case err := <-errCh:
		t.Fatalf("Serve exited unexpectedly: %v", err)
	default:
	}
}

func TestServeBadAddr(t *testing.T) {
	srv := New("256.256.256.256:0", NewMockObjectStore(t), NewMockFileIndex(t), ".index/")
	if err := srv.Serve(); err == nil {
		t.Fatal("Serve with bad addr: want error, got nil")
	}
}
