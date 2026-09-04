package config

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// DefaultMaxRetries is the default number of print attempts before marking a job as ERROR.
const DefaultMaxRetries = 3

// DefaultLocalAPIPort is the port the local HTTP print endpoint listens on when local_api_port is unset.
const DefaultLocalAPIPort = 9100

// Config holds all runtime configuration loaded from config.json.
type Config struct {
	ServerURL     string `json:"server_url"`
	TerminalID    string `json:"terminal_id"`
	Token         string `json:"token"`
	LogLevel      string `json:"log_level"`
	CapturePRN    bool   `json:"capture_prn"`
	MaxRetries    int    `json:"max_retries,omitempty"`
	LocalAPIPort  int    `json:"local_api_port,omitempty"`
	LocalAPIToken string `json:"local_api_token,omitempty"`
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
		cfg, err := runFirstTimeSetup(path, os.Stdin, os.Stdout)
		if err != nil {
			return nil, fmt.Errorf("configuración inicial de config.json: %w", err)
		}
		applyDefaults(cfg)
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("leer config.json: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsear config.json: %w", err)
	}

	applyDefaults(&cfg)
	return &cfg, nil
}

// applyDefaults rellena los campos opcionales que no vienen en config.json.
// server_url, terminal_id y token no tienen default: quedan vacíos si faltan,
// y Validate() los rechaza más adelante.
func applyDefaults(cfg *Config) {
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = DefaultMaxRetries
	}
	if cfg.LocalAPIPort <= 0 {
		cfg.LocalAPIPort = DefaultLocalAPIPort
	}
}

// runFirstTimeSetup se ejecuta solo cuando config.json no existe: pregunta por
// consola los campos obligatorios (server_url, terminal_id, token), arma el
// config.json en path y lo devuelve ya cargado. En corridas siguientes, como
// config.json ya existe, Load() nunca vuelve a llamar esta función.
func runFirstTimeSetup(path string, in io.Reader, out io.Writer) (*Config, error) {
	fmt.Fprintf(out, "config.json no encontrado en %s\n", path)
	fmt.Fprintln(out, "Primer arranque: configuremos el agente respondiendo estos datos.")

	reader := bufio.NewReader(in)

	serverURL, err := promptRequired(reader, out, "server_url (ej. wss://app.usqay.com/ws): ")
	if err != nil {
		return nil, err
	}
	terminalID, err := promptOptional(reader, out, "terminal_id (opcional, ej. caja-01 — Enter para omitir): ")
	if err != nil {
		return nil, err
	}
	token, err := promptRequired(reader, out, "token: ")
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		ServerURL:  serverURL,
		TerminalID: terminalID,
		Token:      token,
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("serializar config.json: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return nil, fmt.Errorf("escribir config.json en %s: %w", path, err)
	}

	fmt.Fprintf(out, "config.json creado en %s\n", path)
	return cfg, nil
}

// promptRequired imprime label y lee una línea de in, reintentando mientras
// venga vacía. Devuelve error si la entrada se corta (EOF) antes de un valor
// no vacío, para no dejar un config.json a medio llenar.
func promptRequired(reader *bufio.Reader, out io.Writer, label string) (string, error) {
	for {
		fmt.Fprint(out, label)
		line, readErr := reader.ReadString('\n')
		value := strings.TrimSpace(line)
		if value != "" {
			return value, nil
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return "", fmt.Errorf("entrada terminada antes de completar %q", strings.TrimSuffix(label, ": "))
			}
			return "", fmt.Errorf("leer entrada de consola: %w", readErr)
		}
		fmt.Fprintln(out, "  este campo es obligatorio, intenta de nuevo")
	}
}

// promptOptional imprime label y lee una línea de in, sin reintentar: una
// respuesta vacía (incluido EOF inmediato) es válida y devuelve "".
func promptOptional(reader *bufio.Reader, out io.Writer, label string) (string, error) {
	fmt.Fprint(out, label)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("leer entrada de consola: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// Validate checks that all required fields are present. terminal_id es
// opcional: solo actúa como llave de la caché offline local (ver
// Connection.LoadCachedConfig); la identidad real de la terminal frente al
// servidor la determina el token.
func (c *Config) Validate() error {
	if c.ServerURL == "" {
		return fmt.Errorf("server_url es requerido en config.json")
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
