package main

import (
	"fmt"
	"os"
	"time"

	"usqay-print-client/internal/printer"
	"usqay-print-client/internal/queue"
)

const samplePayload = `{
  "options": {
    "cut": true,
    "drawer": false
  },
  "paper_properties": {
    "width": 58.0,
    "scale": 1.0,
    "padding": [1.0, 1.0, 1.0, 1.0]
  },
  "body": [
    {
      "type": "text",
      "value": "RESTAURANTE EL BUEN SABOR",
      "align": "center",
      "bold": true,
      "size": "double"
    },
    {
      "type": "text",
      "value": "Av. Los Sauces 123 - San Isidro",
      "align": "center",
      "size": "normal"
    },
    {
      "type": "text",
      "value": "RUC: 20601234567 | TEL: 998877665",
      "align": "center",
      "size": "normal"
    },
    {
      "type": "separator",
      "character": "="
    },
    {
      "type": "columns",
      "columns": [
        { "text": "COMANDA #00128", "width": 0.5, "align": "left", "bold": true, "size": "medium" },
        { "text": "MESA 04", "width": 0.5, "align": "right", "bold": true, "size": "medium" }
      ]
    },
    {
      "type": "columns",
      "columns": [
        { "text": "Mozo: Carlos Mendoza", "width": 0.5, "align": "left" },
        { "text": "18/08/2026 10:15", "width": 0.5, "align": "right" }
      ]
    },
    {
      "type": "separator",
      "character": "-"
    },
    {
      "type": "table",
      "columns": [
        { "header": "CANT", "width": 0.12, "align": "left" },
        { "header": "DESCRIPCION", "width": 0.63, "align": "left" },
        { "header": "IMPORTE", "width": 0.25, "align": "right" }
      ],
      "rows": [
        {
          "bold": true,
          "cells": [
            { "text": "2" },
            { "text": "LOMO SALTADO C/ PAPAS CRUJIENTES" },
            { "text": "S/ 84.00" }
          ]
        },
        {
          "merge": true,
          "cells": [
            { "text": "↳ NOTA COCINA: Término medio, sin cebolla, papas bien doradas" }
          ]
        },
        {
          "bold": true,
          "cells": [
            { "text": "1" },
            { "text": "CEVICHE CLASICO MIXTO" },
            { "text": "S/ 48.00" }
          ]
        },
        {
          "merge": true,
          "cells": [
            { "text": "↳ NOTA: Poco picante (ají aparte)" }
          ]
        },
        {
          "cells": [
            { "text": "1" },
            { "text": "JARRA CHICHA MORADA 1L" },
            { "text": "S/ 18.00" }
          ]
        }
      ]
    },
    {
      "type": "separator",
      "character": "-"
    },
    {
      "type": "columns",
      "columns": [
        { "text": "Subtotal:", "width": 0.6, "align": "right" },
        { "text": "S/ 127.12", "width": 0.4, "align": "right" }
      ]
    },
    {
      "type": "columns",
      "columns": [
        { "text": "I.G.V. (18%):", "width": 0.6, "align": "right" },
        { "text": "S/ 22.88", "width": 0.4, "align": "right" }
      ]
    },
    {
      "type": "columns",
      "columns": [
        { "text": "TOTAL A PAGAR:", "width": 0.6, "align": "right", "bold": true, "size": "medium" },
        { "text": "S/ 150.00", "width": 0.4, "align": "right", "bold": true, "size": "medium" }
      ]
    },
    {
      "type": "separator",
      "character": "="
    },
    {
      "type": "qr",
      "value": "https://usqay.com/c/00128",
      "align": "center",
      "size": 5
    },
    {
      "type": "text",
      "value": "¡Gracias por su preferencia!",
      "align": "center",
      "bold": true,
      "size": "normal"
    },
    {
      "type": "spacer",
      "lines": 1
    }
  ]
}`

func main() {
	devicePath := "/dev/usb/lp0"
	if len(os.Args) > 1 {
		devicePath = os.Args[1]
	}

	fmt.Println("==================================================")
	fmt.Println("🚀 PRUEBA DE RENDERIZADO TÉRMICO (poster)")
	fmt.Printf("Dispositivo destino: %s\n", devicePath)
	fmt.Println("==================================================")

	// 1. Fase de Renderizado
	renderStart := time.Now()
	prof := &printer.DeviceProfile{
		WidthDots:         384,
		DPI:               203,
		CharWidthDots:     12,
		SupportsCut:       true,
		SupportsDrawer:    true,
		SupportsQRNative:  true,
		SupportsPrintArea: true,
		SupportsRaster:    true,
	}
	rasterBytes, err := queue.RenderThermal(prof, samplePayload)
	renderSecs := time.Since(renderStart).Seconds()

	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error en renderizado: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Renderizado completado en: %.3f segundos (%.1f ms)\n", renderSecs, renderSecs*1000)
	fmt.Printf("📦 Tamaño de bytes raster generados: %d bytes (%.1f KB)\n", len(rasterBytes), float64(len(rasterBytes))/1024.0)

	// 2. Fase de Envío a Impresora
	printStart := time.Now()
	p := printer.NewDirectDevicePrinter(devicePath, true)
	err = p.Print(rasterBytes)
	printSecs := time.Since(printStart).Seconds()
	totalSecs := renderSecs + printSecs

	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error enviando a impresora: %v (tiempo: %.3fs)\n", err, printSecs)
		os.Exit(1)
	}

	fmt.Printf("✅ Envío a hardware completado en: %.3f segundos (%.1f ms)\n", printSecs, printSecs*1000)
	fmt.Printf("⏱️ TIEMPO TOTAL DEL PROCESO: %.3f segundos\n", totalSecs)
	fmt.Println("==================================================")
}
