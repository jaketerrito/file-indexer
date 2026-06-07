package crawler

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

type FileInfo struct {
	Path   string
	Source string
}

type FileWalker interface {
	Walk(fn func(FileInfo) error) error
	Open(FileInfo) (io.ReadCloser, error)
}

type LinuxFileWalker struct{}

func (l LinuxFileWalker) Walk(fn func(FileInfo) error) error {
	return filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		return fn(FileInfo{Path: path, Source: "test"})
	})
}

func (l LinuxFileWalker) Open(fileInfo FileInfo) (io.ReadCloser, error) {
	return os.Open(fileInfo.Path)
}
