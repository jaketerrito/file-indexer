package config

import (
	"testing"
	"time"
)

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
	t.Setenv("S3_REGION", "eu-west-2")

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
	if cfg.S3.Region != "eu-west-2" {
		t.Errorf("S3 Region = %q, want %q", cfg.S3.Region, "eu-west-2")
	}
}

func TestLoadIndexerConfig(t *testing.T) {
	t.Setenv("INDEXER_POLL_INTERVAL", "3s")
	t.Setenv("INDEXER_BATCH_SIZE", "16")
	t.Setenv("INDEXER_MAX_ATTEMPTS", "7")
	t.Setenv("INDEXER_CLAIM_TTL", "15m")
	t.Setenv("INDEXER_BACKOFF_BASE", "20s")
	t.Setenv("INDEXER_BACKOFF_MAX", "5m")

	w := Load().Indexer
	if w.PollInterval != 3*time.Second {
		t.Errorf("PollInterval = %v, want 3s", w.PollInterval)
	}
	if w.BatchSize != 16 {
		t.Errorf("BatchSize = %d, want 16", w.BatchSize)
	}
	if w.MaxAttempts != 7 {
		t.Errorf("MaxAttempts = %d, want 7", w.MaxAttempts)
	}
	if w.ClaimTTL != 15*time.Minute {
		t.Errorf("ClaimTTL = %v, want 15m", w.ClaimTTL)
	}
	if w.BackoffBase != 20*time.Second {
		t.Errorf("BackoffBase = %v, want 20s", w.BackoffBase)
	}
	if w.BackoffMax != 5*time.Minute {
		t.Errorf("BackoffMax = %v, want 5m", w.BackoffMax)
	}
}

func TestLoadIndexerConfigUnsetAndInvalid(t *testing.T) {
	// Unset and malformed values fall back to built-in defaults.
	t.Setenv("INDEXER_POLL_INTERVAL", "not-a-duration")

	w := Load().Indexer
	if w.PollInterval != 5*time.Second {
		t.Errorf("PollInterval = %v, want 5s default for malformed value", w.PollInterval)
	}
	if w.BatchSize != 32 {
		t.Errorf("BatchSize = %d, want 32 default when unset", w.BatchSize)
	}
	if w.ClaimTTL != 10*time.Minute {
		t.Errorf("ClaimTTL = %v, want 10m default when unset", w.ClaimTTL)
	}
}

func TestNormalizeIndexPrefix(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"/", ""},
		{".index", ".index/"},
		{".index/", ".index/"},
		{"/a/b/", "a/b/"},
		{"previews", "previews/"},
	}
	for _, c := range cases {
		if got := normalizeIndexPrefix(c.in); got != c.want {
			t.Errorf("normalizeIndexPrefix(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLoadIndexPrefixDefault(t *testing.T) {
	if got := Load().IndexPrefix; got != ".index/" {
		t.Errorf("default IndexPrefix = %q, want %q", got, ".index/")
	}
}

func TestLoadIndexPrefixFromEnv(t *testing.T) {
	t.Setenv("INDEX_PREFIX", "/custom/")
	if got := Load().IndexPrefix; got != "custom/" {
		t.Errorf("IndexPrefix = %q, want %q", got, "custom/")
	}
}

func TestLoadPreviewConfig(t *testing.T) {
	t.Setenv("PREVIEW_MAX_DIM", "64")
	t.Setenv("PREVIEW_QUALITY", "50")
	t.Setenv("PREVIEW_MAX_SOURCE_BYTES", "123")
	t.Setenv("PREVIEW_MAX_SOURCE_PIXELS", "456")

	p := Load().Preview
	if p.MaxDim != 64 {
		t.Errorf("MaxDim = %d, want 64", p.MaxDim)
	}
	if p.Quality != 50 {
		t.Errorf("Quality = %d, want 50", p.Quality)
	}
	if p.MaxSourceBytes != 123 {
		t.Errorf("MaxSourceBytes = %d, want 123", p.MaxSourceBytes)
	}
	if p.MaxSourcePixels != 456 {
		t.Errorf("MaxSourcePixels = %d, want 456", p.MaxSourcePixels)
	}
}

func TestLoadPreviewConfigDefaults(t *testing.T) {
	t.Setenv("PREVIEW_MAX_DIM", "not-an-int")

	p := Load().Preview
	if p.MaxDim != 320 {
		t.Errorf("MaxDim = %d, want 320 default for malformed value", p.MaxDim)
	}
	if p.Quality != 80 {
		t.Errorf("Quality = %d, want 80 default when unset", p.Quality)
	}
	if p.MaxSourceBytes != 64<<20 {
		t.Errorf("MaxSourceBytes = %d, want %d default when unset", p.MaxSourceBytes, int64(64<<20))
	}
	if p.MaxSourcePixels != 40_000_000 {
		t.Errorf("MaxSourcePixels = %d, want 40000000 default when unset", p.MaxSourcePixels)
	}
}

func TestLoadExifConfig(t *testing.T) {
	t.Setenv("EXIF_MAX_HEADER_BYTES", "123")
	t.Setenv("EXIF_MAX_SOURCE_BYTES", "456")

	e := Load().Exif
	if e.MaxHeaderBytes != 123 {
		t.Errorf("MaxHeaderBytes = %d, want 123", e.MaxHeaderBytes)
	}
	if e.MaxSourceBytes != 456 {
		t.Errorf("MaxSourceBytes = %d, want 456", e.MaxSourceBytes)
	}
}

func TestLoadExifConfigDefaults(t *testing.T) {
	t.Setenv("EXIF_MAX_HEADER_BYTES", "not-an-int")

	e := Load().Exif
	if e.MaxHeaderBytes != 1<<20 {
		t.Errorf("MaxHeaderBytes = %d, want %d default for malformed value", e.MaxHeaderBytes, int64(1<<20))
	}
	if e.MaxSourceBytes != 64<<20 {
		t.Errorf("MaxSourceBytes = %d, want %d default when unset", e.MaxSourceBytes, int64(64<<20))
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
