package main

import (
	"file-indexer/internal/config"
	"file-indexer/internal/db"
	"log"
	"os"
)

func main() {
	cfg := config.Load()

	if err := db.RunMigrations("pgx", cfg.Database.URL()); err != nil {
		log.Fatal(err)
	}

	log.Println("Migrations complete")
	os.Exit(0)
}
