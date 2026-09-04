package ws

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"usqay-print-client/internal/config"
	"usqay-print-client/internal/printer"
	"usqay-print-client/internal/queue"
)

func setupTestConnection(t *testing.T, terminalID string) (*Connection, *queue.Repository, *printer.Registry) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := queue.Open(dbPath)
	if err != nil {
		t.Fatalf("error abriendo DB de test: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
	})

	repo := queue.NewRepository(db)
	registry := printer.NewRegistry()
	cfg := &config.Config{
		ServerURL:  "ws://localhost:8080/ws",
		TerminalID: terminalID,
		Token:      "test-token",
		LogLevel:   "debug",
	}
	conn := NewConnection(cfg, repo, registry)
	return conn, repo, registry
}

func TestConnection_LoadCachedConfig(t *testing.T) {
	t.Run("DB vacía retorna ErrNoConfig sin panic", func(t *testing.T) {
		conn, _, reg := setupTestConnection(t, "caja-01")

		err := conn.LoadCachedConfig()
		if err == nil {
			t.Fatal("se esperaba error, got nil")
		}
		if !errors.Is(err, queue.ErrNoConfig) {
			t.Errorf("esperaba errors.Is(err, queue.ErrNoConfig), got %v", err)
		}
		if reg.Len() != 0 {
			t.Errorf("registry debería estar vacío, len = %d", reg.Len())
		}
	})

	t.Run("Config cacheada válida hidrata registry y terminalID", func(t *testing.T) {
		conn, repo, reg := setupTestConnection(t, "caja-principal")

		printers := []PrinterSpec{
			{
				ID:   "p-red",
				Tipo: "RED",
				Addr: "192.168.1.50:9100",
				Mode: "escpos",
			},
			{
				ID:   "p-usb",
				Tipo: "USB",
				Addr: "POS-58",
				Mode: "text",
			},
		}
		printersJSON, err := json.Marshal(printers)
		if err != nil {
			t.Fatalf("error serializando printers: %v", err)
		}

		if err := repo.SaveConfig("caja-principal", printersJSON); err != nil {
			t.Fatalf("SaveConfig falló: %v", err)
		}

		if err := conn.LoadCachedConfig(); err != nil {
			t.Fatalf("LoadCachedConfig falló: %v", err)
		}

		if conn.terminalID != "caja-principal" {
			t.Errorf("terminalID = %q, want 'caja-principal'", conn.terminalID)
		}

		if reg.Len() != 2 {
			t.Errorf("registry len = %d, want 2", reg.Len())
		}

		pRed, ok := reg.Resolve("p-red")
		if !ok || pRed == nil {
			t.Fatal("p-red no fue resuelto")
		}
		if pRed.Mode() != "escpos" {
			t.Errorf("p-red mode = %v, want escpos", pRed.Mode())
		}

		pUSB, ok := reg.Resolve("p-usb")
		if !ok || pUSB == nil {
			t.Fatal("p-usb no fue resuelto")
		}
		if pUSB.Mode() != "text" {
			t.Errorf("p-usb mode = %v, want text", pUSB.Mode())
		}
	})

	t.Run("JSON corrupto en agent_config retorna error envuelto", func(t *testing.T) {
		conn, repo, _ := setupTestConnection(t, "caja-01")

		if err := repo.SaveConfig("caja-01", []byte("invalid-json")); err != nil {
			t.Fatalf("SaveConfig falló: %v", err)
		}

		err := conn.LoadCachedConfig()
		if err == nil {
			t.Fatal("se esperaba error con JSON corrupto, got nil")
		}
		if errors.Is(err, queue.ErrNoConfig) {
			t.Error("error no debería ser ErrNoConfig con JSON corrupto")
		}
	})

	t.Run("Caché de otra terminal es descartada (ErrCachedConfigForeignTerminal)", func(t *testing.T) {
		conn, repo, _ := setupTestConnection(t, "caja-principal")

		printersJSON := []byte(`[{"id":"p1","tipo":"RED","addr":"192.168.1.50:9100","mode":"escpos"}]`)
		if err := repo.SaveConfig("otra-terminal", printersJSON); err != nil {
			t.Fatalf("SaveConfig falló: %v", err)
		}

		err := conn.LoadCachedConfig()
		if err == nil {
			t.Fatal("se esperaba error por terminal distinta, got nil")
		}
		if !errors.Is(err, queue.ErrCachedConfigForeignTerminal) {
			t.Errorf("esperaba errors.Is(err, queue.ErrCachedConfigForeignTerminal), got %v", err)
		}
	})

	t.Run("terminal_id vacío en config.json acepta la caché de cualquier terminal", func(t *testing.T) {
		conn, repo, reg := setupTestConnection(t, "")

		printersJSON := []byte(`[{"id":"p1","tipo":"RED","addr":"192.168.1.50:9100","mode":"escpos"}]`)
		if err := repo.SaveConfig("caja-lo-que-sea", printersJSON); err != nil {
			t.Fatalf("SaveConfig falló: %v", err)
		}

		if err := conn.LoadCachedConfig(); err != nil {
			t.Fatalf("LoadCachedConfig falló con terminal_id local vacío: %v", err)
		}
		if conn.terminalID != "caja-lo-que-sea" {
			t.Errorf("terminalID = %q, want %q (adoptado de la caché)", conn.terminalID, "caja-lo-que-sea")
		}
		if reg.Len() != 1 {
			t.Errorf("registry len = %d, want 1", reg.Len())
		}
	})
}

func TestConnection_ApplyConfig_PersistsToSQLite(t *testing.T) {
	conn, repo, reg := setupTestConnection(t, "caja-bar")

	msg := ConfigMsg{
		Type:       TypeConfig,
		TerminalID: "caja-bar",
		Printers: []PrinterSpec{
			{
				ID:   "impr-barra",
				Tipo: "RED",
				Addr: "192.168.1.200:9100",
				Mode: "escpos",
				Profile: &printer.DeviceProfile{
					WidthDots:         576,
					DPI:               203,
					CharWidthDots:     12,
					SupportsCut:       true,
					SupportsDrawer:    false,
					SupportsQRNative:  true,
					SupportsPrintArea: true,
					SupportsRaster:    true,
				},
			},
		},
	}

	conn.applyConfig(msg)

	if reg.Len() != 1 {
		t.Fatalf("registry len = %d, want 1", reg.Len())
	}
	if conn.terminalID != "caja-bar" {
		t.Errorf("terminalID = %q, want 'caja-bar'", conn.terminalID)
	}

	// Verificar que se guardó en SQLite agent_config
	tID, pJSON, err := repo.LoadConfig()
	if err != nil {
		t.Fatalf("repo.LoadConfig falló: %v", err)
	}
	if tID != "caja-bar" {
		t.Errorf("repo tID = %q, want 'caja-bar'", tID)
	}

	var savedSpecs []PrinterSpec
	if err := json.Unmarshal(pJSON, &savedSpecs); err != nil {
		t.Fatalf("error deserializando saved JSON: %v", err)
	}
	if len(savedSpecs) != 1 || savedSpecs[0].ID != "impr-barra" {
		t.Errorf("savedSpecs = %+v, want 1 printer with id impr-barra", savedSpecs)
	}

	// Verificar que el perfil de la impresora también se guardó
	prof, err := repo.GetProfile("impr-barra")
	if err != nil {
		t.Fatalf("repo.GetProfile falló: %v", err)
	}
	if prof == nil || prof.WidthDots != 576 {
		t.Errorf("prof = %+v, want WidthDots=576", prof)
	}

	// Verificar que una nueva conexión puede hidratarse desde la caché guardada
	freshRegistry := printer.NewRegistry()
	freshConn := NewConnection(conn.cfg, repo, freshRegistry)
	if err := freshConn.LoadCachedConfig(); err != nil {
		t.Fatalf("freshConn.LoadCachedConfig falló: %v", err)
	}
	if freshRegistry.Len() != 1 {
		t.Errorf("freshRegistry len = %d, want 1", freshRegistry.Len())
	}
	if freshConn.terminalID != "caja-bar" {
		t.Errorf("freshConn.terminalID = %q, want 'caja-bar'", freshConn.terminalID)
	}
}
