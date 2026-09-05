package main

import (
	"fmt"
	"os"

	"usqay-print-client/internal/printer"
	"usqay-print-client/internal/queue"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "uso: test_payload.exe <printer_name> <payload.json>")
		os.Exit(1)
	}
	printerName := os.Args[1]
	payloadPath := os.Args[2]

	raw, err := os.ReadFile(payloadPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error leyendo payload: %v\n", err)
		os.Exit(1)
	}

	// prof=nil: se deriva del paper_properties.width del propio payload,
	// igual que hace el agente cuando el servidor no manda perfil explícito.
	data, err := queue.RenderThermal(nil, string(raw))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error renderizando: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("bytes=%d\n", len(data))

	p := printer.NewDirectDevicePrinter(printerName, true)
	if err := p.Print(data); err != nil {
		fmt.Fprintf(os.Stderr, "error imprimiendo: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("OK")
}
