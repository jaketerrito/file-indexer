package config

import (
	"fmt"
	"os"
)

type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
}

func (d DatabaseConfig) URL() string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		d.Host, d.Port, d.User, d.Password, d.DBName)
}

type S3Config struct {
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
}

type Config struct {
	GrpcAddr string
	Database DatabaseConfig
	S3       S3Config
}

func getEnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func Load() *Config {
	return &Config{
		GrpcAddr: getEnvDefault("GRPC_ADDR", ":50051"),
		Database: DatabaseConfig{
			Host:     os.Getenv("DB_HOST"),
			Port:     os.Getenv("DB_PORT"),
			User:     os.Getenv("DB_USER"),
			Password: os.Getenv("DB_PASSWORD"),
			DBName:   os.Getenv("DB_NAME"),
		},
		S3: S3Config{
			Endpoint:        os.Getenv("S3_ENDPOINT"),
			AccessKeyID:     os.Getenv("S3_ACCESS_ID"),
			SecretAccessKey: os.Getenv("S3_SECRET"),
			Bucket:          os.Getenv("S3_BUCKET"),
		},
	}
}
