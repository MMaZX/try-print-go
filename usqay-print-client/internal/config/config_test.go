package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"usqay-print-client/internal/config"
)

func TestConfig_MaxRetries(t *testing.T) {
	t.Run("DefaultMaxRetries asignado cuando max_retries no está presente", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Chdir(tmpDir)

		raw := `{"server_url":"ws://localhost:8080/ws","terminal_id":"caja-1","token":"abc"}`
		if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte(raw), 0644); err != nil {
			t.Fatalf("escribir config.json temporal: %v", err)
		}

		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("config.Load falló: %v", err)
		}
		if cfg.MaxRetries != config.DefaultMaxRetries {
			t.Errorf("MaxRetries = %d, want %d (DefaultMaxRetries)", cfg.MaxRetries, config.DefaultMaxRetries)
		}
	})

	t.Run("MaxRetries respeta valor personalizado en JSON", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Chdir(tmpDir)

		raw := `{"server_url":"ws://localhost:8080/ws","terminal_id":"caja-1","token":"abc","max_retries":5}`
		if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte(raw), 0644); err != nil {
			t.Fatalf("escribir config.json temporal: %v", err)
		}

		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("config.Load falló: %v", err)
		}
		if cfg.MaxRetries != 5 {
			t.Errorf("MaxRetries = %d, want 5", cfg.MaxRetries)
		}
	})
}
