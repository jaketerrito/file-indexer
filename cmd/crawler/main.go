package main

import (
	"log/slog"
	"os"

	"file-indexer/internal/service/crawler"
)

func main() {
	if err := crawler.Run(); err != nil {
		slog.Error("Crawler failed", "error", err)
		os.Exit(1)
	}
}