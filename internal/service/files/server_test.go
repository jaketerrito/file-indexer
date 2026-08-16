package files

import (
	"context"
	"errors"
	"file-indexer/internal/db"
	"net"
	"testing"
	"time"

	pb "file-indexer/internal/pb/service/v1"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestNew(t *testing.T) {
	queries := NewMockFileIndex(t)
	store := NewMockObjectStore(t)

	srv := New(":1234", store, queries)

	if srv == nil {
		t.Fatal("New returned nil")
	}
	if srv.addr != ":1234" {
		t.Errorf("addr = %q, want %q", srv.addr, ":1234")
	}
	if srv.storage != store || srv.queries != queries {
		t.Error("New did not wire dependencies")
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
	queries.EXPECT().DeleteFile(mock.Anything, int64(1)).Return(db.File{ID: 1, Key: "obj-key"}, nil)

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
	queries.EXPECT().DeleteFile(mock.Anything, int64(1)).Return(db.File{ID: 1, Key: "obj-key"}, nil)

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
	queries.EXPECT().DeleteFile(mock.Anything, int64(1)).Return(db.File{ID: 1, Key: "obj-key"}, nil)

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
	queries.AssertNotCalled(t, "DeleteFile", mock.Anything, mock.Anything)
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
	queries.EXPECT().DeleteFile(mock.Anything, int64(1)).Return(db.File{}, errors.New("db error"))

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
	srv := New(addr, store, queries)

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
	srv := New("256.256.256.256:0", NewMockObjectStore(t), NewMockFileIndex(t))
	if err := srv.Serve(); err == nil {
		t.Fatal("Serve with bad addr: want error, got nil")
	}
}
