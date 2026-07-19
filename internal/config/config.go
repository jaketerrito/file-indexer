package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"
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
	Endpoint string
	// PublicEndpoint, when set, is the endpoint presigned URLs are signed
	// against so they are reachable by clients outside the cluster network
	// (e.g. browsers). Empty means presign against Endpoint.
	PublicEndpoint  string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
	// Region is used for SigV4 request signing. When empty, the minio
	// client falls back to its default ("us-east-1").
	Region string
}

// IndexerConfig tunes the indexer's claim/process loop. Zero values use
// built-in defaults set during Load, so only the knobs that matter need
// to be set.
type IndexerConfig struct {
	// PollInterval is the sleep between polls when the queue is empty.
	PollInterval time.Duration
	// BatchSize is the maximum number of jobs claimed per poll.
	BatchSize int32
	// MaxAttempts is the number of tries before a job is parked as an error.
	MaxAttempts int32
	// ClaimTTL is how long a claim may be held before it is presumed
	// abandoned (crashed instance) and re-claimed.
	ClaimTTL time.Duration
	// BackoffBase is the retry delay after the first failure; it doubles
	// per attempt up to BackoffMax.
	BackoffBase time.Duration
	// BackoffMax caps the exponential retry delay.
	BackoffMax time.Duration
}

type Config struct {
	GrpcAddr string
	Database DatabaseConfig
	S3       S3Config
	Indexer   IndexerConfig
}

func getEnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// getEnvInt parses key as an int. Unset or malformed returns def.
func getEnvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		slog.Warn("invalid int in environment, using default", "key", key, "value", v)
		return def
	}
	return n
}

// getEnvDuration parses key as a time.Duration (e.g. "5s", "10m"). Unset
// or malformed returns def.
func getEnvDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		slog.Warn("invalid duration in environment, using default", "key", key, "value", v)
		return def
	}
	return d
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
			PublicEndpoint:  os.Getenv("S3_PUBLIC_ENDPOINT"),
			AccessKeyID:     os.Getenv("S3_ACCESS_ID"),
			SecretAccessKey: os.Getenv("S3_SECRET"),
			Bucket:          os.Getenv("S3_BUCKET"),
			Region:          os.Getenv("S3_REGION"),
		},
		Indexer: IndexerConfig{
			PollInterval: getEnvDuration("INDEXER_POLL_INTERVAL", 5*time.Second),
			BatchSize:    int32(getEnvInt("INDEXER_BATCH_SIZE", 32)),
			MaxAttempts:  int32(getEnvInt("INDEXER_MAX_ATTEMPTS", 5)),
			ClaimTTL:     getEnvDuration("INDEXER_CLAIM_TTL", 10*time.Minute),
			BackoffBase:  getEnvDuration("INDEXER_BACKOFF_BASE", 10*time.Second),
			BackoffMax:   getEnvDuration("INDEXER_BACKOFF_MAX", 10*time.Minute),
		},
	}
}
