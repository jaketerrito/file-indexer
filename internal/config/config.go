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

// WorkerConfig tunes the indexer's claim/process loop. Zero values fall
// back to the indexer package defaults, so only the knobs that matter need
// to be set.
type WorkerConfig struct {
	// PollInterval is the sleep between polls when the queue is empty.
	PollInterval time.Duration
	// BatchSize is the maximum number of jobs claimed per poll.
	BatchSize int
	// MaxAttempts is the number of tries before a job is parked as an error.
	MaxAttempts int
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
	Worker   WorkerConfig
}

func getEnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// getEnvInt parses key as an int. Unset returns 0 (caller-side default);
// malformed values are logged and treated as unset rather than aborting.
func getEnvInt(key string) int {
	v := os.Getenv(key)
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		slog.Warn("invalid int in environment, using default", "key", key, "value", v)
		return 0
	}
	return n
}

// getEnvDuration parses key as a time.Duration (e.g. "5s", "10m"). Unset
// returns 0 (caller-side default); malformed values are logged and treated
// as unset rather than aborting.
func getEnvDuration(key string) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		slog.Warn("invalid duration in environment, using default", "key", key, "value", v)
		return 0
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
		Worker: WorkerConfig{
			PollInterval: getEnvDuration("WORKER_POLL_INTERVAL"),
			BatchSize:    getEnvInt("WORKER_BATCH_SIZE"),
			MaxAttempts:  getEnvInt("WORKER_MAX_ATTEMPTS"),
			ClaimTTL:     getEnvDuration("WORKER_CLAIM_TTL"),
			BackoffBase:  getEnvDuration("WORKER_BACKOFF_BASE"),
			BackoffMax:   getEnvDuration("WORKER_BACKOFF_MAX"),
		},
	}
}
