package main

import (
	"file-indexer/internal/config"
	"file-indexer/internal/service/indexer"
	"log"
)

func main() {
	cfg := config.Load()
	s := &indexer.IndexerServer{}
	log.Fatal(s.Run(cfg.GrpcAddr, cfg.Database.URL()))
}
