package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
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

// PreviewConfig tunes the preview index type's image processing. Every limit
// is a guard against untrusted input: the preview worker decodes whatever
// bytes object storage hands it.
type PreviewConfig struct {
	// MaxDim is the longest edge of a generated preview, in pixels. Sources
	// are scaled to fit inside a MaxDim x MaxDim box with aspect ratio
	// preserved; smaller sources are never upscaled and previews are never
	// padded, so a wide image yields a wide preview.
	MaxDim int
	// Quality is the JPEG encoder quality (1-100) used for opaque sources.
	Quality int
	// MaxSourceBytes is the largest source object the worker will download.
	MaxSourceBytes int64
	// MaxSourcePixels caps width*height of the source image. A decoded image
	// costs roughly 4 bytes per pixel, so this is what bounds peak memory.
	MaxSourcePixels int64
}

// ExifConfig tunes the exif index type's metadata extraction. Both limits
// bound memory and network use against untrusted object content, mirroring
// PreviewConfig's rationale.
type ExifConfig struct {
	// MaxHeaderBytes bounds the first read attempt. EXIF/XMP metadata lives
	// near the start of every modern format (JPEG, HEIC, AVIF, and every
	// TIFF-based RAW), so this only needs to be generously larger than that,
	// not sized to any one format exactly.
	MaxHeaderBytes int64
	// MaxSourceBytes bounds the fallback full read, used only when the
	// header read found no metadata and had more data beyond it (legacy
	// formats like Canon CRW keep their directory at end-of-file).
	MaxSourceBytes int64
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
	// IndexPrefix is the key prefix under which index types write derived
	// objects (previews, etc.) into the same bucket as the source files. The
	// crawler skips keys beneath it so derived objects never become files
	// rows. Always normalized to end in "/"; matches at bucket root only, so
	// a nested "foo/.index/" is not covered.
	IndexPrefix string
	Database    DatabaseConfig
	S3          S3Config
	Indexer     IndexerConfig
	Preview     PreviewConfig
	Exif        ExifConfig
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

// getEnvInt64 parses key as an int64. Unset or malformed returns def.
func getEnvInt64(key string, def int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		slog.Warn("invalid int64 in environment, using default", "key", key, "value", v)
		return def
	}
	return n
}

// normalizeIndexPrefix trims surrounding slashes and re-adds exactly one
// trailing slash, so strings.HasPrefix matching is unambiguous (".index"
// and "/.index/" both become ".index/"). A slash-only or empty value
// disables prefix skipping entirely.
func normalizeIndexPrefix(p string) string {
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	return p + "/"
}

func Load() *Config {
	return &Config{
		GrpcAddr:    getEnvDefault("GRPC_ADDR", ":50051"),
		IndexPrefix: normalizeIndexPrefix(getEnvDefault("INDEX_PREFIX", ".index/")),
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
		Preview: PreviewConfig{
			MaxDim:          getEnvInt("PREVIEW_MAX_DIM", 320),
			Quality:         getEnvInt("PREVIEW_QUALITY", 80),
			MaxSourceBytes:  getEnvInt64("PREVIEW_MAX_SOURCE_BYTES", 64<<20),
			MaxSourcePixels: getEnvInt64("PREVIEW_MAX_SOURCE_PIXELS", 40_000_000),
		},
		Exif: ExifConfig{
			MaxHeaderBytes: getEnvInt64("EXIF_MAX_HEADER_BYTES", 1<<20),
			MaxSourceBytes: getEnvInt64("EXIF_MAX_SOURCE_BYTES", 64<<20),
		},
	}
}
