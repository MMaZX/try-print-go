package main

import (
	"fmt"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"usqay-print-client/internal/ticketrender"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("==================================================")
		fmt.Println("🎨 PREVISUALIZADOR VISUAL DE TICKETS (USQAY)")
		fmt.Println("==================================================")
		fmt.Println("Uso:")
		fmt.Println("  preview_ticket <ruta_a_payload.json> [salida.png]")
		fmt.Println("\nEjemplos:")
		fmt.Println("  go run ./cmd/preview_ticket dist/test_full_payload.json")
		fmt.Println("  go run ./cmd/preview_ticket mi_ticket.json ticket_preview.png")
		fmt.Println("==================================================")
		os.Exit(1)
	}

	payloadPath := os.Args[1]
	outputPath := ""
	if len(os.Args) >= 3 {
		outputPath = os.Args[2]
	} else {
		// Por defecto: mismo nombre y ruta pero con extensión .png
		ext := filepath.Ext(payloadPath)
		base := strings.TrimSuffix(payloadPath, ext)
		outputPath = base + "_preview.png"
	}

	raw, err := os.ReadFile(payloadPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error leyendo archivo JSON %q: %v\n", payloadPath, err)
		os.Exit(1)
	}

	fmt.Println("==================================================")
	fmt.Println("🎨 RENDERIZANDO PREVIEW A IMAGEN (PNG)...")
	fmt.Printf("📄 Entrada : %s\n", payloadPath)
	fmt.Printf("🖼️  Salida  : %s\n", outputPath)
	fmt.Println("==================================================")

	start := time.Now()
	// prof=nil permite derivar automáticamente el ancho y propiedades del JSON (58mm u 80mm)
	img, err := ticketrender.RenderPayloadToImage(nil, string(raw))
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error renderizando ticket: %v\n", err)
		os.Exit(1)
	}

	outFile, err := os.Create(outputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error creando archivo PNG %q: %v\n", outputPath, err)
		os.Exit(1)
	}
	defer outFile.Close()

	if err := ticketrender.RenderPayloadToPNG(nil, string(raw), outFile); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error guardando imagen PNG: %v\n", err)
		os.Exit(1)
	}

	duration := time.Since(start)
	bounds := img.Bounds()

	fileInfo, err := outFile.Stat()
	fileSizeKB := 0.0
	if err == nil {
		fileSizeKB = float64(fileInfo.Size()) / 1024.0
	}

	fmt.Println("✅ PREVIEW GENERADO EXITOSAMENTE!")
	fmt.Printf("⏱️  Tiempo de renderizado : %.2f ms\n", float64(duration.Microseconds())/1000.0)
	fmt.Printf("📐 Dimensiones imagen    : %d x %d px\n", bounds.Dx(), bounds.Dy())
	fmt.Printf("📦 Tamaño del archivo PNG : %.1f KB\n", fileSizeKB)
	fmt.Printf("🚀 Archivo listo en       : %s\n", outputPath)
	fmt.Println("==================================================")

	_ = image.Rect // mantener import limpio
}
