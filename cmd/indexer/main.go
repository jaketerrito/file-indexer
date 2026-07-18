// The indexer is a worker-pool daemon (no server): it polls the index_stat
// queue table in postgres for files needing stat indexing, fetches metadata
// from S3, and records the results. Files enter the queue by self-seeding
// from the files table, which the crawler (and later the API) populates.
package main

import (
	"context"
	"file-indexer/internal/config"
	"file-indexer/internal/db"
	"file-indexer/internal/logger"
	"file-indexer/internal/service/indexer"
	"file-indexer/internal/storage"
	"file-indexer/internal/worker"
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

	pool, err := pgxpool.New(ctx, cfg.Database.URL())
	if err != nil {
		slog.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	s3, err := storage.New(cfg.S3.Endpoint, cfg.S3.AccessKeyID, cfg.S3.SecretAccessKey, false, cfg.S3.Bucket, cfg.S3.Region)
	if err != nil {
		slog.Error("storage init failed", "error", err)
		os.Exit(1)
	}

	queries := db.New(pool)
	statPool := worker.New("stat",
		worker.Config{
			Workers:      cfg.Worker.Workers,
			PollInterval: cfg.Worker.PollInterval,
			SeedInterval: cfg.Worker.SeedInterval,
			BatchSize:    int32(cfg.Worker.BatchSize),
			MaxAttempts:  int32(cfg.Worker.MaxAttempts),
			ClaimTTL:     cfg.Worker.ClaimTTL,
		},
		indexer.NewStatQueue(queries),
		indexer.NewStatIndexer(s3),
	)

	if err := statPool.Run(ctx); err != nil {
		slog.Error("indexer failed", "error", err)
		os.Exit(1)
	}
}
