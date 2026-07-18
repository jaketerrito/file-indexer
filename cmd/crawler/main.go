// The crawler is a one-shot reconciliation job: it walks the S3 bucket
// listing and upserts a files row per object. It does not talk to the
// indexer; indexer worker pools discover work by seeding from the files
// table. Files whose size/last-modified changed get their stat index state
// reset so they are re-indexed.
package main

import (
	"context"
	"file-indexer/internal/config"
	"file-indexer/internal/db"
	"file-indexer/internal/logger"
	"file-indexer/internal/service/crawler"
	"file-indexer/internal/storage"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	logger.Setup(slog.LevelInfo)
	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	s3, err := storage.New(cfg.S3.Endpoint, cfg.S3.AccessKeyID, cfg.S3.SecretAccessKey, false, cfg.S3.Bucket, cfg.S3.Region)
	if err != nil {
		slog.Error("storage init failed", "error", err)
		os.Exit(1)
	}

	pool, err := pgxpool.New(ctx, cfg.Database.URL())
	if err != nil {
		slog.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	c := crawler.New(s3, db.New(pool))
	if err := c.Run(ctx); err != nil {
		slog.Error("crawler failed", "error", err)
		os.Exit(1)
	}
}
