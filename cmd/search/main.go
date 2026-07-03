package main

import (
	"context"
	"file-indexer/internal/config"
	"file-indexer/internal/db"
	"file-indexer/internal/logger"
	"file-indexer/internal/service/search"
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

	srv := search.New(cfg.GrpcAddr, db.New(pool))
	if err := srv.Serve(); err != nil {
		slog.Error("search service failed", "error", err)
		os.Exit(1)
	}
}
