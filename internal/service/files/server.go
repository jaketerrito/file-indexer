package files

import (
	"context"
	"file-indexer/internal/db"
	pb "file-indexer/internal/pb/service/v1"
	"file-indexer/internal/storage"
	"log/slog"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type FilesServer struct {
	pb.UnimplementedFilesServiceServer
	addr    string
	storage storage.Storage
	queries *db.Queries
}

func New(addr string, store storage.Storage, queries *db.Queries) *FilesServer {
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

func dbFileToProto(f db.File) *pb.FileInfo {
	return &pb.FileInfo{
		Id:          f.ID,
		Key:         f.Key,
		ContentType: f.ContentType.String,
		SizeBytes:   f.SizeBytes.Int64,
		CreatedAt:   timestamppb.New(f.CreatedAt.Time),
		UpdatedAt:   timestamppb.New(f.UpdatedAt.Time),
	}
}
