package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config captures all runtime configuration for the application.
type Config struct {
	Address  string
	DataDir  string
	Ollama   OllamaConfig
	Embed    EmbeddingConfig
	Database DatabaseConfig
}

// OllamaConfig groups the settings required to talk to an Ollama server.
type OllamaConfig struct {
	Host  string
	Model string
}

// EmbeddingConfig describes the embedding provider settings.
type EmbeddingConfig struct {
	Model     string
	Dimension int
}

// DatabaseConfig captures the vector database connection string and limits.
type DatabaseConfig struct {
	URL            string
	MaxConnections int
	SearchTopK     int
}

// FromEnv builds a Config by reading environment variables and applying
// sensible defaults. The resulting configuration is validated before it is
// returned.
func FromEnv() (Config, error) {
	cfg := Config{
		Address: getEnv("SERVER_ADDR", "127.0.0.1:8080"),
		DataDir: getEnv("DATA_DIR", "./data"),
		Ollama: OllamaConfig{
			Host:  getEnv("OLLAMA_HOST", "http://localhost:11434"),
			Model: getEnv("OLLAMA_MODEL", "llama3.1:8b"),
		},
		Embed: EmbeddingConfig{
			Model:     getEnv("EMBEDDING_MODEL", "nomic-embed-text"),
			Dimension: getEnvInt("EMBEDDING_DIMENSION", 768),
		},
		Database: DatabaseConfig{
			URL:            getEnv("DATABASE_URL", "postgres://airplane:airplane@localhost:5433/airplane_chat?sslmode=disable"),
			MaxConnections: getEnvInt("DATABASE_MAX_CONNECTIONS", 4),
			SearchTopK:     getEnvInt("RETRIEVAL_TOP_K", 6),
		},
	}

	cfg.Ollama.Host = strings.TrimRight(cfg.Ollama.Host, "/")

	if !filepath.IsAbs(cfg.DataDir) {
		abs, err := filepath.Abs(cfg.DataDir)
		if err != nil {
			return Config{}, fmt.Errorf("resolve data dir: %w", err)
		}
		cfg.DataDir = abs
	}

	if cfg.Ollama.Model == "" {
		return Config{}, fmt.Errorf("OLLAMA_MODEL must not be empty")
	}

	if cfg.Embed.Model == "" {
		return Config{}, fmt.Errorf("EMBEDDING_MODEL must not be empty")
	}

	if cfg.Embed.Dimension <= 0 {
		return Config{}, fmt.Errorf("EMBEDDING_DIMENSION must be positive")
	}

	if cfg.Database.URL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL must not be empty")
	}

	if cfg.Database.SearchTopK <= 0 {
		cfg.Database.SearchTopK = 6
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return fallback
}
