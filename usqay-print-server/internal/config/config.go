package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config holds all runtime configuration for the print server.
type Config struct {
	Port           int    `json:"port"`
	LogLevel       string `json:"log_level"`
	LaravelBaseURL string `json:"laravel_base_url"` // Laravel base URL, e.g. http://localhost:8000 — obligatorio, sin fallback local
	InternalToken  string `json:"internal_token"`   // Shared secret token
}

// Load reads config.json from the executable directory, falling back to cwd.
func Load() (*Config, error) {
	path, err := resolveConfigPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf(
			"config.json no encontrado en %s — créalo con los campos requeridos (port, laravel_base_url, internal_token)",
			path,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("leer config.json: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsear config.json: %w", err)
	}
	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	// Override/set from environment variables if present
	if envToken := os.Getenv("INTERNAL_TOKEN"); envToken != "" {
		cfg.InternalToken = envToken
	}
	if envLaravel := os.Getenv("LARAVEL_BASE_URL"); envLaravel != "" {
		cfg.LaravelBaseURL = envLaravel
	}
	return &cfg, nil
}

// Validate checks that config fields required for the server to authenticate
// agents are present. Sin laravel_base_url el servidor no tiene ninguna forma
// de validar tokens — no existe modo degradado ni fallback local.
func (c *Config) Validate() error {
	if c.LaravelBaseURL == "" {
		return fmt.Errorf("laravel_base_url es requerido en config.json — el servidor no puede validar agentes sin él")
	}
	return nil
}

func resolveConfigPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolver ruta del ejecutable: %w", err)
	}
	candidate := filepath.Join(filepath.Dir(exe), "config.json")
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	return "config.json", nil
}
