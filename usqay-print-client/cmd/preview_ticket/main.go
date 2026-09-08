package main

import (
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

func main() {
	if len(os.Args) < 2 {
		fmt.Println("==================================================")
		fmt.Println("🎨 PREVISUALIZADOR Y BENCHMARK DE TICKETS (USQAY)")
		fmt.Println("==================================================")
		fmt.Println("Uso:")
		fmt.Println("  preview_ticket <ruta_a_payload.json | directorio> [salida.png]")
		fmt.Println("\nEjemplos:")
		fmt.Println("  preview_ticket dist/rest/cola_01.json")
		fmt.Println("  preview_ticket dist/rest")
		fmt.Println("  preview_ticket dist/test")
		fmt.Println("==================================================")
		os.Exit(1)
	}

	targetPath := os.Args[1]
	fi, err := os.Stat(targetPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error accediendo a %q: %v\n", targetPath, err)
		os.Exit(1)
	}

	if fi.IsDir() {
		processDirectory(targetPath)
		return
	}

	outputPath := ""
	if len(os.Args) >= 3 {
		outputPath = os.Args[2]
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
