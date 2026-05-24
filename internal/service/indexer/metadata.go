package indexer

import (
	"io/fs"
	"net/http"
	"os"
	"time"
	"github.com/djherbis/times"
)
func getMimeType(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

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


