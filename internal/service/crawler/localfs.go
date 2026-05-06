package main

import (
	"os"
	"net/http"
	"time"
	"github.com/djherbis/times"
	"path/filepath"
	"io/fs"
)

type LinuxFileSystem struct {}

func getMimeType(path string) (string, error) {
	// Open file
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	// Only first 512 bytes necessary for DetectContentType
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil {
		return "", err
	}

	return http.DetectContentType(buf[:n]), nil

}

func getCreationTime(path string, fileInfo fs.FileInfo) time.Time {
	creationTime := fileInfo.ModTime()
	t, errTime := times.Stat(path)
	if errTime == nil && t.HasBirthTime() {
		creationTime = t.BirthTime()
	}
	return creationTime
}

func (l LinuxFileSystem) Walk(fn FileHandler) error {
	return filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}
		fileInfo, err := d.Info()
		if err != nil {
			return err
		}
		modifiedTime := fileInfo.ModTime()
		creationTime := getCreationTime(path, fileInfo)
		mimeType, err := getMimeType(path)
		if err != nil {
			return err
		}
		return fn(FileMetadata{Path: path, CreationTime: creationTime, ModTime: modifiedTime, Size: fileInfo.Size(), MimeType: mimeType})
	})
}


