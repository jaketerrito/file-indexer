// Package indexer implements the stat index type for the worker pool: the
// handler that computes basic object metadata (content type, size, mtime)
// via S3 Stat, plus the queue adapter that binds the generic worker
// framework to the index_stat table where both queue state and results
// live.
package indexer
