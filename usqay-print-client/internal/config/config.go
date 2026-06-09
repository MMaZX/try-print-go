package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config holds all runtime configuration loaded from config.json.
type Config struct {
	// Etapa 3 — WebSocket connection
	ServerURL  string `json:"server_url"`
	TerminalID string `json:"terminal_id"`
	Token      string `json:"token"`

	// Shared
	LogLevel string `json:"log_level"`

	// Etapa 1 — printer test
	PrinterType string `json:"printer_type"` // "network" or "system"
	PrinterAddr string `json:"printer_addr"` // network: "192.168.1.100:9100"
	PrinterName string `json:"printer_name"` // system: OS printer name
}

// Load reads config.json from the directory containing the executable.
// Falls back to the current working directory during development (go run).
func Load() (*Config, error) {
	path, err := resolveConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf(
			"config.json no encontrado en %s — créalo con los campos requeridos (server_url, terminal_id, token)",
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

	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	return &cfg, nil
}

// ValidateWebSocket checks that the fields required for WebSocket connectivity are present.
func (c *Config) ValidateWebSocket() error {
	if c.ServerURL == "" {
		return fmt.Errorf("server_url es requerido en config.json")
	}
	if c.TerminalID == "" {
		return fmt.Errorf("terminal_id es requerido en config.json")
	}
	if c.Token == "" {
		return fmt.Errorf("token es requerido en config.json")
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
	// Development fallback: current working directory.
	return "config.json", nil
}
