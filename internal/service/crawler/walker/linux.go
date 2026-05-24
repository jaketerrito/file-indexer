package walker

import (
	"io/fs"
	"path/filepath"
)

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
