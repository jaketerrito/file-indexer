package files

import (
	"context"
	"file-indexer/internal/db"
	pb "file-indexer/internal/pb/service/v1"
	"log/slog"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type ObjectStore interface {
	GetURL(ctx context.Context, key string) (string, error)
	GetInlineURL(ctx context.Context, key string) (string, error)
	Delete(ctx context.Context, key string) error
}

type FileIndex interface {
	GetFile(ctx context.Context, id int64) (db.FileInfo, error)
	GetFilesByIDs(ctx context.Context, ids []int64) ([]db.FileInfo, error)
	DeleteFile(ctx context.Context, id int64) (db.File, error)
}

type FilesServer struct {
	pb.UnimplementedFilesServiceServer
	addr    string
	storage ObjectStore
	queries FileIndex
}

// New constructs an IndexerServer with its dependencies already built by the
// caller (composition root). It does no I/O; call Serve to start listening.
func New(addr string, store ObjectStore, queries FileIndex) *FilesServer {
	return &FilesServer{
		addr:    addr,
		storage: store,
		queries: queries,
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

func (s *FilesServer) GetFileInfo(ctx context.Context, req *pb.GetFileInfoRequest) (*pb.GetFileInfoResponse, error) {
	file, err := s.queries.GetFile(ctx, req.GetId())
	if err != nil {
		return nil, err
	}
	return &pb.GetFileInfoResponse{File: dbFileToProto(file)}, nil
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
	// reconciles leftovers (see NOTES.md).
	if file.PreviewKey.Valid && file.PreviewKey.String != "" {
		if err := s.storage.Delete(ctx, file.PreviewKey.String); err != nil {
			slog.Warn("delete preview object", "key", file.PreviewKey.String, "error", err)
		}
	}
	if _, err := s.queries.DeleteFile(ctx, req.GetId()); err != nil {
		return nil, err
	}
	return &pb.DeleteFileResponse{}, nil
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
