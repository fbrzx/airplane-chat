package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config captures all runtime configuration for the application.
type Config struct {
	Address string
	DataDir string
	Ollama  OllamaConfig
}

// OllamaConfig groups the settings required to talk to an Ollama server.
type OllamaConfig struct {
	Host  string
	Model string
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

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}
