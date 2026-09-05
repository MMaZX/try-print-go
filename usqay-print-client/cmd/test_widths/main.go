package main

import (
	"fmt"
	"os"
	"strconv"

	"usqay-print-client/internal/printer"
	"usqay-print-client/internal/queue"
)

const payloadTemplate = `{
  "options": {"cut": true, "drawer": false},
  "margins": {"dimension_papel": %v, "altura_dimension": 0.0, "padding": 0.0},
  "body": [
    {"type": "text", "value": "ANCHO PAPEL: %v mm", "align": "center", "bold": true, "size": "medium"},
    {"type": "separator", "character": "="},
    {"type": "text", "value": "Esta linea debe llegar justo al borde derecho del papel ->|", "align": "left"},
    {"type": "separator", "character": "-"},
    {
      "type": "columns",
      "columns": [
        {"text": "IZQUIERDA", "width": 0.5, "align": "left"},
        {"text": "DERECHA", "width": 0.5, "align": "right"}
      ]
    },
    {"type": "separator"},
    {
      "type": "table",
      "columns": [
        {"header": "CANT", "width": 0.15, "align": "left"},
        {"header": "PRODUCTO", "width": 0.60, "align": "left"},
        {"header": "TOTAL", "width": 0.25, "align": "right"}
      ],
      "rows": [
        {"cells": [{"text": "2"}, {"text": "PRUEBA ANCHO"}, {"text": "S/ 10.00"}]}
      ]
    },
    {"type": "spacer", "lines": 1}
  ]
}`

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "uso: test_widths.exe <printer_name> <ancho_mm>")
		os.Exit(1)
	}
	printerName := os.Args[1]
	widthMM, err := strconv.ParseFloat(os.Args[2], 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ancho inválido: %v\n", err)
		os.Exit(1)
	}

	payload := fmt.Sprintf(payloadTemplate, widthMM, widthMM)

	// prof=nil: fuerza a RenderThermal a derivar el perfil desde
	// margins.dimension_papel (printer.DeriveProfileFromWidth), que es
	// exactamente lo que hace hoy el agente cuando el servidor no manda
	// perfil explícito de la impresora.
	data, err := queue.RenderThermal(nil, payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error renderizando: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("ancho=%vmm bytes=%d\n", widthMM, len(data))

	p := printer.NewDirectDevicePrinter(printerName, true)
	if err := p.Print(data); err != nil {
		fmt.Fprintf(os.Stderr, "error imprimiendo: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("OK")
}
