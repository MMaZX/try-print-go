package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"usqay-print-client/internal/config"
	"usqay-print-client/internal/logging"
	"usqay-print-client/internal/printer"
	"usqay-print-client/internal/queue"
	"usqay-print-client/internal/ws"
)

func main() {
	listFlag   := flag.Bool("list", false, "listar impresoras disponibles y salir")
	insertFlag := flag.Bool("insert", false, "insertar un trabajo de prueba en la cola y salir")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	// --- Logging (archivo diario + stdout) ---
	logDir := filepath.Join(resolveExeDir(), "logs")
	dailyWriter, err := logging.NewDailyWriter(logDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR al inicializar logs: %v\n", err)
		os.Exit(1)
	}
	defer dailyWriter.Close()

	setupLogger(cfg.LogLevel, io.MultiWriter(os.Stdout, dailyWriter))

	if result, err := logging.ArchivePreviousMonthResult(logDir); err != nil {
		slog.Warn("no se pudo archivar logs del mes anterior", "error", err)
	} else if result != nil {
		slog.Info("logs archivados", "zip", result.ZipPath, "archivos", result.Archived)
	}

	slog.Info("usqay-print-client iniciado", "terminal_id", cfg.TerminalID)

	// --- Listar impresoras y salir ---
	if *listFlag {
		names, err := printer.ListPrinters()
		if err != nil {
			slog.Error("error al listar impresoras", "error", err)
			os.Exit(1)
		}
		if len(names) == 0 {
			fmt.Println("No se encontraron impresoras configuradas.")
			return
		}
		fmt.Println("Impresoras disponibles:")
		for _, n := range names {
			fmt.Printf("  - %s\n", n)
		}
		return
	}

	// --- Impresora ---
	p, err := buildPrinter(cfg)
	if err != nil {
		slog.Error("error al crear impresora", "error", err)
		os.Exit(1)
	}

	// --- SQLite ---
	dbPath := filepath.Join(resolveExeDir(), "agent.db")
	db, err := queue.Open(dbPath)
	if err != nil {
		slog.Error("error al abrir base de datos", "path", dbPath, "error", err)
		os.Exit(1)
	}
	defer db.Close()
	slog.Info("base de datos abierta", "path", dbPath)

	repo := queue.NewRepository(db)

	// --- Insertar trabajo de prueba (modo Etapa 2) ---
	if *insertFlag {
		job := buildTestJob()
		if err := repo.Insert(job); err != nil {
			slog.Error("error al insertar trabajo de prueba", "error", err)
			os.Exit(1)
		}
		slog.Info("trabajo de prueba insertado", "job_id", job.ID, "tipo", job.TipoDocumento)
		return
	}

	// --- Validar config para WebSocket ---
	if err := cfg.ValidateWebSocket(); err != nil {
		slog.Error("configuración incompleta para WebSocket", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- Conexión WebSocket ---
	conn := ws.NewConnection(cfg, repo)

	// --- Worker con callback de notificación al servidor ---
	worker := queue.NewWorker(repo, p, func(jobID string, estado queue.Estado, errMsg string) {
		conn.Notify(jobID, estado, errMsg)
	})

	go conn.Run(ctx)
	go worker.Run(ctx)

	// --- Esperar señal de apagado (Ctrl+C o SIGTERM) ---
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	slog.Info("apagando...")
	cancel()
	time.Sleep(600 * time.Millisecond) // drain in-flight messages
}

// buildPrinter creates the Printer from config.json.
func buildPrinter(cfg *config.Config) (printer.Printer, error) {
	switch printer.Type(cfg.PrinterType) {
	case printer.TypeNetwork:
		if cfg.PrinterAddr == "" {
			return nil, fmt.Errorf("printer_addr es requerido para tipo \"network\"")
		}
		return printer.NewNetworkPrinter(cfg.PrinterAddr), nil
	case printer.TypeSystem:
		if cfg.PrinterName == "" {
			return nil, fmt.Errorf("printer_name es requerido para tipo \"system\"")
		}
		return printer.NewSystemPrinter(cfg.PrinterName), nil
	default:
		return nil, fmt.Errorf("printer_type desconocido %q — usa \"network\" o \"system\"", cfg.PrinterType)
	}
}

// buildTestJob builds a sample comanda job for -insert flag testing.
func buildTestJob() queue.PrintJob {
	now := time.Now().UTC()
	return queue.PrintJob{
		ID:            newID(),
		TipoDocumento: "comanda",
		Payload: `{
			"mesa": 5,
			"items": [
				{"nombre": "Lomo Saltado",    "cantidad": 2, "precio": 21.00},
				{"nombre": "Inca Kola 500ml", "cantidad": 2, "precio": 4.00},
				{"nombre": "Arroz con Leche", "cantidad": 1, "precio": 7.00}
			]
		}`,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func setupLogger(level string, w io.Writer) {
	var l slog.Level
	switch level {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: l})))
}

// resolveExeDir returns the directory of the running executable,
// falling back to cwd when invoked via `go run`.
func resolveExeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	dir := filepath.Dir(exe)
	if filepath.Base(dir) == "go-build" || filepath.Base(filepath.Dir(dir)) == "go-build" {
		return "."
	}
	return dir
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
