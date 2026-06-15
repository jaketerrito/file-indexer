package crawler

import (
	"file-indexer/internal/pb"
	"io/fs"
	"path/filepath"
)

type FileWalker interface {
	Walk(fn func(*pb.FileRef) error) error
}

type LinuxFileWalker struct{}

func (l LinuxFileWalker) Walk(fn func(*pb.FileRef) error) error {
	return filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		return fn(&pb.FileRef{Path: path, Bucket: "test"})
	})
}
