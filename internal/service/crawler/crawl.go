package main

import ( 
	"log/slog"
	"sync"
)

// Consider having this just use the path, call the relevant method pased of fs, then send off to indexer
func processFile(file FileMetadata) error {
	slog.Info("processed file:", "file", file)
	return nil
}

func worker(id int, files <-chan FileMetadata) {
	for file := range files {
		if err := processFile(file); err != nil {
			slog.Error("Processing failed", "worker_id", id, "path", file.Path, "error", err)
		}
	}
}

const workerCount = 5

func main() {
	// create a worker pool to process files
	files := make(chan FileMetadata, workerCount)
	var wg sync.WaitGroup
	for i := 1; i <= workerCount; i++ {
		wg.Go(func() {
			worker(i, files)
		})
	}

	// process the files
	localFs := LinuxFileSystem{}
	err := localFs.Walk(func(file FileMetadata) error {
		files <- file
		return nil
	})
	if err != nil {
		slog.Error("Failed", "error", err)
	}

	// Wait for workers to finish
	close(files)
	wg.Wait()
}

