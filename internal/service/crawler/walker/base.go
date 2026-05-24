package walker

type FileInfo struct {
	Path   string
	Source string
}

type FileWalker interface {
	Walk(fn func(FileInfo) error) error
}