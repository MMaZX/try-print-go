package queue

import (
	"encoding/json"
	"fmt"
	"time"

	"usqay-print-client/internal/escpos"
)

// comandaDoc matches the JSON payload for "comanda" documents.
type comandaDoc struct {
	Mesa  int          `json:"mesa"`
	Items []comandaItem `json:"items"`
}

type comandaItem struct {
	Nombre   string  `json:"nombre"`
	Cantidad int     `json:"cantidad"`
	Precio   float64 `json:"precio,omitempty"`
}

// render converts a JSON payload string to ESC/POS bytes based on document type.
func render(tipoDocumento, payload string) ([]byte, error) {
	switch tipoDocumento {
	case "comanda":
		return renderComanda(payload)
	default:
		return nil, fmt.Errorf("tipo_documento desconocido: %q", tipoDocumento)
	}
}

func renderComanda(payload string) ([]byte, error) {
	var doc comandaDoc
	if err := json.Unmarshal([]byte(payload), &doc); err != nil {
		return nil, fmt.Errorf("parsear payload de comanda: %w", err)
	}

	const width = 32
	now := time.Now()

	b := escpos.New().
		Center().Bold(true).Line("RESTAURANTE USQAY").Bold(false).
		Center().Line(fmt.Sprintf("Mesa #%d", doc.Mesa)).
		Left().Separator(width).
		Left().Line(fmt.Sprintf("Fecha: %s", now.Format("02/01/2006"))).
		Left().Line(fmt.Sprintf("Hora:  %s", now.Format("15:04:05"))).
		Left().Separator(width).
		Bold(true).Line(fmt.Sprintf("%-20s %3s %5s", "PRODUCTO", "CAN", "S/")).Bold(false).
		Separator(width)

	var total float64
	for _, item := range doc.Items {
		subtotal := item.Precio * float64(item.Cantidad)
		total += subtotal
		if item.Precio > 0 {
			b.Line(fmt.Sprintf("%-20s %3d %5.2f", item.Nombre, item.Cantidad, subtotal))
		} else {
			b.Line(fmt.Sprintf("%-20s %3d", item.Nombre, item.Cantidad))
		}
	}

	b.Separator(width)
	if total > 0 {
		b.Right().Bold(true).Line(fmt.Sprintf("TOTAL: S/ %.2f", total)).Bold(false)
	}

	return b.Feed(3).Cut().Bytes(), nil
}
