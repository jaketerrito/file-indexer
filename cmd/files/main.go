package main

import (
	"context"
	"file-indexer/internal/config"
	"file-indexer/internal/db"
	"file-indexer/internal/logger"
	"file-indexer/internal/service/files"
	"file-indexer/internal/storage"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	logger.Setup(slog.LevelInfo)
	cfg := config.Load()

	pool, err := pgxpool.New(context.Background(), cfg.Database.URL())
	if err != nil {
		slog.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	s3, err := storage.NewWithPublicEndpoint(cfg.S3.Endpoint, cfg.S3.PublicEndpoint, cfg.S3.AccessKeyID, cfg.S3.SecretAccessKey, false, cfg.S3.Bucket, cfg.S3.Region)
	if err != nil {
		slog.Error("storage init failed", "error", err)
		os.Exit(1)
	}

	srv := files.New(cfg.GrpcAddr, s3, db.New(pool), cfg.IndexPrefix)
	if err := srv.Serve(); err != nil {
		slog.Error("files service failed", "error", err)
		os.Exit(1)
	}
}
