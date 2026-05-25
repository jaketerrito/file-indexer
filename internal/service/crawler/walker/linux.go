package walker

import (
	"io/fs"
	"path/filepath"
	"io"
	"github.com/djherbis/times"
	"os"
	"time"
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

		info, err := d.Info()
		if err != nil {
			return err
		}
		creationTime := getCreationTime(path, info)

		return fn(FileInfo{Path: path, Source: "test", CreationTime: creationTime})
	})
}

func (l LinuxFileWalker) Open(fileInfo FileInfo) (io.ReadCloser, error) {
	return os.Open(fileInfo.Path)
}


func getCreationTime(path string, fileInfo fs.FileInfo) time.Time {
	creationTime := fileInfo.ModTime()
	t, errTime := times.Stat(path)
	if errTime == nil && t.HasBirthTime() {
		creationTime = t.BirthTime()
	}
	return creationTime
}
