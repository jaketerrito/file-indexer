package main

import (
	"log"
	"file-indexer/internal/service/indexer"
	"file-indexer/internal/config"
)

func main() {
	cfg := config.Load()
	s := &indexer.IndexerServer{}
	log.Fatal(s.Run(cfg.GrpcAddr))
}
