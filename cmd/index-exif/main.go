// The exif indexer is a stateless daemon (no server): it polls the
// index_queue table for files needing EXIF/XMP extraction, downloads
// image/RAW objects from S3, and stores decoded metadata. Files that are not
// a supported type, or carry no EXIF/XMP at all, are marked done with no
// result row. It has no in-process concurrency; scale out by running more
// replicas.
package main

import (
	"context"
	"file-indexer/internal/config"
	"file-indexer/internal/logger"
	"file-indexer/internal/service/indexer"
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

	queue := indexer.NewPGQueue(pool, "exif", indexer.StoreExifResult)
	exifIndexer := indexer.NewExifIndexer(s3, cfg.IndexPrefix, indexer.ExifConfig(cfg.Exif))

	indexerCfg := indexer.Config(cfg.Indexer)

	if err := indexer.Run(ctx, "exif", indexerCfg, queue, exifIndexer.Process); err != nil {
		slog.Error("exif indexer failed", "error", err)
		os.Exit(1)
	}
}
