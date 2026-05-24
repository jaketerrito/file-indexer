package crawler

import (
	"log/slog"

	"file-indexer/internal/service/crawler/walker"
)

func Run() error {
	localFs := walker.LinuxFileWalker{}
	return localFs.Walk(func(file walker.FileInfo) error {
		slog.Info("Sending file to indexer", "FileInfo", file)
		return nil
	})
}