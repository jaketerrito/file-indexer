// The preview GC is a one-shot cleanup job: it deletes preview objects under
// the index prefix that no index_preview_result row claims (orphaned by
// out-of-band S3 deletes or failed best-effort cleanups). Schedule it like
// the crawler; each run over a clean bucket deletes nothing.
package main

import (
	"context"
	"file-indexer/internal/config"
	"file-indexer/internal/db"
	"file-indexer/internal/logger"
	"file-indexer/internal/service/previewgc"
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

	gc := previewgc.New(s3, db.NewStore(pool), cfg.IndexPrefix)
	if err := gc.Run(ctx); err != nil {
		slog.Error("preview gc failed", "error", err)
		os.Exit(1)
	}
}
