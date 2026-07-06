package config

import "testing"

func TestDatabaseConfigURL(t *testing.T) {
	cfg := DatabaseConfig{
		Host: "localhost", Port: "5432", User: "u", Password: "p", DBName: "d",
	}
	want := "host=localhost port=5432 user=u password=p dbname=d sslmode=disable"
	if got := cfg.URL(); got != want {
		t.Errorf("URL() = %q, want %q", got, want)
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("GRPC_ADDR", ":9999")
	t.Setenv("DB_HOST", "h")
	t.Setenv("DB_PORT", "1")
	t.Setenv("DB_USER", "u")
	t.Setenv("DB_PASSWORD", "pw")
	t.Setenv("DB_NAME", "n")
	t.Setenv("S3_ENDPOINT", "e")
	t.Setenv("S3_PUBLIC_ENDPOINT", "pub")
	t.Setenv("S3_ACCESS_ID", "id")
	t.Setenv("S3_SECRET", "secret")
	t.Setenv("S3_BUCKET", "b")

	cfg := Load()
	if cfg.GrpcAddr != ":9999" {
		t.Errorf("GrpcAddr = %q, want %q", cfg.GrpcAddr, ":9999")
	}
	if cfg.Database.Host != "h" {
		t.Errorf("DB Host = %q, want %q", cfg.Database.Host, "h")
	}
	if cfg.S3.Endpoint != "e" {
		t.Errorf("S3 Endpoint = %q, want %q", cfg.S3.Endpoint, "e")
	}
	if cfg.S3.PublicEndpoint != "pub" {
		t.Errorf("S3 PublicEndpoint = %q, want %q", cfg.S3.PublicEndpoint, "pub")
	}
}

func TestLoadDefaultGRPCAddr(t *testing.T) {
	t.Setenv("DB_HOST", "h")
	t.Setenv("DB_PORT", "1")
	t.Setenv("DB_USER", "u")
	t.Setenv("DB_PASSWORD", "pw")
	t.Setenv("DB_NAME", "n")
	t.Setenv("S3_ENDPOINT", "e")
	t.Setenv("S3_ACCESS_ID", "id")
	t.Setenv("S3_SECRET", "secret")
	t.Setenv("S3_BUCKET", "b")

	cfg := Load()
	if cfg.GrpcAddr != ":50051" {
		t.Errorf("default GrpcAddr = %q, want %q", cfg.GrpcAddr, ":50051")
	}
}
