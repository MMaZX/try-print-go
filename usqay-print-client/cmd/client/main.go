package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
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
	if err := cfg.Validate(); err != nil {
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

	setupLogger(cfg.LogLevel, os.Stdout, dailyWriter)

	if result, err := logging.ArchivePreviousMonthResult(logDir); err != nil {
		slog.Warn("no se pudo archivar logs del mes anterior", "error", err)
	} else if result != nil {
		slog.Info("logs archivados", "zip", result.ZipPath, "archivos", result.Archived)
	}

	slog.Info("usqay-print-client iniciado")

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

	// --- Registro de impresoras (se puebla al recibir TypeConfig del servidor) ---
	registry := printer.NewRegistry()

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

	// --- Insertar trabajo de prueba ---
	if *insertFlag {
		job := buildTestJob()
		if err := repo.Insert(job); err != nil {
			slog.Error("error al insertar trabajo de prueba", "error", err)
			os.Exit(1)
		}
		slog.Info("trabajo de prueba insertado", "job_id", job.ID, "tipo", job.TipoDocumento)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- Conexión WebSocket ---
	conn := ws.NewConnection(cfg, repo, registry)

	// --- Hidratación offline de configuración ---
	if err := conn.LoadCachedConfig(); err != nil {
		if errors.Is(err, queue.ErrCachedConfigForeignTerminal) {
			slog.Error("configuración cacheada pertenece a otra terminal, descartada — el agente no podrá resolver impresoras hasta reconectar con el servidor")
		} else if errors.Is(err, queue.ErrNoConfig) {
			slog.Error("no hay configuración cacheada disponible — el agente no podrá resolver impresoras hasta reconectar con el servidor")
		} else {
			slog.Error("error cargando configuración cacheada desde SQLite", "error", err)
		}
	}

	// --- Worker con callback de notificación al servidor ---
	prnDir := filepath.Join(resolveExeDir(), "captured_prns")
	worker := queue.NewWorker(repo, registry, func(jobID string, estado queue.Estado, errMsg string) {
		conn.Notify(jobID, estado, errMsg)
	}, cfg.CapturePRN, prnDir)

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

func setupLogger(level string, console io.Writer, fileW io.Writer) {
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
	consoleH := logging.NewConsoleHandler(console, l)
	fileH := slog.NewTextHandler(fileW, &slog.HandlerOptions{Level: l})
	slog.SetDefault(slog.New(logging.NewMultiHandler(consoleH, fileH)))
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
