package files

import (
	"context"
	"errors"
	"file-indexer/internal/db"
	pb "file-indexer/internal/pb/service/v1"
	"file-indexer/internal/storage"
	"log/slog"
	"net"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
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
	Delete(ctx context.Context, key string) error
	// DeleteMany is used by DeleteDirectory to batch-remove a subtree's
	// objects. See its doc comment on the not-atomic contract this implies.
	DeleteMany(ctx context.Context, keys []string) error
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
	UpsertFilesWithDirectories(ctx context.Context, arg db.UpsertFilesParams) (int64, error)
	// GetIndexExifResult returns pgx.ErrNoRows when the file has neither EXIF
	// nor XMP data (or hasn't reached the exif indexer yet).
	GetIndexExifResult(ctx context.Context, fileID int64) (db.IndexExifResult, error)
	GetDirectoryStats(ctx context.Context, keyPattern string) (db.GetDirectoryStatsRow, error)
	ListFilesForDelete(ctx context.Context, arg db.ListFilesForDeleteParams) ([]db.ListFilesForDeleteRow, error)
	DeleteFilesByIDsWithDirectories(ctx context.Context, ids []int64, keys []string) (int64, error)
	GetIndexQueueStatuses(ctx context.Context, arg db.GetIndexQueueStatusesParams) ([]db.GetIndexQueueStatusesRow, error)
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

// New constructs an IndexerServer with its dependencies already built by the
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
	return &pb.CommitUploadResponse{File: dbFileToProto(file)}, nil
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
	grpcServer := grpc.NewServer()
	pb.RegisterFilesServiceServer(grpcServer, s)
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
