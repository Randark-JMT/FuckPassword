package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	ListenAddr       string
	DSN              string
	UploadDir        string
	StatementTimeout time.Duration
	MaxQueue         int
	TTLDays          int
	IngestBatch      int
	MaxLineBytes     int
}

func Load() Config {
	return Config{
		ListenAddr:       env("LISTEN_ADDR", ":8080"),
		DSN:              env("DSN", "postgres://postgres:postgres@db:5432/fuckpassword?sslmode=disable"),
		UploadDir:        env("UPLOAD_DIR", "/data/uploads"),
		StatementTimeout: durEnv("STATEMENT_TIMEOUT", 60*time.Second),
		MaxQueue:         intEnv("MAX_QUEUE", 20),
		TTLDays:          intEnv("TTL_DAYS", 7),
		IngestBatch:      intEnv("INGEST_BATCH", 50000),
		MaxLineBytes:     intEnv("MAX_LINE_BYTES", 4096),
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func intEnv(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func durEnv(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
