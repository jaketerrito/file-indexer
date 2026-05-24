package main

import (
	"log/slog"
	"os"
	"file-indexer/internal/config"
	"file-indexer/internal/service/crawler"
)

func main() {
	cfg := config.Load()
	if err := crawler.Run(cfg.GrpcAddr); err != nil {
		slog.Error("Crawler failed", "error", err)
		os.Exit(1)
	}
}
