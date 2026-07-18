// Package indexer implements the stat index type for the worker pool: the
// handler that computes basic object metadata (content type, size, mtime)
// via S3 Stat and writes it to the files table, plus the queue adapter that
// binds the generic worker framework to the index_stat table.
package indexer
