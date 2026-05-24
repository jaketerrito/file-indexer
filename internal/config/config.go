package config

import "os"

type Config struct {
	GrpcAddr string
}

func Load() *Config {
	return &Config{
		GrpcAddr: os.Getenv("GRPC_ADDR"),
	}
}
