package walker

import (
	"io"
	"time"
)

type FileInfo struct {
	Path   string
	Source string
	CreationTime time.Time
}

type FileWalker interface {
	Walk(fn func(FileInfo, ) error) error
	Open(FileInfo) (io.ReadCloser, error)
}
