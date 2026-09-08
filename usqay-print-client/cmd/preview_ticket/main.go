package main

import (
	"flag"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"usqay-print-client/internal/htmlrender"
	"usqay-print-client/internal/printer"
)

type BenchmarkResult struct {
	FileName    string
	WidthPx     int
	HeightPx    int
	FileSizeKB  float64
	RenderMS    float64
	DispatchMS  float64
	TotalMS     float64
	ESCPOSBytes int
}

func printUsage() {
	fmt.Println("==================================================")
	fmt.Println("🎨 PREVISUALIZADOR Y BENCHMARK DE TICKETS (USQAY)")
	fmt.Println("==================================================")
	fmt.Println("Uso:")
	fmt.Println("  preview_ticket <payload.json | directorio> [salida.png]")
	fmt.Println("  preview_ticket --path <payload.json> [--live] [--port N] [--png-only]")
	fmt.Println("\nLa entrada puede ir como argumento posicional o con --path (equivalentes).")
	fmt.Println("Los flags valen en cualquier orden.")
	fmt.Println("\nEjemplos:")
	fmt.Println("  preview_ticket dist/rest/cola_01.json")
	fmt.Println("  preview_ticket dist/rest")
	fmt.Println("  preview_ticket --live dist/rest/cola_01.json")
	fmt.Println("  preview_ticket --live --port 1234 --path dist/rest/cola_01.json")
	fmt.Println("  preview_ticket --live --png-only dist/rest/cola_01.json")
	fmt.Println("\nModo --live: observa el .json y refresca la vista al guardar.")
	fmt.Println("  Sin --png-only sirve un servidor HTTP en 127.0.0.1:<port>")
	fmt.Println("  (--port 0 elige un puerto libre). Con --png-only solo")
	fmt.Println("  regenera el .png junto al .json, sin navegador.")
	fmt.Println("==================================================")
}

func main() {
	liveFlag := flag.Bool("live", false, "observa el .json y refresca la vista previa al guardar")
	portFlag := flag.Int("port", 8080, "puerto HTTP para --live (0 = elegir uno libre automáticamente)")
	pngOnlyFlag := flag.Bool("png-only", false, "con --live, solo regenera el .png en disco (sin servidor HTTP)")
	pathFlag := flag.String("path", "", "ruta del payload .json o directorio (equivale al argumento posicional)")
	flag.Usage = printUsage
	flag.Parse()

	// flag.Parse se detiene en el primer argumento posicional, así que
	// "archivo.json --live" dejaría los flags sin leer. Recorremos lo que
	// quede alternando: si empieza con "-" lo re-parseamos como flags, si no
	// es un posicional. Así los flags valen en cualquier orden.
	var positionals []string
	rest := flag.Args()
	for len(rest) > 0 {
		if strings.HasPrefix(rest[0], "-") {
			if err := flag.CommandLine.Parse(rest); err != nil {
				os.Exit(2)
			}
			rest = flag.CommandLine.Args()
			continue
		}
		positionals = append(positionals, rest[0])
		rest = rest[1:]
	}

	// La entrada puede venir por --path o como primer posicional. --path tiene
	// prioridad y, si se usa, todos los posicionales quedan para la salida.
	targetPath := *pathFlag
	outPositionals := positionals
	if targetPath == "" {
		if len(positionals) < 1 {
			printUsage()
			os.Exit(1)
		}
		targetPath = positionals[0]
		outPositionals = positionals[1:]
	}

	fi, err := os.Stat(targetPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error accediendo a %q: %v\n", targetPath, err)
		os.Exit(1)
	}

	if *liveFlag {
		if fi.IsDir() {
			fmt.Fprintln(os.Stderr, "❌ El modo --live requiere un archivo .json, no un directorio")
			os.Exit(1)
		}
		if err := runLive(targetPath, *portFlag, *pngOnlyFlag); err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n", err)
			os.Exit(1)
		}
		return
	}

	if fi.IsDir() {
		processDirectory(targetPath)
		return
	}

	outputPath := ""
	if len(outPositionals) >= 1 {
		outputPath = outPositionals[0]
	} else {
		ext := filepath.Ext(targetPath)
		base := strings.TrimSuffix(targetPath, ext)
		outputPath = base + ".png"
	}

	res, err := processSingleFile(targetPath, outputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("==================================================")
	fmt.Println("✅ PREVIEW GENERADO EXITOSAMENTE!")
	fmt.Printf("📄 Entrada          : %s\n", targetPath)
	fmt.Printf("🖼️  Salida           : %s\n", outputPath)
	fmt.Printf("📐 Dimensiones      : %d x %d px\n", res.WidthPx, res.HeightPx)
	fmt.Printf("📦 Tamaño PNG       : %.1f KB\n", res.FileSizeKB)
	fmt.Printf("⚡ Render PNG       : %.2f ms\n", res.RenderMS)
	fmt.Printf("🖨️  Despacho ESC/POS : %.2f ms (%d bytes)\n", res.DispatchMS, res.ESCPOSBytes)
	fmt.Printf("⏱️  Tiempo Total     : %.2f ms\n", res.TotalMS)
	fmt.Println("==================================================")
}

func processSingleFile(jsonPath, pngPath string) (*BenchmarkResult, error) {
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, fmt.Errorf("leer JSON %q: %w", jsonPath, err)
	}

	totalStart := time.Now()

	// 1. Medir Renderizado HTML a Imagen PNG en memoria
	renderStart := time.Now()
	img, err := htmlrender.RenderPayloadToImage(nil, string(raw))
	if err != nil {
		return nil, fmt.Errorf("renderizar a imagen: %w", err)
	}
	renderDuration := time.Since(renderStart)

	// Guardar PNG a disco
	outFile, err := os.Create(pngPath)
	if err != nil {
		return nil, fmt.Errorf("crear PNG %q: %w", pngPath, err)
	}
	defer outFile.Close()

	if err := png.Encode(outFile, img); err != nil {
		return nil, fmt.Errorf("codificar PNG: %w", err)
	}

	fi, err := outFile.Stat()
	fileSizeKB := 0.0
	if err == nil {
		fileSizeKB = float64(fi.Size()) / 1024.0
	}

	// 2. Medir Generación de Despacho ESC/POS Raster (GS v 0) a partir de la imagen ya en RAM
	dispatchStart := time.Now()
	_, _, payload, _ := htmlrender.BuildHTMLFromJSON(nil, string(raw))
	var activeProf printer.DeviceProfile
	if payload != nil && payload.PaperProperties.Width > 0 {
		activeProf = printer.DeriveProfileFromWidth(payload.PaperProperties.Width)
	} else {
		activeProf = printer.Default58mmProfile()
	}
	escBytes, err := htmlrender.ConvertImageToESC(activeProf, payload, img)
	if err != nil {
		return nil, fmt.Errorf("generar ESC/POS: %w", err)
	}
	dispatchDuration := time.Since(dispatchStart)

	totalDuration := time.Since(totalStart)
	bounds := img.Bounds()

	return &BenchmarkResult{
		FileName:    filepath.Base(jsonPath),
		WidthPx:     bounds.Dx(),
		HeightPx:    bounds.Dy(),
		FileSizeKB:  fileSizeKB,
		RenderMS:    float64(renderDuration.Microseconds()) / 1000.0,
		DispatchMS:  float64(dispatchDuration.Microseconds()) / 1000.0,
		TotalMS:     float64(totalDuration.Microseconds()) / 1000.0,
		ESCPOSBytes: len(escBytes),
	}, nil
}

func processDirectory(dirPath string) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error leyendo directorio %q: %v\n", dirPath, err)
		os.Exit(1)
	}

	var jsonFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			jsonFiles = append(jsonFiles, filepath.Join(dirPath, e.Name()))
		}
	}
	sort.Strings(jsonFiles)

	if len(jsonFiles) == 0 {
		fmt.Printf("⚠️  No se encontraron archivos .json en %q\n", dirPath)
		return
	}

	fmt.Println("=========================================================================================================")
	fmt.Printf("🎨 PROCESANDO DIRECTORIO: %s (%d comprobantes)\n", dirPath, len(jsonFiles))
	fmt.Println("=========================================================================================================")

	var results []*BenchmarkResult
	var totalRenderMS, totalDispatchMS float64

	for i, jsonPath := range jsonFiles {
		baseName := strings.TrimSuffix(jsonPath, filepath.Ext(jsonPath))
		pngPath := baseName + ".png"

		res, err := processSingleFile(jsonPath, pngPath)
		if err != nil {
			fmt.Printf("[%02d/%02d] ❌ %s: %v\n", i+1, len(jsonFiles), filepath.Base(jsonPath), err)
			continue
		}

		results = append(results, res)
		totalRenderMS += res.RenderMS
		totalDispatchMS += res.DispatchMS

		fmt.Printf("[%02d/%02d] ✅ %-28s | %4dx%-4d px | %5.1f KB | Render: %7.2f ms | Despacho: %7.2f ms\n",
			i+1, len(jsonFiles), res.FileName, res.WidthPx, res.HeightPx, res.FileSizeKB, res.RenderMS, res.DispatchMS)
	}

	count := float64(len(results))
	if count > 0 {
		avgRender := totalRenderMS / count
		avgDispatch := totalDispatchMS / count
		fmt.Println("=========================================================================================================")
		fmt.Printf("📊 RESUMEN BENCHMARK (%d comprobantes procesados)\n", len(results))
		fmt.Printf("⚡ Promedio Renderizado HTML → PNG   : %.2f ms por ticket\n", avgRender)
		fmt.Printf("🖨️  Promedio Despacho Total a ESC/POS : %.2f ms por ticket\n", avgDispatch)
		fmt.Println("=========================================================================================================")
	}
}
