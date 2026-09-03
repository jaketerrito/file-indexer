// Package crawler reconciles the S3 bucket listing with the files table: it
// walks the listing and upserts one files row per object, then sweeps rows
// whose objects were deleted out of band. It does not talk to the indexer;
// index workers seed their queues from the files table.
package crawler
