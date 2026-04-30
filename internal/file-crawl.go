package main

import ( 
	"log/slog"
	"path/filepath"
	"io/fs"
	"sync"
	"time"
	"github.com/djherbis/times"
)

// File System Abstractions
type FileMetadata struct {
	Path string
	Size int64
	ModTime time.Time
	CreationTime time.Time
}

type FileHandler func(file FileMetadata) error

type FileSystem interface {
	Walk(fn FileHandler) error
}

// Linux FS Implemenetations
type LinuxFileSystem struct {}

func (l LinuxFileSystem) Walk(fn FileHandler) error {
	return filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}
		fileInfo, err := d.Info()
		modifiedTime := fileInfo.ModTime()

		creationTime := modifiedTime
		t, errTime := times.Stat(path)
		if errTime != nil && t.HasBirthTime() {
			creationTime = t.BirthTime()
		}
		return fn(FileMetadata{Path: path, CreationTime: creationTime, ModTime: modifiedTime, Size: fileInfo.Size()})
	})
}

// File processing
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

