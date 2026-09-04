package config_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
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

func TestConfig_FirstTimeSetup(t *testing.T) {
	t.Run("crea config.json interactivamente cuando no existe", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Chdir(tmpDir)

		cfg := runLoadWithStdin(t, "wss://app.usqay.com/ws\ncaja-01\ntoken-secreto\n")

		if cfg.ServerURL != "wss://app.usqay.com/ws" {
			t.Errorf("ServerURL = %q, want %q", cfg.ServerURL, "wss://app.usqay.com/ws")
		}
		if cfg.TerminalID != "caja-01" {
			t.Errorf("TerminalID = %q, want %q", cfg.TerminalID, "caja-01")
		}
		if cfg.Token != "token-secreto" {
			t.Errorf("Token = %q, want %q", cfg.Token, "token-secreto")
		}
		if cfg.LogLevel != "info" {
			t.Errorf("LogLevel = %q, want %q (default)", cfg.LogLevel, "info")
		}

		raw, err := os.ReadFile(filepath.Join(tmpDir, "config.json"))
		if err != nil {
			t.Fatalf("config.json no fue escrito en disco: %v", err)
		}
		var onDisk map[string]any
		if err := json.Unmarshal(raw, &onDisk); err != nil {
			t.Fatalf("config.json escrito no es JSON válido: %v", err)
		}
		if onDisk["server_url"] != "wss://app.usqay.com/ws" {
			t.Errorf("config.json en disco: server_url = %v, want %q", onDisk["server_url"], "wss://app.usqay.com/ws")
		}
	})

	t.Run("terminal_id es opcional: Enter vacío no bloquea el wizard", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Chdir(tmpDir)

		cfg := runLoadWithStdin(t, "wss://app.usqay.com/ws\n\ntoken-secreto\n")

		if cfg.TerminalID != "" {
			t.Errorf("TerminalID = %q, want \"\" (omitido)", cfg.TerminalID)
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() falló con terminal_id vacío: %v", err)
		}
	})

	t.Run("no vuelve a preguntar si config.json ya existe", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Chdir(tmpDir)

		raw := `{"server_url":"ws://localhost:8080/ws","terminal_id":"caja-1","token":"abc"}`
		if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte(raw), 0644); err != nil {
			t.Fatalf("escribir config.json temporal: %v", err)
		}

		// Stdin vacío: si Load() intentara preguntar, se colgaría leyendo EOF
		// inmediato y fallaría con error, en vez de leer el JSON existente.
		cfg := runLoadWithStdin(t, "")

		if cfg.ServerURL != "ws://localhost:8080/ws" {
			t.Errorf("ServerURL = %q, want %q (leído de config.json existente, no del wizard)", cfg.ServerURL, "ws://localhost:8080/ws")
		}
	})
}

// runLoadWithStdin redirige os.Stdin/os.Stdout, llama a config.Load() y
// restaura ambos antes de devolver el resultado.
func runLoadWithStdin(t *testing.T, stdin string) *config.Config {
	t.Helper()

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatalf("crear pipe de stdin: %v", err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("crear pipe de stdout: %v", err)
	}

	origStdin, origStdout := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = inR, outW
	t.Cleanup(func() {
		os.Stdin, os.Stdout = origStdin, origStdout
	})

	go func() {
		io.WriteString(inW, stdin)
		inW.Close()
	}()

	var outBuf bytes.Buffer
	done := make(chan struct{})
	go func() {
		io.Copy(&outBuf, outR)
		close(done)
	}()

	cfg, err := config.Load()

	os.Stdout = origStdout
	outW.Close()
	<-done

	if err != nil {
		t.Fatalf("config.Load falló: %v (stdout capturado: %s)", err, strings.TrimSpace(outBuf.String()))
	}
	return cfg
}

func TestConfig_LocalAPIPort(t *testing.T) {
	t.Run("DefaultLocalAPIPort asignado cuando local_api_port no está presente", func(t *testing.T) {
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
		if cfg.LocalAPIPort != config.DefaultLocalAPIPort {
			t.Errorf("LocalAPIPort = %d, want %d (DefaultLocalAPIPort)", cfg.LocalAPIPort, config.DefaultLocalAPIPort)
		}
	})

	t.Run("LocalAPIPort respeta valor personalizado en JSON", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Chdir(tmpDir)

		raw := `{"server_url":"ws://localhost:8080/ws","terminal_id":"caja-1","token":"abc","local_api_port":9200}`
		if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte(raw), 0644); err != nil {
			t.Fatalf("escribir config.json temporal: %v", err)
		}

		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("config.Load falló: %v", err)
		}
		if cfg.LocalAPIPort != 9200 {
			t.Errorf("LocalAPIPort = %d, want 9200", cfg.LocalAPIPort)
		}
	})
}
