package main

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

type FileHandler func(file FileMetadata) error

type FileSystem interface {
	Walk(fn FileHandler) error
}
