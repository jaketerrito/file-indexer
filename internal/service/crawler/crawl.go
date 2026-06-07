package crawler

import (
	"context"
	"file-indexer/internal/pb"
	"io"
	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func Run(address string) error {
	slog.Info("port", "is", address)
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	conn, err := grpc.NewClient("localhost:50051", opts...)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	client := pb.NewIndexerClient(conn)

	localFs := LinuxFileWalker{}
	return localFs.Walk(func(file FileInfo) error {
		r, err := localFs.Open(file)
		if err != nil {
			return err
		}
		defer func() { _ = r.Close() }()

		stream, err := client.Index(context.Background())
		if err != nil {
			return err
		}

		if err := stream.Send(&pb.IndexRequest{
			Data: &pb.IndexRequest_Metadata{
				Metadata: &pb.FileMetadata{
					Name:   file.Path,
					Source: "test",
				},
			},
		}); err != nil {
			return err
		}

		buf := make([]byte, 64*1024)
		for {
			n, err := r.Read(buf)
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}

			if n > 0 {
				if err := stream.Send(&pb.IndexRequest{
					Data: &pb.IndexRequest_Content{Content: buf[:n]},
				}); err != nil {
					return err
				}
			}
		}

		slog.Info("Sent file to indexer", "FileInfo", file)
		response, err := stream.CloseAndRecv()
		if err != nil {
			return err
		}
		slog.Info("uploaded", "file", file.Path, "status", response.Status)
		return nil
	})
}
