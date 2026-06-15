package main

import (
	"file-indexer/internal/config"
	"file-indexer/internal/logger"
	"file-indexer/internal/service/crawler"
	"log/slog"
	"os"
)

func main() {
	logger.Setup(slog.LevelInfo)
	cfg := config.Load()

	c := crawler.New(cfg.GrpcAddr, crawler.LinuxFileWalker{})
	if err := c.Run(); err != nil {
		slog.Error("Crawler failed", "error", err)
		os.Exit(1)
	}
}
