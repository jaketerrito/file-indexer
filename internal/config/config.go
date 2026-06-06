package config

import (
	"fmt"
	"os"
)

type Config struct {
	GrpcAddr    string
	DatabaseURL string
}

func Load() *Config {
	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")
	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")
	dbname := os.Getenv("DB_NAME")

	databaseURL := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname)

	return &Config{
		GrpcAddr:    os.Getenv("GRPC_ADDR"),
		DatabaseURL: databaseURL,
	}
}
