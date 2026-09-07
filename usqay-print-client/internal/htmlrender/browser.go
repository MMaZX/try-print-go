package htmlrender

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
)

var (
	resolvedBrowserPath string
	browserResolveErr   error
	resolveOnce         sync.Once

	// ErrBrowserNotFound se retorna cuando no se encuentra ningún navegador Chromium en el sistema.
	ErrBrowserNotFound = errors.New("no se encontró ningún navegador Chromium instalado (Microsoft Edge o Google Chrome)")
)

// FindBrowserExecutable localiza la ruta absoluta al ejecutable del navegador
// Chromium instalado en el sistema operativo. En Windows prioriza Edge y Chrome.
// El resultado se almacena en caché tras la primera resolución exitosa.
func FindBrowserExecutable() (string, error) {
	resolveOnce.Do(func() {
		resolvedBrowserPath, browserResolveErr = detectBrowser()
	})
	return resolvedBrowserPath, browserResolveErr
}

// ResetBrowserCache limpia la caché para permitir una nueva detección (útil en tests).
func ResetBrowserCache() {
	resolveOnce = sync.Once{}
	resolvedBrowserPath = ""
	browserResolveErr = nil
}

func detectBrowser() (string, error) {
	// 1. Permitir anulación explícita vía variable de entorno
	if envPath := os.Getenv("USQAY_BROWSER_PATH"); envPath != "" {
		if fileExists(envPath) {
			return envPath, nil
		}
	}

	if runtime.GOOS == "windows" {
		return detectWindowsBrowser()
	}
	return detectLinuxBrowser()
}

func detectWindowsBrowser() (string, error) {
	programFiles := os.Getenv("ProgramFiles")
	programFilesX86 := os.Getenv("ProgramFiles(x86)")
	localAppData := os.Getenv("LocalAppData")

	candidates := []string{
		// 1. Microsoft Edge (Garantizado en Windows 10/11)
		filepath.Join(programFilesX86, "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(programFiles, "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(localAppData, "Microsoft", "Edge", "Application", "msedge.exe"),

		// 2. Google Chrome
		filepath.Join(programFiles, "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(programFilesX86, "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(localAppData, "Google", "Chrome", "Application", "chrome.exe"),

		// 3. Brave
		filepath.Join(programFiles, "BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
		filepath.Join(localAppData, "BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
	}

	for _, path := range candidates {
		if path != "" && fileExists(path) {
			return path, nil
		}
	}

	// Búsqueda en PATH de Windows como fallback
	for _, binName := range []string{"msedge.exe", "chrome.exe", "brave.exe"} {
		if path, err := exec.LookPath(binName); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("%w en Windows", ErrBrowserNotFound)
}

func detectLinuxBrowser() (string, error) {
	// Búsqueda directa en rutas estándar de Linux
	standardPaths := []string{
		"/usr/bin/google-chrome",
		"/usr/bin/google-chrome-stable",
		"/usr/bin/chromium-browser",
		"/usr/bin/chromium",
		"/usr/bin/brave-browser",
		"/usr/bin/microsoft-edge",
		"/usr/bin/microsoft-edge-stable",
		"/snap/bin/chromium",
	}

	for _, path := range standardPaths {
		if fileExists(path) {
			return path, nil
		}
	}

	// Búsqueda en PATH
	bins := []string{
		"google-chrome",
		"google-chrome-stable",
		"chromium-browser",
		"chromium",
		"brave-browser",
		"microsoft-edge",
	}

	for _, bin := range bins {
		if path, err := exec.LookPath(bin); err == nil {
			if fileExists(path) {
				return path, nil
			}
		}
	}

	// Búsqueda en cachés de desarrollo locales (~/.cache/ms-playwright o ~/.cache/puppeteer)
	home, _ := os.UserHomeDir()
	if home != "" {
		cacheGlobs := []string{
			filepath.Join(home, ".cache", "ms-playwright", "chromium-*", "chrome-linux64", "chrome"),
			filepath.Join(home, ".cache", "puppeteer", "chrome", "*", "chrome-linux64", "chrome"),
			filepath.Join(home, ".cache", "puppeteer", "chrome-headless-shell", "*", "chrome-headless-shell-linux64", "chrome-headless-shell"),
		}
		for _, pattern := range cacheGlobs {
			if matches, _ := filepath.Glob(pattern); len(matches) > 0 {
				for _, match := range matches {
					if fileExists(match) {
						return match, nil
					}
				}
			}
		}
	}

	return "", fmt.Errorf("%w en Linux", ErrBrowserNotFound)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
