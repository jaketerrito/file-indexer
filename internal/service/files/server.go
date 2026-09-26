package files

import (
	"context"
	"errors"
	"file-indexer/internal/db"
	pb "file-indexer/internal/pb/service/v1"
	"file-indexer/internal/storage"
	"file-indexer/internal/validate"
	"log/slog"
	"net"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type ObjectStore interface {
	GetURL(ctx context.Context, key string) (string, error)
	GetInlineURL(ctx context.Context, key string) (string, error)
	// PutURL returns a presigned URL a client can PUT bytes to directly.
	PutURL(ctx context.Context, key string) (string, error)
	// Stat is used by CommitUpload to confirm the object actually landed in
	// S3 (and read its size/content-type/mtime) before it is upserted into
	// the index — never trust an unverified client claim.
	Stat(ctx context.Context, key string) (storage.ObjectInfo, error)
	// Copy duplicates an object within the same bucket, used by MoveFile to
	// land the object at its new key before updating the database row.
	Copy(ctx context.Context, srcKey, dstKey string) error
	Delete(ctx context.Context, key string) error
	// DeleteMany is used by DeleteDirectory to batch-remove a subtree's
	// objects. See its doc comment on the not-atomic contract this implies.
	DeleteMany(ctx context.Context, keys []string) error
	// CreateMultipartUpload, UploadPartURL, ListParts, CompleteMultipartUpload,
	// and AbortMultipartUpload back the large-file resumable upload RPCs; see
	// storage.Storage's doc comments on each for their contracts.
	CreateMultipartUpload(ctx context.Context, key, contentType string) (uploadID string, err error)
	UploadPartURL(ctx context.Context, key, uploadID string, partNumber int) (string, error)
	ListParts(ctx context.Context, key, uploadID string) ([]storage.PartInfo, error)
	CompleteMultipartUpload(ctx context.Context, key, uploadID string) error
	AbortMultipartUpload(ctx context.Context, key, uploadID string) error
}

// FileIndex is deliberately narrower than db.Store: it exposes only the
// directory-index-maintaining WithDirectories variants of the files write
// queries, never the bare UpsertFiles/DeleteFile/DeleteFilesByIDs, so this
// service cannot write a files row without also keeping the directories
// table in sync (see directories' doc comment in migrations/001_initial.sql).
type FileIndex interface {
	GetFile(ctx context.Context, id int64) (db.FileInfo, error)
	GetFilesByIDs(ctx context.Context, ids []int64) ([]db.FileInfo, error)
	GetFileByKey(ctx context.Context, key string) (db.FileInfo, error)
	DeleteFileWithDirectories(ctx context.Context, id int64) (db.File, error)
	MoveFileWithDirectories(ctx context.Context, arg db.MoveFileWithDirectoriesParams) (db.File, error)
	UpsertFilesWithDirectories(ctx context.Context, arg db.UpsertFilesParams) (int64, error)
	// GetIndexExifResult returns pgx.ErrNoRows when the file has neither EXIF
	// nor XMP data (or hasn't reached the exif indexer yet).
	GetIndexExifResult(ctx context.Context, fileID int64) (db.IndexExifResult, error)
	GetDirectoryStats(ctx context.Context, keyPattern string) (db.GetDirectoryStatsRow, error)
	ListFilesForDelete(ctx context.Context, arg db.ListFilesForDeleteParams) ([]db.ListFilesForDeleteRow, error)
	DeleteFilesByIDsWithDirectories(ctx context.Context, ids []int64, keys []string) (int64, error)
	GetIndexQueueStatuses(ctx context.Context, arg db.GetIndexQueueStatusesParams) ([]db.GetIndexQueueStatusesRow, error)
	// AcquireMoveLocks pins a connection and acquires Postgres advisory locks
	// for the supplied keys. The returned release function must be called to
	// unlock and return the connection. Used by MoveFile to serialize moves
	// across multiple files-service replicas.
	AcquireMoveLocks(ctx context.Context, keys ...string) (func(), error)
}

// previewIndexType is duplicated from internal/service/search for the same
// reason hasDotDotSegment is: services share queue names by convention.
const previewIndexType = "preview"

// previewStatus maps a file's read-model row and its preview index_queue
// state onto the API enum. Duplicated from internal/service/search; kept
// local because both services depend on the same queue semantics but not on
// each other.
func previewStatus(f db.FileInfo, qStatus string, qQueued bool) pb.PreviewStatus {
	if f.PreviewKey.Valid {
		return pb.PreviewStatus_PREVIEW_STATUS_READY
	}
	if qQueued {
		switch qStatus {
		case "pending":
			return pb.PreviewStatus_PREVIEW_STATUS_PENDING
		case "claimed":
			return pb.PreviewStatus_PREVIEW_STATUS_PROCESSING
		case "error":
			return pb.PreviewStatus_PREVIEW_STATUS_FAILED
		}
		return pb.PreviewStatus_PREVIEW_STATUS_NONE
	}
	if !f.ContentType.Valid || strings.HasPrefix(f.ContentType.String, "image/") {
		return pb.PreviewStatus_PREVIEW_STATUS_PENDING
	}
	return pb.PreviewStatus_PREVIEW_STATUS_NONE
}

type FilesServer struct {
	pb.UnimplementedFilesServiceServer
	addr        string
	storage     ObjectStore
	queries     FileIndex
	indexPrefix string
}

// New constructs a FilesServer with its dependencies already built by the
// caller (composition root). It does no I/O; call Serve to start listening.
// indexPrefix is the key prefix under which index types write derived
// objects (see config.IndexPrefix); uploads targeting it are rejected so a
// client can never masquerade bytes as a derived artifact (which the
// crawler treats specially, see internal/service/crawler).
func New(addr string, store ObjectStore, queries FileIndex, indexPrefix string) *FilesServer {
	return &FilesServer{
		addr:        addr,
		storage:     store,
		queries:     queries,
		indexPrefix: indexPrefix,
	}
}

func (s *FilesServer) GetDownloadURL(ctx context.Context, req *pb.GetDownloadURLRequest) (*pb.GetDownloadURLResponse, error) {
	files, err := s.queries.GetFilesByIDs(ctx, req.GetIds())
	if err != nil {
		return nil, err
	}
	specs := make([]*pb.DownloadURLSpec, 0, len(files))
	for _, f := range files {
		url, err := s.storage.GetURL(ctx, f.Key)
		if err != nil {
			return nil, err
		}
		specs = append(specs, &pb.DownloadURLSpec{Id: f.ID, Url: url})
	}
	return &pb.GetDownloadURLResponse{DownloadUrls: specs}, nil
}

// GetOpenURL returns presigned URLs that render the requested files inline
// in a browser tab (response-content-disposition=inline), unlike
// GetDownloadURL which forces an attachment download.
func (s *FilesServer) GetOpenURL(ctx context.Context, req *pb.GetOpenURLRequest) (*pb.GetOpenURLResponse, error) {
	files, err := s.queries.GetFilesByIDs(ctx, req.GetIds())
	if err != nil {
		return nil, err
	}
	specs := make([]*pb.OpenURLSpec, 0, len(files))
	for _, f := range files {
		url, err := s.storage.GetInlineURL(ctx, f.Key)
		if err != nil {
			return nil, err
		}
		specs = append(specs, &pb.OpenURLSpec{Id: f.ID, Url: url})
	}
	return &pb.GetOpenURLResponse{OpenUrls: specs}, nil
}

// GetPreviewURL returns inline presigned URLs for the requested files'
// preview images. Files without a preview are omitted from the response
// rather than returned with an empty URL, so callers can treat presence as
// "this file is renderable". Presigning is a local signature computation, not
// a round trip to storage, so a full page of ids is cheap.
func (s *FilesServer) GetPreviewURL(ctx context.Context, req *pb.GetPreviewURLRequest) (*pb.GetPreviewURLResponse, error) {
	files, err := s.queries.GetFilesByIDs(ctx, req.GetIds())
	if err != nil {
		return nil, err
	}
	specs := make([]*pb.PreviewURLSpec, 0, len(files))
	for _, f := range files {
		if !f.PreviewKey.Valid || f.PreviewKey.String == "" {
			continue
		}
		url, err := s.storage.GetInlineURL(ctx, f.PreviewKey.String)
		if err != nil {
			return nil, err
		}
		specs = append(specs, &pb.PreviewURLSpec{Id: f.ID, Url: url})
	}
	return &pb.GetPreviewURLResponse{PreviewUrls: specs}, nil
}

// GetFilePreviewStatuses returns the preview pipeline status (and presigned
// URL when ready) for a batch of files. This is a targeted read intended for
// frontend polling: it avoids re-fetching entire pages just to watch a few
// pending items transition.
func (s *FilesServer) GetFilePreviewStatuses(ctx context.Context, req *pb.GetFilePreviewStatusesRequest) (*pb.GetFilePreviewStatusesResponse, error) {
	ids := req.GetIds()
	if len(ids) == 0 {
		return &pb.GetFilePreviewStatusesResponse{}, nil
	}
	files, err := s.queries.GetFilesByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	rows, err := s.queries.GetIndexQueueStatuses(ctx, db.GetIndexQueueStatusesParams{
		IndexType: previewIndexType,
		FileIds:   ids,
	})
	if err != nil {
		return nil, err
	}
	queued := make(map[int64]string, len(rows))
	for _, r := range rows {
		queued[r.FileID] = r.Status
	}

	statuses := make([]*pb.FilePreviewStatus, 0, len(files))
	for _, f := range files {
		st, ok := queued[f.ID]
		ps := previewStatus(f, st, ok)
		fps := &pb.FilePreviewStatus{
			Id:            f.ID,
			PreviewStatus: ps,
		}
		if ps == pb.PreviewStatus_PREVIEW_STATUS_READY {
			url, err := s.storage.GetInlineURL(ctx, f.PreviewKey.String)
			if err != nil {
				return nil, err
			}
			fps.PreviewUrl = url
		}
		statuses = append(statuses, fps)
	}
	return &pb.GetFilePreviewStatusesResponse{Statuses: statuses}, nil
}

func (s *FilesServer) GetFileInfo(ctx context.Context, req *pb.GetFileInfoRequest) (*pb.GetFileInfoResponse, error) {
	file, err := s.queries.GetFile(ctx, req.GetId())
	if err != nil {
		return nil, err
	}
	info := dbFileToProto(file)
	exif, err := s.queries.GetIndexExifResult(ctx, req.GetId())
	switch {
	case err == nil:
		info.Exif = exifToProto(exif)
	case errors.Is(err, pgx.ErrNoRows):
		// No exif index result yet, or the file has neither EXIF nor XMP:
		// leave Exif unset.
	default:
		return nil, err
	}
	return &pb.GetFileInfoResponse{File: info}, nil
}

func (s *FilesServer) DeleteFile(ctx context.Context, req *pb.DeleteFileRequest) (*pb.DeleteFileResponse, error) {
	file, err := s.queries.GetFile(ctx, req.GetId())
	if err != nil {
		return nil, err
	}
	if err := s.storage.Delete(ctx, file.Key); err != nil {
		return nil, err
	}
	// Best effort: an orphaned preview blob wastes a few kilobytes, but
	// failing to remove it must not block deleting the file itself. A GC job
	// reconciles leftovers (see cmd/preview-gc and internal/service/previewgc).
	if file.PreviewKey.Valid && file.PreviewKey.String != "" {
		if err := s.storage.Delete(ctx, file.PreviewKey.String); err != nil {
			slog.Warn("delete preview object", "key", file.PreviewKey.String, "error", err)
		}
	}
	if _, err := s.queries.DeleteFileWithDirectories(ctx, req.GetId()); err != nil {
		return nil, err
	}
	return &pb.DeleteFileResponse{}, nil
}

// MoveFile relocates a file to a new S3 key. The copy, DB update, and
// best-effort old-object deletion all run inside a Postgres advisory-lock
// critical section so moves are safe across multiple files-service replicas;
// if the old-object deletion fails, the move is still successful because the
// crawler reconciles orphaned source objects out of band.
func (s *FilesServer) MoveFile(ctx context.Context, req *pb.MoveFileRequest) (*pb.MoveFileResponse, error) {
	file, err := s.queries.GetFile(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "file %d not found", req.GetId())
		}
		return nil, status.Errorf(codes.Internal, "failed to read source file")
	}

	newKey := req.GetDestinationKey()
	if newKey == "" {
		return nil, status.Error(codes.InvalidArgument, "destination_key must not be empty")
	}
	if newKey == file.Key {
		return nil, status.Error(codes.InvalidArgument, "destination_key must differ from current key")
	}
	if s.indexPrefix != "" && strings.HasPrefix(newKey, s.indexPrefix) {
		return nil, status.Errorf(codes.InvalidArgument, "destination_key %q is reserved for derived objects", newKey)
	}

	// Friendly fast-path check: if the destination is already occupied, fail
	// before doing any work. The same check is repeated inside the advisory
	// locks so a key that appears while we are waiting is caught before any
	// Copy.
	if _, err := s.queries.GetFileByKey(ctx, newKey); err == nil {
		return nil, status.Errorf(codes.AlreadyExists, "file with key %q already exists", newKey)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, status.Errorf(codes.Internal, "failed to check destination key")
	}

	moved, err := s.moveFileWithLocks(ctx, req.GetId(), file.Key, newKey)
	if err != nil {
		return nil, err
	}

	updated, err := s.queries.GetFile(ctx, moved.ID)
	if err != nil {
		return nil, err
	}
	return &pb.MoveFileResponse{File: dbFileToProto(updated)}, nil
}

// moveFileWithLocks serializes the MoveFile copy-vs-commit critical section
// across multiple files-service replicas using Postgres advisory locks. The
// locks are held through the DB update and the old-key Delete; keeping the
// delete inside the locked section closes the A→B→A race where a concurrent
// move back to the source key could otherwise copy an object that the first
// move then deletes after releasing the locks.
func (s *FilesServer) moveFileWithLocks(ctx context.Context, id int64, oldKey, newKey string) (db.File, error) {
	// Lock both endpoints in the global byte order. The same order is used
	// by every mover, so M(A→B) and M(B→A) serialize instead of deadlocking.
	// db.Store also sorts defensively, but sorting here makes the contract
	// explicit at the call site and lets unit tests assert the lock order.
	keys := []string{oldKey, newKey}
	slices.SortFunc(keys, func(a, b string) int {
		if a == b {
			return 0
		}
		if a < b {
			return -1
		}
		return 1
	})
	release, err := s.queries.AcquireMoveLocks(ctx, keys...)
	if err != nil {
		return db.File{}, status.Errorf(codes.Internal, "acquire move locks: %v", err)
	}
	defer release()

	// Re-read the source row under the lock so the storage Copy and the
	// old-key Delete use the key as it exists at lock time. A concurrent
	// move in another replica could have changed it while we waited for the
	// advisory locks.
	src, err := s.queries.GetFile(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.File{}, status.Errorf(codes.NotFound, "file %d not found", id)
		}
		return db.File{}, status.Errorf(codes.Internal, "failed to read source file")
	}
	currentOldKey := src.Key

	// Re-validate the destination under the lock. A destination that
	// appeared while we were waiting must be rejected before any Copy.
	if _, err := s.queries.GetFileByKey(ctx, newKey); err == nil {
		return db.File{}, status.Errorf(codes.AlreadyExists, "file with key %q already exists", newKey)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return db.File{}, status.Errorf(codes.Internal, "failed to check destination key")
	}

	if err := s.storage.Copy(ctx, currentOldKey, newKey); err != nil {
		return db.File{}, status.Errorf(codes.Internal, "copy object: %v", err)
	}

	moved, err := s.queries.MoveFileWithDirectories(ctx, db.MoveFileWithDirectoriesParams{
		ID:     id,
		NewKey: newKey,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			// Another concurrent request won the race and owns newKey.
			// Do NOT delete the destination object: the winner's DB row now
			// references it, and removing it would leave that row orphaned.
			return db.File{}, status.Errorf(codes.AlreadyExists, "file with key %q already exists", newKey)
		}
		if delErr := s.storage.Delete(ctx, newKey); delErr != nil {
			slog.Warn("rollback copied object after move failed", "key", newKey, "error", delErr)
		}
		return db.File{}, status.Errorf(codes.Internal, "move file: %v", err)
	}

	// Best-effort cleanup of the old object, done before releasing the locks
	// so M1's delete(A) is ordered before M2's copy to A.
	if err := s.storage.Delete(ctx, currentOldKey); err != nil {
		slog.Warn("delete old object after move", "key", currentOldKey, "error", err)
	}
	return moved, nil
}

// GetUploadURL returns a presigned URL the caller can PUT an object's bytes
// to directly, bypassing this service for the transfer itself. It rejects
// keys under indexPrefix so a client can never plant an object where the
// crawler expects only derived artifacts (see doc comment on New).
func (s *FilesServer) GetUploadURL(ctx context.Context, req *pb.GetUploadURLRequest) (*pb.GetUploadURLResponse, error) {
	key, err := s.validateUploadKey(req.GetKey())
	if err != nil {
		return nil, err
	}
	url, err := s.storage.PutURL(ctx, key)
	if err != nil {
		return nil, err
	}
	return &pb.GetUploadURLResponse{Url: url}, nil
}

// CommitUpload is called once a client's presigned PUT has completed. It
// never trusts the client's say-so: it re-stats the key in S3 and only then
// upserts a files row (the same reference-based path the crawler uses),
// which the next index-queue seed picks up. Returns a stat-derived FileInfo;
// preview/EXIF fields are unset until their indexers run.
func (s *FilesServer) CommitUpload(ctx context.Context, req *pb.CommitUploadRequest) (*pb.CommitUploadResponse, error) {
	key, err := s.validateUploadKey(req.GetKey())
	if err != nil {
		return nil, err
	}
	file, err := s.indexUploadedKey(ctx, key)
	if err != nil {
		return nil, err
	}
	return &pb.CommitUploadResponse{File: file}, nil
}

// indexUploadedKey re-stats key in S3 and upserts a files row from that
// stat (the same reference-based path the crawler uses), shared by
// CommitUpload (single-PUT path) and CompleteMultipartUpload (multipart
// path) once each has finished landing bytes in S3.
func (s *FilesServer) indexUploadedKey(ctx context.Context, key string) (*pb.FileInfo, error) {
	info, err := s.storage.Stat(ctx, key)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "stat uploaded object: %v", err)
	}
	if _, err := s.queries.UpsertFilesWithDirectories(ctx, db.UpsertFilesParams{
		Keys:      []string{info.Key},
		MarkedAts: []pgtype.Timestamptz{{Time: info.LastModified, Valid: true}},
	}); err != nil {
		return nil, err
	}
	file, err := s.queries.GetFileByKey(ctx, info.Key)
	if err != nil {
		return nil, err
	}
	return dbFileToProto(file), nil
}

// CreateMultipartUpload starts the resumable large-file upload path: see
// CreateMultipartUploadRequest's doc comment in files.proto for the full
// flow. Content-Type is required — S3 binds it to the object at initiate
// time rather than trusting the eventual PUT.
func (s *FilesServer) CreateMultipartUpload(ctx context.Context, req *pb.CreateMultipartUploadRequest) (*pb.CreateMultipartUploadResponse, error) {
	key, err := s.validateUploadKey(req.GetKey())
	if err != nil {
		return nil, err
	}
	uploadID, err := s.storage.CreateMultipartUpload(ctx, key, req.GetContentType())
	if err != nil {
		return nil, err
	}
	return &pb.CreateMultipartUploadResponse{UploadId: uploadID}, nil
}

// minPartNumber and maxPartNumber bound S3's multipart part numbering.
const (
	minPartNumber = 1
	maxPartNumber = 10000
)

// GetUploadPartURL returns a presigned URL for one part of an in-progress
// multipart upload. Like GetUploadURL, this never touches the bytes — the
// client PUTs directly to S3.
func (s *FilesServer) GetUploadPartURL(ctx context.Context, req *pb.GetUploadPartURLRequest) (*pb.GetUploadPartURLResponse, error) {
	key, err := s.validateUploadKey(req.GetKey())
	if err != nil {
		return nil, err
	}
	if req.GetUploadId() == "" {
		return nil, status.Error(codes.InvalidArgument, "upload_id must not be empty")
	}
	if n := req.GetPartNumber(); n < minPartNumber || n > maxPartNumber {
		return nil, status.Errorf(codes.InvalidArgument, "part_number must be between %d and %d, got %d", minPartNumber, maxPartNumber, n)
	}
	url, err := s.storage.UploadPartURL(ctx, key, req.GetUploadId(), int(req.GetPartNumber()))
	if err != nil {
		return nil, err
	}
	return &pb.GetUploadPartURLResponse{Url: url}, nil
}

// ListUploadedParts reports the parts S3 already has for upload_id, letting
// a client resuming after a dropped connection or reload skip re-uploading
// parts that already landed.
func (s *FilesServer) ListUploadedParts(ctx context.Context, req *pb.ListUploadedPartsRequest) (*pb.ListUploadedPartsResponse, error) {
	key, err := s.validateUploadKey(req.GetKey())
	if err != nil {
		return nil, err
	}
	if req.GetUploadId() == "" {
		return nil, status.Error(codes.InvalidArgument, "upload_id must not be empty")
	}
	parts, err := s.storage.ListParts(ctx, key, req.GetUploadId())
	if err != nil {
		return nil, err
	}
	protoParts := make([]*pb.UploadedPart, len(parts))
	for i, p := range parts {
		protoParts[i] = &pb.UploadedPart{PartNumber: int32(p.PartNumber), Size: p.Size}
	}
	return &pb.ListUploadedPartsResponse{Parts: protoParts}, nil
}

// CompleteMultipartUpload finalizes upload_id in S3 (see
// storage.Storage.CompleteMultipartUpload's doc comment: it lists landed
// parts itself rather than trusting a client-supplied list) and then indexes
// the finished object exactly as CommitUpload does for the single-PUT path.
func (s *FilesServer) CompleteMultipartUpload(ctx context.Context, req *pb.CompleteMultipartUploadRequest) (*pb.CompleteMultipartUploadResponse, error) {
	key, err := s.validateUploadKey(req.GetKey())
	if err != nil {
		return nil, err
	}
	if req.GetUploadId() == "" {
		return nil, status.Error(codes.InvalidArgument, "upload_id must not be empty")
	}
	if err := s.storage.CompleteMultipartUpload(ctx, key, req.GetUploadId()); err != nil {
		return nil, err
	}
	file, err := s.indexUploadedKey(ctx, key)
	if err != nil {
		return nil, err
	}
	return &pb.CompleteMultipartUploadResponse{File: file}, nil
}

// AbortMultipartUpload cancels an in-progress multipart upload, e.g. when
// the client gives up retrying a part or the user cancels mid-upload.
func (s *FilesServer) AbortMultipartUpload(ctx context.Context, req *pb.AbortMultipartUploadRequest) (*pb.AbortMultipartUploadResponse, error) {
	key, err := s.validateUploadKey(req.GetKey())
	if err != nil {
		return nil, err
	}
	if req.GetUploadId() == "" {
		return nil, status.Error(codes.InvalidArgument, "upload_id must not be empty")
	}
	if err := s.storage.AbortMultipartUpload(ctx, key, req.GetUploadId()); err != nil {
		return nil, err
	}
	return &pb.AbortMultipartUploadResponse{}, nil
}

// validateUploadKey rejects keys that are empty, escape the bucket root via
// a ".." path segment, or fall under indexPrefix (reserved for derived
// objects the crawler must never see as user files).
func (s *FilesServer) validateUploadKey(key string) (string, error) {
	if key == "" {
		return "", status.Error(codes.InvalidArgument, "key must not be empty")
	}
	if strings.HasPrefix(key, "/") || hasDotDotSegment(key) {
		return "", status.Errorf(codes.InvalidArgument, "invalid key %q", key)
	}
	if s.indexPrefix != "" && strings.HasPrefix(key, s.indexPrefix) {
		return "", status.Errorf(codes.InvalidArgument, "key %q is reserved for derived objects", key)
	}
	return key, nil
}

// validateDirPath validates a directory path for GetDirectoryStats and
// DeleteDirectory: non-empty (an empty prefix would match every key in the
// bucket), trailing "/" required (so "docs" can never also match
// "docs-archive/"), no ".." segments, and not under indexPrefix — deleting
// derived objects out from under a running index worker isn't something
// this RPC needs to support; they are regenerated anyway.
func (s *FilesServer) validateDirPath(path string) (string, error) {
	if path == "" {
		return "", status.Error(codes.InvalidArgument, "path must not be empty")
	}
	if !strings.HasSuffix(path, "/") {
		return "", status.Errorf(codes.InvalidArgument, "path %q must end in \"/\"", path)
	}
	if strings.HasPrefix(path, "/") || hasDotDotSegment(path) {
		return "", status.Errorf(codes.InvalidArgument, "invalid path %q", path)
	}
	if s.indexPrefix != "" && strings.HasPrefix(path, s.indexPrefix) {
		return "", status.Errorf(codes.InvalidArgument, "path %q is reserved for derived objects", path)
	}
	return path, nil
}

// hasDotDotSegment reports whether s contains ".." as a whole path segment
// (i.e. bounded by "/" or the string's own edges), not merely as a
// substring — a plain strings.Contains(s, "..") would reject legitimate
// names like "archive..2026.zip" that happen to contain two consecutive
// dots without ever meaning "parent directory".
func hasDotDotSegment(s string) bool {
	for _, segment := range strings.Split(s, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}

// GetDirectoryStats reports how many files live under path and their total
// size, for a delete-folder confirmation dialog to show before
// DeleteDirectory actually runs.
func (s *FilesServer) GetDirectoryStats(ctx context.Context, req *pb.GetDirectoryStatsRequest) (*pb.GetDirectoryStatsResponse, error) {
	path, err := s.validateDirPath(req.GetPath())
	if err != nil {
		return nil, err
	}
	stats, err := s.queries.GetDirectoryStats(ctx, escapeLike(path)+"%")
	if err != nil {
		return nil, err
	}
	return &pb.GetDirectoryStatsResponse{FileCount: stats.FileCount, TotalBytes: stats.TotalBytes}, nil
}

// deleteDirectoryBatchSize bounds how many files DeleteDirectory processes
// per round trip: one ListFilesForDelete page, one DeleteMany call, one
// DeleteFilesByIDs call. Independent of S3's own 1000-key multi-delete cap
// (storage.DeleteMany batches that internally) — this bounds memory and
// transaction size on the DB side.
const deleteDirectoryBatchSize = 1000

// DeleteDirectory removes every file under path: S3 objects (source and
// preview) first, then the DB rows, batch by batch. It is not atomic (see
// DeleteDirectoryResponse's doc comment in files.proto): a batch's S3
// deletion completing without error is what gates deleting that batch's DB
// rows, so a failure part-way through leaves some prefix of the directory
// gone and the rest untouched — retrying with the same path picks up where
// it left off (S3 no-ops keys already deleted, ListFilesForDelete simply
// won't see rows already removed from the DB).
func (s *FilesServer) DeleteDirectory(ctx context.Context, req *pb.DeleteDirectoryRequest) (*pb.DeleteDirectoryResponse, error) {
	path, err := s.validateDirPath(req.GetPath())
	if err != nil {
		return nil, err
	}
	keyPattern := escapeLike(path) + "%"

	var deleted int64
	var lastID int64
	hasCursor := false
	for {
		batch, err := s.queries.ListFilesForDelete(ctx, db.ListFilesForDeleteParams{
			KeyPattern: keyPattern,
			HasCursor:  hasCursor,
			LastID:     lastID,
			PageLimit:  deleteDirectoryBatchSize,
		})
		if err != nil {
			return &pb.DeleteDirectoryResponse{DeletedCount: deleted}, err
		}
		if len(batch) == 0 {
			break
		}

		// objectKeys includes preview keys (for storage.DeleteMany, which
		// must remove derived objects too); sourceKeys is files' own keys
		// only (for PruneDirectoriesForKeys via
		// DeleteFilesByIDsWithDirectories — a preview key lives under
		// indexPrefix, never a real directory, and has no ancestors to
		// prune).
		objectKeys := make([]string, 0, len(batch)*2)
		sourceKeys := make([]string, 0, len(batch))
		ids := make([]int64, 0, len(batch))
		for _, f := range batch {
			objectKeys = append(objectKeys, f.Key)
			sourceKeys = append(sourceKeys, f.Key)
			if f.PreviewKey.Valid && f.PreviewKey.String != "" {
				objectKeys = append(objectKeys, f.PreviewKey.String)
			}
			ids = append(ids, f.ID)
		}

		if err := s.storage.DeleteMany(ctx, objectKeys); err != nil {
			return &pb.DeleteDirectoryResponse{DeletedCount: deleted},
				status.Errorf(codes.Internal, "delete objects under %q: %v", path, err)
		}

		n, err := s.queries.DeleteFilesByIDsWithDirectories(ctx, ids, sourceKeys)
		if err != nil {
			return &pb.DeleteDirectoryResponse{DeletedCount: deleted}, err
		}
		deleted += n

		lastID = batch[len(batch)-1].ID
		hasCursor = true
		if len(batch) < deleteDirectoryBatchSize {
			break
		}
	}

	return &pb.DeleteDirectoryResponse{DeletedCount: deleted}, nil
}

// escapeLike escapes LIKE metacharacters so a directory path matches
// literally inside a prefix pattern; a copy of search.escapeLike, kept
// package-local rather than shared since it is three lines and pulling in a
// cross-service-package dependency for it is not worth it.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

func (s *FilesServer) Serve() error {
	lis, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	return s.serveOn(lis)
}

func (s *FilesServer) serveOn(lis net.Listener) error {
	interceptor, err := validate.UnaryInterceptor()
	if err != nil {
		return err
	}
	grpcServer := grpc.NewServer(grpc.UnaryInterceptor(interceptor))
	pb.RegisterFilesServiceServer(grpcServer, s)
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	slog.Info("listening", "addr", s.addr)
	return grpcServer.Serve(lis)
}

// dbFileToProto maps the file_infos read model to the API shape. Metadata
// fields are NULL until a file is stat-indexed; timestamps stay unset (nil)
// rather than encoding the zero time. created_at is discovery time and
// updated_at is the object's last-modified time from the stat index.
// preview_key/width/height come from the preview index and are empty/zero
// until it runs (or the file isn't an image).
func dbFileToProto(f db.FileInfo) *pb.FileInfo {
	info := &pb.FileInfo{
		Id:            f.ID,
		Key:           f.Key,
		ContentType:   f.ContentType.String,
		SizeBytes:     f.SizeBytes.Int64,
		PreviewKey:    f.PreviewKey.String,
		PreviewWidth:  f.PreviewWidth.Int32,
		PreviewHeight: f.PreviewHeight.Int32,
	}
	if f.CreatedAt.Valid {
		info.CreatedAt = timestamppb.New(f.CreatedAt.Time)
	}
	if f.LastModified.Valid {
		info.UpdatedAt = timestamppb.New(f.LastModified.Time)
	}
	return info
}

// exifToProto maps an index_exif_result row to the API shape. Every scalar
// is nullable in the schema (see internal/db/migrations/003_exif.sql), so
// each field is converted individually rather than zero-valued: a NULL must
// stay unset, not become e.g. ISO 0 or rating 0.
func exifToProto(e db.IndexExifResult) *pb.ExifMetadata {
	m := &pb.ExifMetadata{
		ImageType:        textPtr(e.ImageType),
		CameraMake:       textPtr(e.CameraMake),
		CameraModel:      textPtr(e.CameraModel),
		CameraSerial:     textPtr(e.CameraSerial),
		LensMake:         textPtr(e.LensMake),
		LensModel:        textPtr(e.LensModel),
		Iso:              int4Ptr(e.Iso),
		FNumber:          float4Ptr(e.FNumber),
		ExposureTime:     float4Ptr(e.ExposureTime),
		FocalLength:      float4Ptr(e.FocalLength),
		FocalLength_35Mm: float4Ptr(e.FocalLength35mm),
		ExposureProgram:  int2Ptr(e.ExposureProgram),
		MeteringMode:     int2Ptr(e.MeteringMode),
		Flash:            int2Ptr(e.Flash),
		Orientation:      int2Ptr(e.Orientation),
		ImageWidth:       int4Ptr(e.ImageWidth),
		ImageHeight:      int4Ptr(e.ImageHeight),
		GpsLatitude:      float8Ptr(e.GpsLatitude),
		GpsLongitude:     float8Ptr(e.GpsLongitude),
		GpsAltitude:      float4Ptr(e.GpsAltitude),
		Software:         textPtr(e.Software),
		Artist:           textPtr(e.Artist),
		Copyright:        textPtr(e.Copyright),
		ImageDescription: textPtr(e.ImageDescription),
		XmpTitle:         textPtr(e.XmpTitle),
		XmpDescription:   textPtr(e.XmpDescription),
		XmpCreator:       textPtr(e.XmpCreator),
		XmpLabel:         textPtr(e.XmpLabel),
		XmpRating:        int2Ptr(e.XmpRating),
		XmpKeywords:      e.XmpKeywords,
		HasExif:          e.HasExif,
		HasXmp:           e.HasXmp,
	}
	if e.TakenAt.Valid {
		m.TakenAt = timestamppb.New(e.TakenAt.Time)
	}
	if e.GpsAt.Valid {
		m.GpsAt = timestamppb.New(e.GpsAt.Time)
	}
	if e.XmpCreateDate.Valid {
		m.XmpCreateDate = timestamppb.New(e.XmpCreateDate.Time)
	}
	return m
}

func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

func int2Ptr(i pgtype.Int2) *int32 {
	if !i.Valid {
		return nil
	}
	v := int32(i.Int16)
	return &v
}

func int4Ptr(i pgtype.Int4) *int32 {
	if !i.Valid {
		return nil
	}
	return &i.Int32
}

func float4Ptr(f pgtype.Float4) *float32 {
	if !f.Valid {
		return nil
	}
	return &f.Float32
}

func float8Ptr(f pgtype.Float8) *float64 {
	if !f.Valid {
		return nil
	}
	return &f.Float64
}
