package main

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"usqay-print-server/compatibility"

	_ "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"golang.org/x/net/html"
)

// ============================================================================
// CONSTANTES Y TIPOS
// ============================================================================

const (
	VERSION        = "0.1.1"
	nssmURL        = "https://nssm.cc/release/nssm-2.24.zip"
	serviceName    = "UsqayPrintService"
	serviceDisplay = "Usqay - Servicio de Impresión"
	serviceDesc    = "Servicio que gestiona las impresiones enviadas desde los sistemas Usqay."
)

type Win32_Printer struct {
	Name string
}

type Config struct {
	DBServer   string `json:"db_server"`
	DBDatabase string `json:"db_database"`
	DBUser     string `json:"db_user"`
	DBPassword string `json:"db_password"`
	URLBase    string `json:"url_base"`
	Agrupar    bool   `json:"agrupar"`
	SaveDebug  bool   `json:"save_debug"`
	APIPort    int    `json:"api_port"`
}

type PrintJob struct {
	ID       int            `json:"id"`
	Codigo   string         `json:"codigo"`
	Terminal string         `json:"terminal"`
	Aux      sql.NullString `json:"aux"`
	Tipo     string         `json:"tipo"`
}

type PrinterConfig struct {
	Impresora string  `json:"impresora"`
	Ratio     float64 `json:"ratio"`
}

type ServiceStatus struct {
	Running       bool      `json:"running"`
	StartTime     time.Time `json:"start_time"`
	JobsProcessed int       `json:"jobs_processed"`
	LastJob       time.Time `json:"last_job"`
	Version       string    `json:"version"`
	OS            string    `json:"os"`
	Errors        int       `json:"errors"`
}

type DailyLogger struct {
	logPath      string
	currentDate  string
	accessFile   *os.File
	errorFile    *os.File
	debugFile    *os.File
	accessLogger *log.Logger
	errorLogger  *log.Logger
	debugLogger  *log.Logger
}

// ============================================================================
// VARIABLES GLOBALES
// ============================================================================

var (
	config        Config
	db            *sql.DB
	logger        *DailyLogger
	configPath    = getConfigPath()
	logPath       = getLogPath()
	serviceStatus ServiceStatus
	jobsProcessed = 0
	errorsCount   = 0

	// Flags
	refreshFlag   = flag.Bool("r", false, "Actualizar impresoras en la BD")
	clearFlag     = flag.Bool("clear", false, "Limpiar logs antiguos (30+ días)")
	installFlag   = flag.String("install", "", "Instalar servicio (windows|linux|macos)")
	uninstallFlag = flag.String("uninstall", "", "Desinstalar servicio (windows|linux|macos)")
	startFlag     = flag.String("start", "", "Iniciar servicio (windows|linux|macos)")
	stopFlag      = flag.String("stop", "", "Detener servicio (windows|linux|macos)")
	statusFlag    = flag.String("status", "", "Ver estado del servicio (windows|linux|macos)")
	restartFlag   = flag.String("restart", "", "Reiniciar servicio (windows|linux|macos)")
	versionFlag   = flag.Bool("version", false, "Mostrar versión")
	apiFlag       = flag.Bool("api", false, "Habilitar API REST")
	helpFlag      = flag.Bool("help", false, "Mostrar ayuda")
)

// ============================================================================
// FUNCIONES DE RUTAS
// ============================================================================

func getConfigPath() string {
	homeDir, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		return filepath.Join("C:", "usqay", "cnfg", "config.json")
	}
	return filepath.Join(homeDir, ".usqay", "config.json")
}

func getLogPath() string {
	homeDir, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		return filepath.Join("C:", "usqay", "logs")
	}
	return filepath.Join(homeDir, ".usqay", "logs")
}

func getExecutablePath() (string, error) {
	ex, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Abs(ex)
}

func getNssmPath() (string, error) {
	exePath, err := getExecutablePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exePath), "nssm.exe"), nil
}

// ============================================================================
// SISTEMA DE LOGGING DIARIO
// ============================================================================

func initLogger() error {
	logger = &DailyLogger{
		logPath: logPath,
	}
	return logger.rotateIfNeeded()
}

func (l *DailyLogger) rotateIfNeeded() error {
	today := time.Now().Format("02-01-2006")

	// Si es el mismo día y los archivos están abiertos, no hacer nada
	if l.currentDate == today && l.accessFile != nil {
		return nil
	}

	// Cerrar archivos anteriores
	l.closeFiles()

	// Crear directorio del día
	dayDir := filepath.Join(l.logPath, today)
	if err := os.MkdirAll(dayDir, 0755); err != nil {
		return fmt.Errorf("error creando directorio de logs: %v", err)
	}

	// Abrir archivos nuevos
	var err error
	l.accessFile, err = os.OpenFile(
		filepath.Join(dayDir, "access.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0644,
	)
	if err != nil {
		return fmt.Errorf("error abriendo access.log: %v", err)
	}

	l.errorFile, err = os.OpenFile(
		filepath.Join(dayDir, "error.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0644,
	)
	if err != nil {
		return fmt.Errorf("error abriendo error.log: %v", err)
	}

	l.debugFile, err = os.OpenFile(
		filepath.Join(dayDir, "debug.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0644,
	)
	if err != nil {
		return fmt.Errorf("error abriendo debug.log: %v", err)
	}

	// Configurar loggers
	l.accessLogger = log.New(io.MultiWriter(os.Stdout, l.accessFile, l.debugFile), "[INFO] ", log.LstdFlags)
	l.errorLogger = log.New(io.MultiWriter(os.Stderr, l.errorFile, l.debugFile), "[ERROR] ", log.LstdFlags)
	l.debugLogger = log.New(l.debugFile, "[DEBUG] ", log.LstdFlags)

	l.currentDate = today
	return nil
}

func (l *DailyLogger) closeFiles() {
	if l.accessFile != nil {
		l.accessFile.Close()
	}
	if l.errorFile != nil {
		l.errorFile.Close()
	}
	if l.debugFile != nil {
		l.debugFile.Close()
	}
}

func (l *DailyLogger) Info(format string, v ...interface{}) {
	l.rotateIfNeeded()
	l.accessLogger.Printf(format, v...)
}

func (l *DailyLogger) Error(format string, v ...interface{}) {
	l.rotateIfNeeded()
	l.errorLogger.Printf(format, v...)
	errorsCount++
}

func (l *DailyLogger) Debug(format string, v ...interface{}) {
	if config.SaveDebug {
		l.rotateIfNeeded()
		l.debugLogger.Printf(format, v...)
	}
}

// ============================================================================
// GESTIÓN DE CONFIGURACIÓN
// ============================================================================

func loadConfig() error {
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("error creando directorio de configuración: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		config = Config{
			DBServer:   "localhost",
			DBDatabase: "restaurantes",
			DBUser:     "root",
			DBPassword: "",
			URLBase:    "localhost/usqay",
			Agrupar:    true,
			SaveDebug:  false,
			APIPort:    8765,
		}
		return saveConfig()
	}

	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("error parseando configuración: %v", err)
	}

	return nil
}

func saveConfig() error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("error serializando configuración: %v", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("error guardando configuración: %v", err)
	}

	return nil
}

func connectDB() error {
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?parseTime=true",
		config.DBUser,
		config.DBPassword,
		config.DBServer,
		config.DBDatabase,
	)

	var err error
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("error abriendo conexión a la base de datos: %v", err)
	}

	if err := db.Ping(); err != nil {
		return fmt.Errorf("error conectando a la base de datos: %v", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	logger.Info("Conexión a la base de datos establecida correctamente")
	return nil
}

// ============================================================================
// GESTIÓN DE SERVICIOS - WINDOWS
// ============================================================================

func downloadAndExtractNSSM(destDir string) error {
	resp, err := http.Get(nssmURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	zipReader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return err
	}

	var nssmFile *zip.File
	for _, file := range zipReader.File {
		if strings.Contains(file.Name, "win64/nssm.exe") {
			nssmFile = file
			break
		}
	}

	if nssmFile == nil {
		return fmt.Errorf("no se encontró win64/nssm.exe en el archivo zip")
	}

	src, err := nssmFile.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	destPath := filepath.Join(destDir, "nssm.exe")
	dst, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

func runCommand(command string, args ...string) error {
	cmd := exec.Command(command, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("falló comando '%s %v': %v\nSalida: %s", command, args, err, string(output))
	}
	return nil
}

func installServiceWindows() error {
	fmt.Println("🔧 Preparando instalación del servicio...")

	exePath, err := getExecutablePath()
	if err != nil {
		return fmt.Errorf("error obteniendo ruta del ejecutable: %v", err)
	}
	workDir := filepath.Dir(exePath)
	nssmPath := filepath.Join(workDir, "nssm.exe")

	if _, err := os.Stat(nssmPath); os.IsNotExist(err) {
		fmt.Println("⬇️  NSSM no encontrado. Descargando...")
		if err := downloadAndExtractNSSM(workDir); err != nil {
			return fmt.Errorf("error descargando NSSM: %v", err)
		}
	}

	cmd := exec.Command(nssmPath, "status", serviceName)
	if err := cmd.Run(); err == nil {
		return fmt.Errorf("el servicio ya está instalado. Usa --uninstall windows primero")
	}

	fmt.Println("⚙️  Configurando servicio con NSSM...")

	if err := runCommand(nssmPath, "install", serviceName, exePath); err != nil {
		return err
	}

	if err := runCommand(nssmPath, "set", serviceName, "AppDirectory", workDir); err != nil {
		return err
	}

	if err := runCommand(nssmPath, "set", serviceName, "DisplayName", serviceDisplay); err != nil {
		return err
	}

	if err := runCommand(nssmPath, "set", serviceName, "Description", serviceDesc); err != nil {
		return err
	}

	if err := runCommand(nssmPath, "set", serviceName, "Start", "SERVICE_AUTO_START"); err != nil {
		return err
	}

	logFile := filepath.Join(workDir, "usqay_service.log")
	runCommand(nssmPath, "set", serviceName, "AppStdout", logFile)
	runCommand(nssmPath, "set", serviceName, "AppStderr", logFile)

	fmt.Println("✅ Servicio instalado correctamente")
	fmt.Println("💡 Iniciando servicio...")

	exec.Command(nssmPath, "start", serviceName).Run()
	return nil
}

func uninstallServiceWindows() error {
	fmt.Println("🗑️  Desinstalando servicio...")

	nssmPath, err := getNssmPath()
	if err != nil {
		return err
	}

	if _, err := os.Stat(nssmPath); os.IsNotExist(err) {
		return fmt.Errorf("no se encontró nssm.exe")
	}

	exec.Command(nssmPath, "stop", serviceName).Run()

	if err := runCommand(nssmPath, "remove", serviceName, "confirm"); err != nil {
		return fmt.Errorf("error eliminando servicio: %v", err)
	}

	fmt.Println("✅ Servicio eliminado correctamente")
	return nil
}

func startServiceWindows() error {
	fmt.Println("▶️  Iniciando servicio...")

	nssmPath, err := getNssmPath()
	if err != nil {
		return err
	}

	cmd := exec.Command(nssmPath, "start", serviceName)
	output, err := cmd.CombinedOutput()

	if err != nil {
		if strings.Contains(string(output), "running") {
			fmt.Println("⚠️  El servicio ya estaba corriendo")
			return nil
		}
		return fmt.Errorf("error iniciando servicio: %v\n%s", err, string(output))
	}

	fmt.Println("✅ Servicio iniciado correctamente")
	return nil
}

func stopServiceWindows() error {
	fmt.Println("⏹️  Deteniendo servicio...")

	nssmPath, err := getNssmPath()
	if err != nil {
		return err
	}

	cmd := exec.Command(nssmPath, "stop", serviceName)
	output, err := cmd.CombinedOutput()

	if err != nil {
		if strings.Contains(string(output), "stopped") {
			fmt.Println("⚠️  El servicio ya estaba detenido")
			return nil
		}
		return fmt.Errorf("error deteniendo servicio: %v\n%s", err, string(output))
	}

	fmt.Println("✅ Servicio detenido")
	return nil
}

func statusServiceWindows() error {
	nssmPath, err := getNssmPath()
	if err != nil {
		return fmt.Errorf("no se pudo ubicar nssm: %v", err)
	}

	cmd := exec.Command(nssmPath, "status", serviceName)
	output, err := cmd.CombinedOutput()

	fmt.Println("\n📊 Estado del Servicio:")
	fmt.Println(strings.Repeat("─", 40))

	if err != nil {
		if strings.Contains(string(output), "Can't open service") {
			fmt.Println("❌ NO INSTALADO")
		} else {
			fmt.Printf("❌ Error: %s\n", string(output))
		}
		return nil
	}

	estado := strings.TrimSpace(string(output))
	switch estado {
	case "SERVICE_RUNNING":
		fmt.Println("🟢 CORRIENDO")
	case "SERVICE_STOPPED":
		fmt.Println("🔴 DETENIDO")
	case "SERVICE_PAUSED":
		fmt.Println("⏸️  PAUSADO")
	default:
		fmt.Printf("⚪ %s\n", estado)
	}

	return nil
}

// ============================================================================
// GESTIÓN DE SERVICIOS - LINUX
// ============================================================================

func installServiceLinux() error {
	fmt.Println("🔧 Instalando servicio en Linux...")

	if os.Geteuid() != 0 {
		exePath, _ := getExecutablePath()
		fmt.Println("\n⚠️  Se requieren permisos de root")
		fmt.Printf("Ejecuta: sudo %s --install linux\n", exePath)
		return fmt.Errorf("permisos insuficientes")
	}

	exePath, err := getExecutablePath()
	if err != nil {
		return fmt.Errorf("error obteniendo ruta del ejecutable: %v", err)
	}

	serviceContent := fmt.Sprintf(`[Unit]
Description=Usqay Print Server
After=network.target mysql.service

[Service]
Type=simple
User=%s
WorkingDirectory=%s
ExecStart=%s
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
`, os.Getenv("USER"), filepath.Dir(exePath), exePath)

	servicePath := "/etc/systemd/system/usqay-print.service"

	if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
		return fmt.Errorf("error creando archivo de servicio: %v", err)
	}

	exec.Command("systemctl", "daemon-reload").Run()
	exec.Command("systemctl", "enable", "usqay-print").Run()

	fmt.Println("✅ Servicio instalado correctamente")
	fmt.Println("💡 Iniciar: sudo systemctl start usqay-print")
	fmt.Println("💡 Logs: sudo journalctl -u usqay-print -f")

	return nil
}

func uninstallServiceLinux() error {
	fmt.Println("🗑️  Desinstalando servicio...")

	if os.Geteuid() != 0 {
		return fmt.Errorf("se requieren permisos de root")
	}

	exec.Command("systemctl", "stop", "usqay-print").Run()
	exec.Command("systemctl", "disable", "usqay-print").Run()

	servicePath := "/etc/systemd/system/usqay-print.service"
	os.Remove(servicePath)

	exec.Command("systemctl", "daemon-reload").Run()

	fmt.Println("✅ Servicio desinstalado")
	return nil
}

func startServiceLinux() error {
	fmt.Println("▶️  Iniciando servicio...")

	cmd := exec.Command("systemctl", "start", "usqay-print")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("error: %v\n%s", err, string(output))
	}

	fmt.Println("✅ Servicio iniciado")
	return nil
}

func stopServiceLinux() error {
	fmt.Println("⏹️  Deteniendo servicio...")

	cmd := exec.Command("systemctl", "stop", "usqay-print")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("error: %v\n%s", err, string(output))
	}

	fmt.Println("✅ Servicio detenido")
	return nil
}

func statusServiceLinux() error {
	cmd := exec.Command("systemctl", "status", "usqay-print")
	output, _ := cmd.CombinedOutput()

	fmt.Println("\n📊 Estado del Servicio:")
	fmt.Println(strings.Repeat("─", 40))
	fmt.Println(string(output))

	return nil
}

// ============================================================================
// GESTIÓN DE SERVICIOS - macOS
// ============================================================================

func installServiceMacOS() error {
	fmt.Println("🔧 Instalando servicio en macOS...")

	exePath, err := getExecutablePath()
	if err != nil {
		return fmt.Errorf("error obteniendo ruta del ejecutable: %v", err)
	}

	homeDir, _ := os.UserHomeDir()
	plistPath := filepath.Join(homeDir, "Library", "LaunchAgents", "com.usqay.print.plist")

	plistContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.usqay.print</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>StandardOutPath</key>
	<string>%s/usqay-stdout.log</string>
	<key>StandardErrorPath</key>
	<string>%s/usqay-stderr.log</string>
	<key>WorkingDirectory</key>
	<string>%s</string>
</dict>
</plist>
`, exePath, logPath, logPath, filepath.Dir(exePath))

	os.MkdirAll(filepath.Dir(plistPath), 0755)

	if err := os.WriteFile(plistPath, []byte(plistContent), 0644); err != nil {
		return fmt.Errorf("error creando archivo plist: %v", err)
	}

	cmd := exec.Command("launchctl", "load", plistPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("error cargando servicio: %v\n%s", err, string(output))
	}

	fmt.Println("✅ Servicio instalado correctamente")
	fmt.Println("💡 Ver estado: launchctl list | grep usqay")

	return nil
}

func uninstallServiceMacOS() error {
	fmt.Println("🗑️  Desinstalando servicio...")

	homeDir, _ := os.UserHomeDir()
	plistPath := filepath.Join(homeDir, "Library", "LaunchAgents", "com.usqay.print.plist")

	exec.Command("launchctl", "unload", plistPath).Run()
	os.Remove(plistPath)

	fmt.Println("✅ Servicio desinstalado")
	return nil
}

func startServiceMacOS() error {
	fmt.Println("▶️  Iniciando servicio...")

	homeDir, _ := os.UserHomeDir()
	plistPath := filepath.Join(homeDir, "Library", "LaunchAgents", "com.usqay.print.plist")

	cmd := exec.Command("launchctl", "load", plistPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("error: %v\n%s", err, string(output))
	}

	fmt.Println("✅ Servicio iniciado")
	return nil
}

func stopServiceMacOS() error {
	fmt.Println("⏹️  Deteniendo servicio...")

	homeDir, _ := os.UserHomeDir()
	plistPath := filepath.Join(homeDir, "Library", "LaunchAgents", "com.usqay.print.plist")

	cmd := exec.Command("launchctl", "unload", plistPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("error: %v\n%s", err, string(output))
	}

	fmt.Println("✅ Servicio detenido")
	return nil
}

func statusServiceMacOS() error {
	cmd := exec.Command("launchctl", "list")
	output, _ := cmd.CombinedOutput()

	fmt.Println("\n📊 Estado del Servicio:")
	fmt.Println(strings.Repeat("─", 40))

	lines := strings.Split(string(output), "\n")
	found := false
	for _, line := range lines {
		if strings.Contains(line, "usqay") {
			fmt.Println(line)
			found = true
		}
	}

	if !found {
		fmt.Println("❌ Servicio no instalado o no corriendo")
	}

	return nil
}

// ============================================================================
// FUNCIONES GENÉRICAS DE SERVICIO
// ============================================================================

func installService(os string) error {
	switch strings.ToLower(os) {
	case "windows":
		return installServiceWindows()
	case "linux":
		return installServiceLinux()
	case "macos", "darwin":
		return installServiceMacOS()
	default:
		return fmt.Errorf("sistema operativo no soportado: %s", os)
	}
}

func uninstallService(os string) error {
	switch strings.ToLower(os) {
	case "windows":
		return uninstallServiceWindows()
	case "linux":
		return uninstallServiceLinux()
	case "macos", "darwin":
		return uninstallServiceMacOS()
	default:
		return fmt.Errorf("sistema operativo no soportado: %s", os)
	}
}

func startService(os string) error {
	switch strings.ToLower(os) {
	case "windows":
		return startServiceWindows()
	case "linux":
		return startServiceLinux()
	case "macos", "darwin":
		return startServiceMacOS()
	default:
		return fmt.Errorf("sistema operativo no soportado: %s", os)
	}
}

func stopService(os string) error {
	switch strings.ToLower(os) {
	case "windows":
		return stopServiceWindows()
	case "linux":
		return stopServiceLinux()
	case "macos", "darwin":
		return stopServiceMacOS()
	default:
		return fmt.Errorf("sistema operativo no soportado: %s", os)
	}
}

func statusService(os string) error {
	switch strings.ToLower(os) {
	case "windows":
		return statusServiceWindows()
	case "linux":
		return statusServiceLinux()
	case "macos", "darwin":
		return statusServiceMacOS()
	default:
		return fmt.Errorf("sistema operativo no soportado: %s", os)
	}
}

func restartService(os string) error {
	fmt.Println("🔄 Reiniciando servicio...")
	if err := stopService(os); err != nil {
		return err
	}
	time.Sleep(2 * time.Second)
	return startService(os)
}

// ============================================================================
// API REST
// ============================================================================

func startAPIServer() {
	if config.APIPort == 0 {
		config.APIPort = 8765
	}

	http.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		status := ServiceStatus{
			Running:       true,
			StartTime:     serviceStatus.StartTime,
			JobsProcessed: jobsProcessed,
			LastJob:       serviceStatus.LastJob,
			Version:       VERSION,
			OS:            runtime.GOOS,
			Errors:        errorsCount,
		}
		json.NewEncoder(w).Encode(status)
	})

	http.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(config)
	})

	http.HandleFunc("/api/queue", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		rows, err := db.Query("SELECT id, codigo, terminal, aux, tipo FROM cola_impresion ORDER BY id ASC LIMIT 10")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var jobs []PrintJob
		for rows.Next() {
			var job PrintJob
			rows.Scan(&job.ID, &job.Codigo, &job.Terminal, &job.Aux, &job.Tipo)
			jobs = append(jobs, job)
		}
		json.NewEncoder(w).Encode(jobs)
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	addr := fmt.Sprintf(":%d", config.APIPort)
	logger.Info("🌐 API REST iniciada en http://localhost%s", addr)

	go http.ListenAndServe(addr, nil)
}

// ============================================================================
// GESTIÓN DE IMPRESORAS
// ============================================================================


func refreshPrinters() error {
	logger.Info("Iniciando actualización de impresoras...")

	printers, err := compatibility.GetInstalledPrinters()
	if err != nil {
		return fmt.Errorf("error obteniendo impresoras: %v", err)
	}

	if _, err := db.Exec("TRUNCATE TABLE impresoras"); err != nil {
		return fmt.Errorf("error limpiando tabla de impresoras: %v", err)
	}

	stmt, err := db.Prepare("INSERT INTO impresoras (nombre) VALUES (?)")
	if err != nil {
		return fmt.Errorf("error preparando statement: %v", err)
	}
	defer stmt.Close()

	for _, printer := range printers {
		if _, err := stmt.Exec(printer); err != nil {
			logger.Error("Error insertando impresora %s: %v", printer, err)
			continue
		}
		logger.Info("Impresora agregada: %s", printer)

		if err := createDefaultMargins(printer); err != nil {
			logger.Error("Error creando márgenes para %s: %v", printer, err)
		}
	}

	logger.Info("Actualización completada. Total: %d impresoras", len(printers))
	return nil
}

func createDefaultMargins(printer string) error {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM margenes_impresion WHERE impresora = ?", printer).Scan(&count)
	if err != nil {
		return err
	}

	if count > 0 {
		return nil
	}

	query := `INSERT INTO margenes_impresion (impresora, ratio) VALUES (?, 0.99)`
	_, err = db.Exec(query, printer)
	return err
}

// ============================================================================
// LIMPIEZA DE LOGS
// ============================================================================

func cleanOldLogs() error {
	logger.Info("Limpiando logs antiguos (30+ días)...")

	retentionDays := 30
	cutoffTime := time.Now().AddDate(0, 0, -retentionDays)
	totalDeleted := 0

	// Leer directorios del logPath
	entries, err := os.ReadDir(logPath)
	if err != nil {
		return fmt.Errorf("error leyendo directorio de logs: %v", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Verificar si es un directorio de fecha (formato DD-MM-YYYY)
		dirName := entry.Name()
		if len(dirName) == 10 && strings.Count(dirName, "-") == 2 {
			dirPath := filepath.Join(logPath, dirName)

			info, err := entry.Info()
			if err != nil {
				logger.Error("Error obteniendo info de %s: %v", dirPath, err)
				continue
			}

			// Eliminar directorios antiguos
			if info.ModTime().Before(cutoffTime) {
				if err := os.RemoveAll(dirPath); err != nil {
					logger.Error("Error eliminando %s: %v", dirPath, err)
					continue
				}
				logger.Debug("Directorio eliminado: %s", dirName)
				totalDeleted++
			}
		}
	}

	// También limpiar directorio prints si existe
	printsDir := filepath.Join(logPath, "prints")
	if _, err := os.Stat(printsDir); err == nil {
		files, err := os.ReadDir(printsDir)
		if err == nil {
			for _, file := range files {
				if file.IsDir() {
					continue
				}

				filePath := filepath.Join(printsDir, file.Name())
				info, err := file.Info()
				if err != nil {
					continue
				}

				if info.ModTime().Before(cutoffTime) {
					if err := os.Remove(filePath); err == nil {
						totalDeleted++
					}
				}
			}
		}
	}

	if totalDeleted > 0 {
		logger.Info("✅ Limpieza completada: %d elementos eliminados", totalDeleted)
	} else {
		logger.Info("✅ No se encontraron logs antiguos para eliminar")
	}

	return nil
}

// ============================================================================
// PROCESAMIENTO DE TRABAJOS DE IMPRESIÓN
// ============================================================================

func getNextPrintJob() (*PrintJob, error) {
	query := "SELECT id, codigo, terminal, aux, tipo FROM cola_impresion ORDER BY id ASC LIMIT 1"

	job := &PrintJob{}
	err := db.QueryRow(query).Scan(&job.ID, &job.Codigo, &job.Terminal, &job.Aux, &job.Tipo)
	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("error consultando cola de impresión: %v", err)
	}

	return job, nil
}

func buildPrintURL(job *PrintJob) string {
	baseURL := fmt.Sprintf("http://%s/ArchivosImpresion/", config.URLBase)
	params := url.Values{}
	params.Add("id", job.Codigo)

	var auxParams []string
	if job.Aux.Valid {
		auxParams = strings.Split(job.Aux.String, ",")
	}

	tipo := strings.ToUpper(job.Tipo)

	switch {
	case strings.Contains(tipo, "CUENTA"):
		if len(auxParams) > 0 {
			params.Add("cajero", auxParams[0])
		}
		return baseURL + "cuenta.php?" + params.Encode()
	case strings.Contains(tipo, "BOLETA"):
		if len(auxParams) >= 2 {
			params.Add("cajero", auxParams[0])
			params.Add("consumo", auxParams[1])
		}
		return baseURL + "boleta.php?" + params.Encode()
	case strings.Contains(tipo, "FACTURA"):
		if len(auxParams) >= 2 {
			params.Add("cajero", auxParams[0])
			params.Add("consumo", auxParams[1])
		}
		return baseURL + "factura.php?" + params.Encode()
	case strings.Contains(tipo, "PED"):
		params.Add("agrupar", fmt.Sprintf("%t", config.Agrupar))
		return baseURL + "comanda.php?" + params.Encode()
	case strings.Contains(tipo, "CIE"):
		if len(auxParams) >= 4 {
			params.Add("fecha", job.Codigo)
			params.Add("cajero", auxParams[0])
			params.Add("corte", auxParams[1])
			params.Add("inicial", auxParams[2])
			params.Add("caja", auxParams[3])
		}
		return baseURL + "cierre_caja.php?" + params.Encode()
	default:
		return baseURL + "reimpresion.php?" + params.Encode()
	}
}

func removePrintJob(jobID int) error {
	_, err := db.Exec("DELETE FROM cola_impresion WHERE id = ?", jobID)
	return err
}

func downloadHTML(url string) ([]byte, error) {
	client := &http.Client{
		Timeout: 15 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("error en petición HTTP: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("código HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error leyendo respuesta: %v", err)
	}

	return content, nil
}

func extractTitle(htmlContent []byte) string {
	doc, err := html.Parse(strings.NewReader(string(htmlContent)))
	if err != nil {
		return ""
	}

	var title string
	var findTitle func(*html.Node)
	findTitle = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "title" {
			if n.FirstChild != nil {
				title = strings.TrimSpace(n.FirstChild.Data)
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findTitle(c)
			if title != "" {
				return
			}
		}
	}

	findTitle(doc)
	return title
}

func getPrinterConfigsByTitle(terminal, title string) ([]PrinterConfig, error) {
	query := `SELECT ci.impresora, mi.ratio 
		FROM configuracion_impresion ci 
		INNER JOIN margenes_impresion mi ON ci.impresora = mi.impresora 
		WHERE ci.terminal = ? AND ci.opcion = ?`

	rows, err := db.Query(query, terminal, title)
	if err != nil {
		return nil, fmt.Errorf("error en consulta SQL: %v", err)
	}
	defer rows.Close()

	var configs []PrinterConfig
	for rows.Next() {
		var config PrinterConfig
		if err := rows.Scan(&config.Impresora, &config.Ratio); err != nil {
			logger.Error("Error escaneando configuración: %v", err)
			continue
		}
		configs = append(configs, config)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterando resultados: %v", err)
	}

	return configs, nil
}

func printHTMLDocument(printerName string, htmlContent []byte, ratio float64, jobID string) error {
	logger.Debug("[JOB:%s] Preparando impresión para %s", jobID, printerName)

	if config.SaveDebug {
		debugPath := filepath.Join(logPath, "prints", fmt.Sprintf("print_%s_%s.html",
			jobID,
			strings.ReplaceAll(printerName, " ", "_")))

		os.MkdirAll(filepath.Dir(debugPath), 0755)
		if err := os.WriteFile(debugPath, htmlContent, 0644); err != nil {
			logger.Error("[JOB:%s] Error guardando debug: %v", jobID, err)
		} else {
			logger.Debug("[JOB:%s] HTML guardado en: %s", jobID, debugPath)
		}
	}

	logger.Info("[JOB:%s] 🖨️  Enviando a '%s' (ratio: %.2f)", jobID, printerName, ratio)
	time.Sleep(100 * time.Millisecond)

	return nil
}

func shouldDoublePrint(tipo string) bool {
	tipo = strings.ToUpper(tipo)
	return strings.Contains(tipo, "CONSUMO") || strings.Contains(tipo, "CREDITO")
}

func logJobError(job *PrintJob, errorMsg string) {
	logFile := filepath.Join(filepath.Dir(configPath), "usqaylog.txt")

	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		logger.Error("Error abriendo archivo de log: %v", err)
		return
	}
	defer f.Close()

	timestamp := time.Now().Format("01/02/2006 3:04 PM")
	logLine := fmt.Sprintf("<%s>Error: %s (ID: %d, Terminal: %s, Tipo: %s)\n",
		timestamp, errorMsg, job.ID, job.Terminal, job.Tipo)

	f.WriteString(logLine)
}

func processNextJob() error {
	job, err := getNextPrintJob()
	if err != nil {
		logger.Error("Error obteniendo trabajo: %v", err)
		return err
	}

	if job == nil {
		return nil
	}

	jobID := uuid.New().String()[:8]
	logger.Info("[JOB:%s] Procesando ID=%d, Terminal=%s, Tipo=%s", jobID, job.ID, job.Terminal, job.Tipo)

	docURL := buildPrintURL(job)
	logger.Debug("[JOB:%s] URL generada: %s", jobID, docURL)

	htmlContent, err := downloadHTML(docURL)
	if err != nil {
		logger.Error("[JOB:%s] Error descargando HTML: %v", jobID, err)
		logJobError(job, fmt.Sprintf("Error descargando documento: %v", err))
		removePrintJob(job.ID)
		return err
	}

	docTitle := extractTitle(htmlContent)
	if docTitle == "" {
		logger.Error("[JOB:%s] No se pudo extraer el título del documento", jobID)
		logJobError(job, "Documento sin título válido")
		removePrintJob(job.ID)
		return fmt.Errorf("título de documento vacío")
	}

	logger.Info("[JOB:%s] Título del documento: '%s'", jobID, docTitle)

	printerConfigs, err := getPrinterConfigsByTitle(job.Terminal, docTitle)
	if err != nil {
		logger.Error("[JOB:%s] Error obteniendo configuración: %v", jobID, err)
		logJobError(job, fmt.Sprintf("No se encontró configuración para terminal='%s', opcion='%s'", job.Terminal, docTitle))
		removePrintJob(job.ID)
		return err
	}

	if len(printerConfigs) == 0 {
		logger.Error("[JOB:%s] No hay impresoras configuradas para terminal='%s', opcion='%s'", jobID, job.Terminal, docTitle)
		logJobError(job, fmt.Sprintf("Sin configuración de impresión para tipo '%s'", docTitle))
		removePrintJob(job.ID)
		return fmt.Errorf("sin configuración de impresión")
	}

	successCount := 0
	for _, pc := range printerConfigs {
		logger.Info("[JOB:%s] Imprimiendo en: %s (ratio: %.2f)", jobID, pc.Impresora, pc.Ratio)

		if err := printHTMLDocument(pc.Impresora, htmlContent, pc.Ratio, jobID); err != nil {
			logger.Error("[JOB:%s] Error imprimiendo en %s: %v", jobID, pc.Impresora, err)
			logJobError(job, fmt.Sprintf("Error en impresora %s: %v", pc.Impresora, err))
		} else {
			successCount++
			logger.Info("[JOB:%s] ✓ Impresión exitosa en %s", jobID, pc.Impresora)
		}
	}

	if shouldDoublePrint(job.Tipo) {
		logger.Info("[JOB:%s] Realizando impresión doble...", jobID)
		for _, pc := range printerConfigs {
			printHTMLDocument(pc.Impresora, htmlContent, pc.Ratio, jobID)
		}
	}

	if err := removePrintJob(job.ID); err != nil {
		logger.Error("[JOB:%s] Error eliminando trabajo: %v", jobID, err)
		return err
	}

	jobsProcessed++
	serviceStatus.LastJob = time.Now()

	if successCount > 0 {
		logger.Info("[JOB:%s] ✅ Completado (%d/%d impresoras)", jobID, successCount, len(printerConfigs))
	}

	return nil
}

func initJobs() {
	logger.Info("🖨️  Sistema de impresión iniciado")
	logger.Info("⏱️  Intervalo de polling: 800ms")

	ticker := time.NewTicker(800 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		processNextJob()
	}
}

// ============================================================================
// AYUDA
// ============================================================================

func showHelp() {
	fmt.Printf(`
Usqay Print Server v%s
Servidor de impresión multiplataforma                 
USO:
  usqay [opciones]

SERVICIO:
  --install <os>      Instalar servicio
  --uninstall <os>    Desinstalar servicio
  --start <os>        Iniciar servicio
  --stop <os>         Detener servicio
  --restart <os>      Reiniciar servicio
  --status <os>       Ver estado

  <os>: windows | linux | macos

OPERACIÓN:
  -r                  Actualizar impresoras
  --clear             Limpiar logs antiguos (30+ días)
  --api               Habilitar API REST
  --version           Versión del programa
  --help              Mostrar esta ayuda

EJEMPLOS:
  usqay --install windows     # Instalar en Windows
  usqay --start linux         # Iniciar en Linux
  usqay -r                    # Actualizar impresoras
  usqay --clear               # Limpiar logs antiguos
  usqay                       # Iniciar servidor

API REST (con --api):
  GET /api/status             Estado del servicio
  GET /api/config             Configuración
  GET /api/queue              Cola de impresión
  GET /health                 Health check

ARCHIVOS:
  Config: %s
  Logs:   %s

Documentación: https://usqay.com/docs
`, VERSION, configPath, logPath)
}

// ============================================================================
// MAIN
// ============================================================================

func main() {
	flag.Parse()

	// Si se pasa --help o cualquier argumento desconocido
	if *helpFlag || (flag.NArg() > 0 && flag.NFlag() == 0) {
		showHelp()
		return
	}

	// Versión
	if *versionFlag {
		fmt.Printf("Usqay Print Server v%s (%s/%s)\n", VERSION, runtime.GOOS, runtime.GOARCH)
		return
	}

	// Gestión de servicios
	if *installFlag != "" {
		if err := installService(*installFlag); err != nil {
			log.Fatalf("❌ Error: %v", err)
		}
		return
	}

	if *uninstallFlag != "" {
		if err := uninstallService(*uninstallFlag); err != nil {
			log.Fatalf("❌ Error: %v", err)
		}
		return
	}

	if *startFlag != "" {
		if err := startService(*startFlag); err != nil {
			log.Fatalf("❌ Error: %v", err)
		}
		return
	}

	if *stopFlag != "" {
		if err := stopService(*stopFlag); err != nil {
			log.Fatalf("❌ Error: %v", err)
		}
		return
	}

	if *statusFlag != "" {
		if err := statusService(*statusFlag); err != nil {
			log.Fatalf("❌ Error: %v", err)
		}
		return
	}

	if *restartFlag != "" {
		if err := restartService(*restartFlag); err != nil {
			log.Fatalf("❌ Error: %v", err)
		}
		return
	}

	// Cargar configuración
	if err := loadConfig(); err != nil {
		log.Fatalf("❌ Error cargando configuración: %v", err)
	}

	if err := initLogger(); err != nil {
		log.Fatalf("❌ Error inicializando logger: %v", err)
	}

	logger.Info("Usqay Print Server v%-20s", VERSION)
	logger.Info("Sistema: %s/%s", runtime.GOOS, runtime.GOARCH)
	logger.Info("Config: %s", configPath)
	logger.Info("Logs: %s", logPath)

	if err := connectDB(); err != nil {
		logger.Error("❌ Error conectando BD: %v", err)
		os.Exit(1)
	}
	defer db.Close()

	serviceStatus.StartTime = time.Now()

	// Actualizar impresoras
	if *refreshFlag {
		if err := refreshPrinters(); err != nil {
			logger.Error("❌ Error: %v", err)
			os.Exit(1)
		}
		logger.Info("✅ Impresoras actualizadas correctamente")
		return
	}

	// Limpiar logs
	if *clearFlag {
		if err := cleanOldLogs(); err != nil {
			logger.Error("❌ Error: %v", err)
			os.Exit(1)
		}
		return
	}

	// API
	if *apiFlag {
		startAPIServer()
	}

	// Iniciar servicio
	logger.Info("🚀 Servidor iniciado. Presiona Ctrl+C para salir")
	initJobs()
}
