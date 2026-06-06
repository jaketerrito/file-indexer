package main

import (
	"log"
	"os"

	"file-indexer/internal/config"
	"file-indexer/internal/db"
)

func main() {
	cfg := config.Load()

	if err := db.RunMigrations("pgx", cfg.DatabaseURL); err != nil {
		log.Fatal(err)
	}

	log.Println("Migrations complete")
	os.Exit(0)
}
