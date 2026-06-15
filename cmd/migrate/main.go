package main

import (
	"file-indexer/internal/config"
	"file-indexer/internal/db"
	"file-indexer/internal/logger"
	"log/slog"
	"os"
)

func main() {
	logger.Setup(slog.LevelInfo)
	cfg := config.Load()

	if err := db.RunMigrations("pgx", cfg.Database.URL()); err != nil {
		slog.Error("migrations failed", "error", err)
		os.Exit(1)
	}

	slog.Info("Migrations complete")
	os.Exit(0)
}
