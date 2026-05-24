package indexer

import (
	"time"
)

type FileMetadata struct {
	Path string
	Size int64
	ModTime time.Time
	CreationTime time.Time
	MimeType string
}
