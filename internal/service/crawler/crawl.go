package crawler

import (
	"log/slog"
	"context"
	"google.golang.org/grpc/credentials/insecure"
	"file-indexer/internal/pb"
	"file-indexer/internal/service/crawler/walker"
	"google.golang.org/grpc"
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
	defer conn.Close()

	client := pb.NewIndexerClient(conn)

	localFs := walker.LinuxFileWalker{}
	return localFs.Walk(func(file walker.FileInfo) error {
		_, err := client.Index(context.Background(), &pb.IndexRequest{
			Name: file.Path,
		})
		slog.Info("Sent file to indexer", "FileInfo", file)
		return err
	})
}
