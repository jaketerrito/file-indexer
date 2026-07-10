package main

import (
	"file-indexer/internal/config"
	"file-indexer/internal/logger"
	pb "file-indexer/internal/pb/service/v1"
	"file-indexer/internal/service/crawler"
	"file-indexer/internal/storage"
	"log/slog"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	logger.Setup(slog.LevelInfo)
	cfg := config.Load()

	s3, err := storage.New(cfg.S3.Endpoint, cfg.S3.AccessKeyID, cfg.S3.SecretAccessKey, false, cfg.S3.Bucket, cfg.S3.Region)
	if err != nil {
		slog.Error("storage init failed", "error", err)
		os.Exit(1)
	}

	slog.Info("connecting to indexer", "addr", cfg.GrpcAddr)
	conn, err := grpc.NewClient(cfg.GrpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		slog.Error("indexer connection failed", "error", err)
		os.Exit(1)
	}
	defer func() { _ = conn.Close() }()

	client := pb.NewIndexerServiceClient(conn)

	c := crawler.New(s3, client)
	if err := c.Run(); err != nil {
		slog.Error("Crawler failed", "error", err)
		os.Exit(1)
	}
}
