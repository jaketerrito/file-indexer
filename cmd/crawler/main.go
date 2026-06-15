package main

import (
	"file-indexer/internal/config"
	"file-indexer/internal/logger"
	"file-indexer/internal/service/crawler"
	"file-indexer/internal/storage"
	"log/slog"
	"os"
)

func main() {
	logger.Setup(slog.LevelInfo)
	cfg := config.Load()

	s3, err := storage.New(cfg.S3.Endpoint, cfg.S3.AccessKeyID, cfg.S3.SecretAccessKey, false, cfg.S3.Bucket)
	if err != nil {
		slog.Error("storage init failed", "error", err)
		os.Exit(1)
	}

	c := crawler.New(cfg.GrpcAddr, s3)
	if err := c.Run(); err != nil {
		slog.Error("Crawler failed", "error", err)
		os.Exit(1)
	}
}
