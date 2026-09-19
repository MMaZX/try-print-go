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
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"usqay-print-client/internal/autostart"
	"usqay-print-client/internal/config"
	"usqay-print-client/internal/localapi"
	"usqay-print-client/internal/logging"
	"usqay-print-client/internal/printer"
	"usqay-print-client/internal/queue"
	"usqay-print-client/internal/statuswindow"
	"usqay-print-client/internal/tray"
	"usqay-print-client/internal/ui"
	"usqay-print-client/internal/winconsole"
	"usqay-print-client/internal/ws"
)

const AppVersion = "2026.1.3"

func main() {
	listFlag := flag.Bool("list", false, "listar impresoras disponibles y salir")
	insertFlag := flag.Bool("insert", false, "insertar un trabajo de prueba en la cola y salir")
	headlessFlag := flag.Bool("headless", false, "ejecutar sin bandeja del sistema (Windows); en Linux siempre es headless")
	terminalFlag := flag.Bool("terminal", false, "alias de -headless pensado para testing/depuracion manual en una terminal (logs a stdout, sin tray ni ventanas)")
	installerFlag := flag.Bool("installer", false, "registrar inicio automatico (Task Scheduler / autostart) y salir")
	uninstallFlag := flag.Bool("uninstaller", false, "remover inicio automatico y salir")
	versionFlag := flag.Bool("version", false, "mostrar version y salir")
	flag.Parse()

	// -terminal es -headless con otro nombre, mas claro para quien quiere
	// correr el agente a mano en una terminal y ver los logs en vivo. No hay
	// forma de mostrar la consola de proceso desde el tray (se saco esa
	// opcion: cerrar esa ventana con la X mataba todo el proceso) — este es
	// el reemplazo soportado para ese caso de uso.
	headless := *headlessFlag || *terminalFlag

	if *versionFlag {
		fmt.Printf("usqay-print-client - Version %s\n", AppVersion)
		return
	}

	if *installerFlag {
		if err := autostart.Install(); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR al instalar inicio automatico: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Inicio automatico configurado exitosamente.")
		return
	}

	if *uninstallFlag {
		if err := autostart.Uninstall(); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR al remover inicio automatico: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Inicio automatico removido exitosamente.")
		return
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	// config.json ya existe (o el wizard de primer arranque, que necesita la
	// consola, ya termino): nada mas que escribir/leer por consola de aqui
	// en adelante, asi que en Windows se oculta la ventana de consola para
	// que ejecutar via Task Scheduler no deje un cmd.exe negro visible.
	if !headless {
		winconsole.Hide()
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

	// --- Tray: creado antes de arrancar el worker, que puede notificar un
	// fallo de impresion de inmediato ---
	t := tray.NewTray(headless)

	// --- Worker con callback de notificación al servidor + tray ---
	prnDir := filepath.Join(resolveExeDir(), "captured_prns")
	worker := queue.NewWorker(repo, registry, func(jobID string, estado queue.Estado, errMsg string) {
		conn.Notify(jobID, estado, errMsg)
		if estado == queue.EstadoError {
			t.NotifyPrintError(
				"Usqay Print - Error de Impresion",
				fmt.Sprintf("No se pudo imprimir el trabajo #%s", jobID),
				fmt.Sprintf("Trabajo ID: %s\n\nError:\n%s", jobID, errMsg),
			)
		}
	}, cfg.CapturePRN, prnDir, cfg.MaxRetries)

	// --- Servidor HTTP local (Propuesta 1: modo offline en LAN) ---
	localSrv := localapi.NewServer(cfg, repo)

	go conn.Run(ctx)
	go worker.Run(ctx)
	go localSrv.Run(ctx)

	// --- Ventanas de estado de conexion: solo tiene sentido con el tray
	// grafico real en Windows. En --headless/--terminal no hay ventanas de
	// ningun tipo (uso pensado para testing por consola). ---
	if runtime.GOOS == "windows" && !headless {
		if !waitForInitialConnection(ctx, conn) {
			slog.Error("no se pudo conectar al servidor de impresion tras los intentos iniciales, cerrando")
			cancel()
			time.Sleep(300 * time.Millisecond)
			os.Exit(1)
		}
		showConnectionStatus(cfg, conn)
		go watchReconnects(ctx, conn)
	}

	err = t.Run(tray.Callbacks{
		OnShowStatus: func() { showConnectionStatus(cfg, conn) },
		OnReload:     func() { reloadConfig(cancel) },
		OnToggleAutostart: func() {
			toggleAutostart()
		},
		OnExit: func() {
			slog.Info("apagando...", "src", "TRAY")
			cancel()
		},
	})
	if err != nil {
		slog.Error("error en ejecucion de la bandeja del sistema", "error", err)
	}

	time.Sleep(600 * time.Millisecond) // drain in-flight messages
}

// waitForInitialConnection muestra una ventana "intentando conectar..." no
// cerrable por el usuario mientras el agente hace sus primeros intentos de
// conexion, actualizando el contador en vivo. Devuelve true apenas conecta;
// devuelve false (mostrando antes un dialogo de error) si el tercer intento
// termina y todavia no hay conexion.
//
// Se espera a que Attempts supere maxAttempts, no a que lo alcance, para
// estar seguros de que ese ultimo intento ya termino y fallo — no que sigue
// en curso (ws.Connection.incrementAttempt cuenta intentos que arrancan, no
// que terminan).
func waitForInitialConnection(ctx context.Context, conn *ws.Connection) bool {
	const maxAttempts = 3

	win := statuswindow.Show("Usqay Print Client",
		fmt.Sprintf("Intentando conectar al servidor de impresion...\n\nIntento 1 de %d", maxAttempts))
	defer win.Close()

	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			st := conn.Status()
			if st.Connected {
				return true
			}

			shown := st.Attempts
			if shown < 1 {
				shown = 1
			} else if shown > maxAttempts {
				shown = maxAttempts
			}
			win.SetText(fmt.Sprintf("Intentando conectar al servidor de impresion...\n\nIntento %d de %d", shown, maxAttempts))

			if st.Attempts > maxAttempts {
				win.Close()
				ui.ShowError("Usqay Print Client - Error de Conexion",
					fmt.Sprintf("No se pudo conectar al servidor de impresion tras %d intentos.\n\nServidor: %s\nUltimo error: %s",
						maxAttempts, st.ServerURL, st.LastError))
				return false
			}
		}
	}
}

// watchReconnects vigila caidas de conexion una vez que el agente ya conecto
// exitosamente al menos una vez (a diferencia de waitForInitialConnection,
// que solo cubre el arranque). Mientras dura la caida muestra una ventana
// "reconectando..." no cerrable por el usuario, actualizada con el contador
// de intentos; se cierra sola al reconectar, o junto con el proceso si el
// binario se cierra (ctx.Done).
func watchReconnects(ctx context.Context, conn *ws.Connection) {
	var win *statuswindow.Window

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			win.Close()
			return
		case <-ticker.C:
			st := conn.Status()
			if st.Connected {
				win.Close()
				win = nil
				continue
			}

			text := fmt.Sprintf("Conexion perdida. Intentando reconectar...\n\nIntento %d", st.Attempts)
			if win == nil {
				win = statuswindow.Show("Usqay Print Client", text)
			} else {
				win.SetText(text)
			}
		}
	}
}

// reloadConfig valida el config.json actual y, si es correcto, relanza el
// mismo ejecutable con los mismos argumentos y termina este proceso. No se
// hace hot-swap del *config.Config que ya usa ws.Connection: server_url y
// token requieren reconectar el socket de todas formas, y mutar esos campos
// sin proteccion desde el hilo del tray abriria una carrera de datos con las
// goroutines que ya los leen.
func reloadConfig(cancel context.CancelFunc) {
	if _, err := config.Load(); err != nil {
		ui.ShowError("Usqay Print Client - Error de Configuracion",
			fmt.Sprintf("No se pudo recargar: config.json invalido.\n\n%v", err))
		return
	}

	exe, err := os.Executable()
	if err != nil {
		ui.ShowError("Usqay Print Client", fmt.Sprintf("No se pudo determinar la ruta del ejecutable: %v", err))
		return
	}

	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Dir = resolveExeDir()
	if err := cmd.Start(); err != nil {
		ui.ShowError("Usqay Print Client", fmt.Sprintf("No se pudo reiniciar el proceso: %v", err))
		return
	}

	slog.Info("configuracion valida: reiniciando proceso para aplicarla", "src", "TRAY", "pid_nuevo", cmd.Process.Pid)
	cancel()
	time.Sleep(300 * time.Millisecond)
	os.Exit(0)
}

// showConnectionStatus muestra en un dialogo nativo la informacion de
// conexion actual: servidor, terminal_id (resuelto por el servidor si no
// vino fijo en config.json), estado y ultimos eventos de conexion.
func showConnectionStatus(cfg *config.Config, conn *ws.Connection) {
	st := conn.Status()

	connState := "Desconectado"
	if st.Connected {
		connState = "Conectado"
	}

	terminalID := st.TerminalID
	if terminalID == "" {
		terminalID = "(pendiente de asignar por el servidor)"
	}

	lastConnected := "nunca"
	if !st.LastConnectedAt.IsZero() {
		lastConnected = st.LastConnectedAt.Local().Format("2006-01-02 15:04:05")
	}
	lastDisconnected := "—"
	if !st.LastDisconnectedAt.IsZero() {
		lastDisconnected = st.LastDisconnectedAt.Local().Format("2006-01-02 15:04:05")
	}

	msg := fmt.Sprintf(
		"Estado: %s\n\nServidor: %s\nTerminal ID: %s\nToken: %s\n\nUltima conexion exitosa: %s\nUltima desconexion: %s",
		connState, st.ServerURL, terminalID, maskToken(cfg.Token), lastConnected, lastDisconnected,
	)
	if st.LastError != "" {
		msg += fmt.Sprintf("\n\nUltimo error: %s", st.LastError)
	}

	title := "Usqay Print Client - Informacion de Conexion"
	if st.Connected {
		ui.ShowInfo(title, msg)
	} else {
		ui.ShowWarning(title, msg)
	}
}

// maskToken oculta el token salvo sus primeros/ultimos 4 caracteres, para
// poder confirmar cual esta configurado sin exponerlo entero en pantalla.
func maskToken(token string) string {
	if len(token) <= 8 {
		return strings.Repeat("*", len(token))
	}
	return token[:4] + strings.Repeat("*", len(token)-8) + token[len(token)-4:]
}

func toggleAutostart() {
	if !autostart.IsElevated() {
		ui.ShowWarning("Usqay Print Client - Permisos Insuficientes",
			"\"Iniciar con el sistema\" necesita privilegios de administrador (registra/elimina una tarea programada de Windows).\n\n"+
				"Cierre el agente y vuelva a abrirlo con \"Ejecutar como administrador\" (click derecho sobre el .exe) antes de usar esta opcion.")
		return
	}

	enabled, err := autostart.IsEnabled()
	if err != nil {
		slog.Warn("no se pudo verificar estado de autostart", "error", err)
	}

	if enabled {
		if err := autostart.Uninstall(); err != nil {
			ui.ShowError("Usqay Print Client", fmt.Sprintf("Error al desinstalar inicio automatico: %v", err))
		} else {
			ui.ShowInfo("Usqay Print Client", "Inicio automatico con el sistema desactivado.")
		}
		return
	}

	if err := autostart.Install(); err != nil {
		ui.ShowError("Usqay Print Client", fmt.Sprintf("Error al instalar inicio automatico: %v", err))
	} else {
		ui.ShowInfo("Usqay Print Client", "Inicio automatico con el sistema activado correctamente.")
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
