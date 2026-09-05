package queue

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"usqay-print-client/internal/printer"
)

// --- helpers de test ---

func mustRenderText(t *testing.T, payload string) string {
	t.Helper()
	b, err := renderText(nil, payload)
	if err != nil {
		t.Fatalf("renderText falló: %v", err)
	}
	return string(b)
}

func mustRenderThermal(t *testing.T, payload string) []byte {
	t.Helper()
	b, err := RenderThermal(nil, payload)
	if err != nil {
		t.Fatalf("RenderThermal falló: %v", err)
	}
	return b
}

func assertContains(t *testing.T, text, want string) {
	t.Helper()
	if !strings.Contains(text, want) {
		t.Errorf("esperaba %q en la salida\ngot: %q", want, text)
	}
}

// --- Bloque text ---

func TestRenderText(t *testing.T) {
	payload := `{
		"options": {"cut": false, "drawer": false},
		"margins": {"ancho_dimension": 80.0, "altura_dimension": 0.0},
		"body": [
			{"type": "text", "value": "TICKET DE PRUEBA", "align": "center", "bold": true, "size": "double"},
			{"type": "text", "value": "línea izquierda"},
			{"type": "text", "value": "derecha", "align": "right"}
		]
	}`

	text := mustRenderText(t, payload)
	assertContains(t, text, "TICKET DE PRUEBA")
	assertContains(t, text, "línea izquierda")
	assertContains(t, text, "derecha")

	data := mustRenderThermal(t, payload)
	if len(data) == 0 {
		t.Fatal("se esperaba output ESC/POS no vacío")
	}
}

// --- Bloque separator y spacer ---

func TestRenderSeparatorSpacer(t *testing.T) {
	payload := `{
		"options": {"cut": false, "drawer": false},
		"margins": {"ancho_dimension": 80.0, "altura_dimension": 0.0},
		"body": [
			{"type": "separator", "character": "="},
			{"type": "spacer", "lines": 2},
			{"type": "separator"}
		]
	}`

	text := mustRenderText(t, payload)
	assertContains(t, text, "================================================")
	assertContains(t, text, "------------------------------------------------")
}

// --- Bloque table: con columnas y encabezados ---

func TestRenderTableWithColumns(t *testing.T) {
	payload := `{
		"options": {"cut": true, "drawer": false},
		"margins": {"ancho_dimension": 80.0, "altura_dimension": 0.0},
		"body": [
			{
				"type": "table",
				"columns": [
					{"header": "CANT",     "width": 0.10, "align": "left"},
					{"header": "PRODUCTO", "width": 0.65, "align": "left"},
					{"header": "TOTAL",    "width": 0.25, "align": "right"}
				],
				"rows": [
					{"cells": [{"text": "2"}, {"text": "LOMO SALTADO"},   {"text": "S/ 42.00"}]},
					{"cells": [{"text": "1"}, {"text": "COCA COLA ZERO"}, {"text": "S/ 5.00"}]},
					{
						"bold": true,
						"cells": [
							{"text": ""},
							{"text": "Total a pagar:"},
							{"text": "S/ 47.00", "bold": true}
						]
					}
				]
			}
		]
	}`

	text := mustRenderText(t, payload)
	assertContains(t, text, "CANT")
	assertContains(t, text, "PRODUCTO")
	assertContains(t, text, "TOTAL")
	assertContains(t, text, "LOMO SALTADO")
	assertContains(t, text, "S/ 42.00")
	assertContains(t, text, "COCA COLA ZERO")
	assertContains(t, text, "Total a pagar:")
	assertContains(t, text, "S/ 47.00")

	data := mustRenderThermal(t, payload)
	if len(data) == 0 {
		t.Fatal("se esperaba output ESC/POS no vacío")
	}
}

// --- Bloque table: modo automático sin columnas (reemplaza key_value) ---

func TestRenderTableAutoColumns(t *testing.T) {
	payload := `{
		"options": {"cut": false, "drawer": false},
		"margins": {"ancho_dimension": 80.0, "altura_dimension": 0.0},
		"body": [
			{
				"type": "table",
				"columns": null,
				"rows": [
					{"cells": [{"text": "Subtotal:"},  {"text": "S/ 42.00"}]},
					{"cells": [{"text": "IGV (18%):"}, {"text": "S/ 7.56"}]},
					{"bold": true, "cells": [{"text": "Total:"}, {"text": "S/ 49.56"}]}
				]
			}
		]
	}`

	text := mustRenderText(t, payload)
	assertContains(t, text, "Subtotal:")
	assertContains(t, text, "S/ 42.00")
	assertContains(t, text, "IGV (18%):")
	assertContains(t, text, "Total:")
	assertContains(t, text, "S/ 49.56")
}

// --- Bloque table: merge:true (comanda con notas) ---

func TestRenderTableMerge(t *testing.T) {
	payload := `{
		"options": {"cut": true, "drawer": false},
		"margins": {"ancho_dimension": 58.0, "altura_dimension": 0.0},
		"body": [
			{"type": "text", "value": "** COMANDA **", "align": "center", "bold": true},
			{
				"type": "table",
				"columns": [
					{"header": "CANT",     "width": 0.15, "align": "left"},
					{"header": "PRODUCTO", "width": 0.85, "align": "left"}
				],
				"rows": [
					{"cells": [{"text": "2"}, {"text": "ARROZ CHAUFA"}]},
					{"merge": true, "cells": [{"text": "↳ Bien tostado, sin cebollita"}]},
					{"cells": [{"text": "1"}, {"text": "LOMO SALTADO"}]}
				]
			}
		]
	}`

	text := mustRenderText(t, payload)
	assertContains(t, text, "** COMANDA **")
	assertContains(t, text, "ARROZ CHAUFA")
	assertContains(t, text, "↳ Bien tostado, sin cebollita")
	assertContains(t, text, "LOMO SALTADO")

	data := mustRenderThermal(t, payload)
	if len(data) == 0 {
		t.Fatal("se esperaba output ESC/POS no vacío")
	}
}

// --- Bloque columns ---

func TestRenderColumns(t *testing.T) {
	payload := `{
		"options": {"cut": false, "drawer": false},
		"margins": {"ancho_dimension": 80.0, "altura_dimension": 0.0},
		"body": [
			{
				"type": "columns",
				"columns": [
					{"text": "Caja #1",          "width": 0.5, "align": "left"},
					{"text": "12/06/2026 14:30", "width": 0.5, "align": "right"}
				]
			},
			{
				"type": "columns",
				"columns": [
					{"text": "Mozo:",      "width": 0.3, "align": "left"},
					{"text": "Juan Pérez", "width": 0.7, "align": "left", "bold": true}
				]
			}
		]
	}`

	text := mustRenderText(t, payload)
	assertContains(t, text, "Caja #1")
	assertContains(t, text, "12/06/2026 14:30")
	assertContains(t, text, "Mozo:")
	assertContains(t, text, "Juan Pérez")

	data := mustRenderThermal(t, payload)
	if len(data) == 0 {
		t.Fatal("se esperaba output ESC/POS no vacío")
	}
}

// --- Bloque qr ---

func TestRenderQR(t *testing.T) {
	payload := `{
		"options": {"cut": false, "drawer": false},
		"margins": {"ancho_dimension": 80.0, "altura_dimension": 0.0},
		"body": [
			{"type": "qr", "value": "https://app.usqay.com/c/abc123", "align": "center", "size": 6}
		]
	}`

	text := mustRenderText(t, payload)
	assertContains(t, text, "[QR: https://app.usqay.com/c/abc123]")

	data := mustRenderThermal(t, payload)
	if len(data) == 0 {
		t.Fatal("se esperaba output ESC/POS no vacío")
	}
}

// --- Bloque barcode ---

func TestRenderBarcode(t *testing.T) {
	payload := `{
		"options": {"cut": false, "drawer": false},
		"margins": {"ancho_dimension": 80.0, "altura_dimension": 0.0},
		"body": [
			{
				"type": "barcode",
				"symbology": "CODE128",
				"value": "B001-00000042",
				"align": "center",
				"height": 80,
				"hri": "below"
			}
		]
	}`

	text := mustRenderText(t, payload)
	assertContains(t, text, "[BARCODE CODE128: B001-00000042]")

	data := mustRenderThermal(t, payload)
	if len(data) == 0 {
		t.Fatal("se esperaba output ESC/POS no vacío")
	}
}

// --- Ticket completo: precuenta ---

func TestRenderTicketPrecuenta(t *testing.T) {
	payload := `{
		"options": {"cut": true, "drawer": true},
		"margins": {"ancho_dimension": 80.0, "altura_dimension": 0.0},
		"body": [
			{"type": "text", "value": "RESTAURANTE USQAY", "align": "center", "bold": true, "size": "double"},
			{"type": "separator", "character": "="},
			{
				"type": "columns",
				"columns": [
					{"text": "Mesa: 5",          "width": 0.5, "align": "left"},
					{"text": "12/06/2026 14:30", "width": 0.5, "align": "right"}
				]
			},
			{"type": "separator"},
			{
				"type": "table",
				"columns": [
					{"header": "CANT",     "width": 0.10, "align": "left"},
					{"header": "PRODUCTO", "width": 0.65, "align": "left"},
					{"header": "TOTAL",    "width": 0.25, "align": "right"}
				],
				"rows": [
					{"cells": [{"text": "2"}, {"text": "LOMO SALTADO"},   {"text": "S/ 42.00"}]},
					{"cells": [{"text": "1"}, {"text": "COCA COLA ZERO"}, {"text": "S/ 5.00"}]}
				]
			},
			{"type": "separator"},
			{
				"type": "table",
				"columns": null,
				"rows": [
					{"cells": [{"text": "Subtotal:"},  {"text": "S/ 47.00"}]},
					{"cells": [{"text": "IGV (18%):"}, {"text": "S/ 8.46"}]},
					{
						"bold": true,
						"cells": [{"text": "TOTAL:"}, {"text": "S/ 55.46", "size": "double"}]
					}
				]
			},
			{"type": "qr", "value": "https://app.usqay.com/c/abc123", "align": "center"}
		]
	}`

	text := mustRenderText(t, payload)
	assertContains(t, text, "RESTAURANTE USQAY")
	assertContains(t, text, "Mesa: 5")
	assertContains(t, text, "LOMO SALTADO")
	assertContains(t, text, "S/ 42.00")
	assertContains(t, text, "COCA COLA ZERO")
	assertContains(t, text, "Subtotal:")
	assertContains(t, text, "TOTAL:")
	assertContains(t, text, "S/ 55.46")

	data := mustRenderThermal(t, payload)
	if len(data) == 0 {
		t.Fatal("se esperaba output ESC/POS no vacío para ticket precuenta completo")
	}
}

// --- Helpers de layout ---

func TestFormatCol(t *testing.T) {
	tests := []struct {
		text     string
		width    int
		align    string
		expected string
	}{
		{"hola", 8, "left", "hola    "},
		{"hola", 8, "right", "    hola"},
		{"hola", 8, "center", "  hola  "},
		{"truncado", 4, "left", "trun"},
		{"", 5, "left", "     "},
	}
	for _, tc := range tests {
		got := formatCol(tc.text, tc.width, tc.align)
		if got != tc.expected {
			t.Errorf("formatCol(%q, %d, %q) = %q, want %q", tc.text, tc.width, tc.align, got, tc.expected)
		}
	}
}

func TestResolveTableLayoutAuto(t *testing.T) {
	block := TableBlock{
		Columns: nil,
		Rows: []TableRow{
			{Cells: []TableCell{{Text: "A"}, {Text: "B"}, {Text: "C"}}},
		},
	}
	widths, aligns := resolveTableLayout(block, 48)
	if len(widths) != 3 {
		t.Fatalf("esperaba 3 columnas, got %d", len(widths))
	}
	total := 0
	for _, w := range widths {
		total += w
	}
	if total != 48 {
		t.Errorf("suma de anchos = %d, want 48", total)
	}
	for i, a := range aligns {
		if a != "left" {
			t.Errorf("aligns[%d] = %q, want 'left'", i, a)
		}
	}
}

func TestResolveTableLayoutWithColumns(t *testing.T) {
	header := "TOTAL"
	block := TableBlock{
		Columns: []TableColumn{
			{Width: 0.10, Align: "left"},
			{Width: 0.65, Align: "left"},
			{Header: &header, Width: 0.25, Align: "right"},
		},
	}
	widths, aligns := resolveTableLayout(block, 48)
	if len(widths) != 3 {
		t.Fatalf("esperaba 3 columnas, got %d", len(widths))
	}
	total := 0
	for _, w := range widths {
		total += w
	}
	if total != 48 {
		t.Errorf("suma de anchos = %d, want 48", total)
	}
	if aligns[2] != "right" {
		t.Errorf("aligns[2] = %q, want 'right'", aligns[2])
	}
}

func TestPageWidth(t *testing.T) {
	tests := []struct {
		dim      float64
		expected int
	}{
		{80.0, 48},
		{58.0, 32},
		{0.0, 32},   // sin configurar: fallback seguro al ancho más angosto conocido (58mm)
		{70.0, 40},  // valor intermedio personalizado: interpolado, no un bucket fijo
		{62.0, 34},  // otro valor intermedio, distinto de 70mm
		{100.0, 62}, // fuera de rango: se extrapola con la misma pendiente
	}
	for _, tc := range tests {
		got := pageWidth(tc.dim)
		if got != tc.expected {
			t.Errorf("pageWidth(%v) = %d, want %d", tc.dim, got, tc.expected)
		}
	}
}

func TestPaperGeometryDots(t *testing.T) {
	tests := []struct {
		dim      float64
		wantDots int
	}{
		{58.0, 384},
		{80.0, 576},
		{70.0, 489}, // interpolado entre las dos anclas: distinto de 58 y de 80
		{62.0, 419},
	}
	for _, tc := range tests {
		dots, _ := paperGeometry(tc.dim)
		if dots != tc.wantDots {
			t.Errorf("paperGeometry(%v).dots = %d, want %d", tc.dim, dots, tc.wantDots)
		}
	}
}

// TestRenderStructuredEmitsPrintAreaWidth verifica que el comando físico
// GS L/GS W se emita con el ancho en dots correspondiente al ancho_dimension
// recibido, incluyendo valores personalizados que no son 58 ni 80mm exactos,
// para que la impresora respete realmente el ancho configurado en el JSON.
func TestRenderStructuredEmitsPrintAreaWidth(t *testing.T) {
	tests := []struct {
		name      string
		anchoDim  float64
		wantBytes []byte
	}{
		{"58mm", 58.0, []byte{0x1D, 0x4C, 0x00, 0x00, 0x1D, 0x57, 0x80, 0x01}},        // 384 = 0x0180
		{"80mm", 80.0, []byte{0x1D, 0x4C, 0x00, 0x00, 0x1D, 0x57, 0x40, 0x02}},        // 576 = 0x0240
		{"70mm custom", 70.0, []byte{0x1D, 0x4C, 0x00, 0x00, 0x1D, 0x57, 0xE9, 0x01}}, // 489 = 0x01E9
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload := fmt.Sprintf(`{
				"options": {"cut": false, "drawer": false},
				"margins": {"ancho_dimension": %v, "altura_dimension": 0.0},
				"body": [{"type": "text", "value": "x"}]
			}`, tc.anchoDim)

			data := mustRenderThermal(t, payload)
			if !bytes.Contains(data, tc.wantBytes) {
				t.Errorf("esperaba encontrar comando GS L/GS W %x en la salida, got %x", tc.wantBytes, data)
			}
		})
	}
}

func TestRenderStructuredCentersNarrowProfile(t *testing.T) {
	profile := &printer.DeviceProfile{
		WidthDots:         384, // 58mm
		CharWidthDots:     12,
		SupportsPrintArea: true,
	}
	// Payload asks for 80mm paper (576 dots)
	payload := `{
		"options": {"cut": false, "drawer": false},
		"margins": {"ancho_dimension": 80.0, "altura_dimension": 0.0},
		"body": [{"type": "text", "value": "x"}]
	}`
	data, err := RenderThermal(profile, payload)
	if err != nil {
		t.Fatalf("RenderThermal falló: %v", err)
	}
	// leftMarginDots = (576 - 384) / 2 = 96 dots = 0x60, 0x00
	// printWidthDots = 384 dots = 0x80, 0x01
	wantBytes := []byte{0x1D, 0x4C, 0x60, 0x00, 0x1D, 0x57, 0x80, 0x01}
	if !bytes.Contains(data, wantBytes) {
		t.Errorf("esperaba encontrar comandos de centrado %x en la salida, got %x", wantBytes, data)
	}
}

// --- Error handling ---

func TestRenderInvalidPayload(t *testing.T) {
	_, err := RenderThermal(nil, "esto no es json")
	if err == nil {
		t.Error("esperaba error para payload inválido")
	}
}

func TestRenderEmptyBody(t *testing.T) {
	payload := `{"options": {"cut": false, "drawer": false}, "margins": {"ancho_dimension": 80}, "body": []}`
	_, err := RenderThermal(nil, payload)
	if err == nil {
		t.Error("esperaba error para body vacío")
	}
}

func TestRenderUnknownBlockSkipped(t *testing.T) {
	payload := `{
		"options": {"cut": false, "drawer": false},
		"margins": {"ancho_dimension": 80.0, "altura_dimension": 0.0},
		"body": [
			{"type": "unknown_block", "foo": "bar"},
			{"type": "text", "value": "visible"}
		]
	}`
	text := mustRenderText(t, payload)
	assertContains(t, text, "visible")
}

func TestFormatColCuentaRunas(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		colWidth int
		align    string
		want     string
	}{
		{"tilde no desalinea a la izquierda", "Café", 8, "left", "Café    "},
		{"tilde no desalinea a la derecha", "Café", 8, "right", "    Café"},
		{"tilde no desalinea centrado", "Añejo", 9, "center", "  Añejo  "},
		{"truncado no parte runa multibyte", "Añejo", 3, "left", "Añe"},
		{"ascii intacto", "Total", 7, "left", "Total  "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatCol(tc.text, tc.colWidth, tc.align); got != tc.want {
				t.Errorf("formatCol(%q, %d, %q) = %q, want %q", tc.text, tc.colWidth, tc.align, got, tc.want)
			}
		})
	}
}

func TestApplyAlignTextCuentaRunas(t *testing.T) {
	// "Café" son 4 runas: centrado en 10 debe dejar 3 espacios, no 2.
	if got := applyAlignText("Café", "center", 10); got != "   Café" {
		t.Errorf("center = %q, want %q", got, "   Café")
	}
	if got := applyAlignText("Café", "right", 10); got != "      Café" {
		t.Errorf("right = %q, want %q", got, "      Café")
	}
}

func TestGoldenSuite(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		profile *printer.DeviceProfile
	}{
		{
			name: "ticket_80_no_padding",
			payload: `{
				"options": {"cut": true, "drawer": true},
				"margins": {"ancho_dimension": 80.0, "altura_dimension": 0.0, "padding": 0.0},
				"body": [
					{"type": "text", "value": "RESTAURANTE USQAY", "align": "center", "bold": true, "size": "double"},
					{"type": "separator", "character": "="},
					{
						"type": "columns",
						"columns": [
							{"text": "Mesa: 5",          "width": 0.5, "align": "left"},
							{"text": "12/06/2026 14:30", "width": 0.5, "align": "right"}
						]
					},
					{"type": "separator"},
					{
						"type": "table",
						"columns": [
							{"header": "CANT",     "width": 0.10, "align": "left"},
							{"header": "PRODUCTO", "width": 0.65, "align": "left"},
							{"header": "TOTAL",    "width": 0.25, "align": "right"}
						],
						"rows": [
							{"cells": [{"text": "2"}, {"text": "LOMO SALTADO"},   {"text": "S/ 42.00"}]},
							{"cells": [{"text": "1"}, {"text": "COCA COLA ZERO"}, {"text": "S/ 5.00"}]}
						]
					},
					{"type": "separator"},
					{
						"type": "table",
						"columns": null,
						"rows": [
							{"cells": [{"text": "Subtotal:"},  {"text": "S/ 47.00"}]},
							{"cells": [{"text": "IGV (18%):"}, {"text": "S/ 8.46"}]},
							{
								"bold": true,
								"cells": [{"text": "TOTAL:"}, {"text": "S/ 55.46", "size": "double"}]
							}
						]
					},
					{"type": "qr", "value": "https://app.usqay.com/c/abc123", "align": "center"}
				]
			}`,
		},
		{
			name: "ticket_80_with_padding",
			payload: `{
				"options": {"cut": true, "drawer": true},
				"margins": {"ancho_dimension": 80.0, "altura_dimension": 0.0, "padding": 1.5},
				"body": [
					{"type": "text", "value": "RESTAURANTE USQAY", "align": "center", "bold": true, "size": "double"},
					{"type": "separator", "character": "="},
					{
						"type": "columns",
						"columns": [
							{"text": "Mesa: 5",          "width": 0.5, "align": "left"},
							{"text": "12/06/2026 14:30", "width": 0.5, "align": "right"}
						]
					},
					{"type": "separator"},
					{
						"type": "table",
						"columns": [
							{"header": "CANT",     "width": 0.10, "align": "left"},
							{"header": "PRODUCTO", "width": 0.65, "align": "left"},
							{"header": "TOTAL",    "width": 0.25, "align": "right"}
						],
						"rows": [
							{"cells": [{"text": "2"}, {"text": "LOMO SALTADO"},   {"text": "S/ 42.00"}]},
							{"cells": [{"text": "1"}, {"text": "COCA COLA ZERO"}, {"text": "S/ 5.00"}]}
						]
					}
				]
			}`,
		},
		{
			name: "ticket_58_no_padding",
			payload: `{
				"options": {"cut": true, "drawer": false},
				"margins": {"ancho_dimension": 58.0, "altura_dimension": 0.0, "padding": 0.0},
				"body": [
					{"type": "text", "value": "** COMANDA **", "align": "center", "bold": true},
					{
						"type": "table",
						"columns": [
							{"header": "CANT",     "width": 0.15, "align": "left"},
							{"header": "PRODUCTO", "width": 0.85, "align": "left"}
						],
						"rows": [
							{"cells": [{"text": "2"}, {"text": "ARROZ CHAUFA"}]},
							{"merge": true, "cells": [{"text": "↳ Bien tostado, sin cebollita"}]},
							{"cells": [{"text": "1"}, {"text": "LOMO SALTADO"}]}
						]
					}
				]
			}`,
		},
		{
			name: "ticket_58_with_padding",
			payload: `{
				"options": {"cut": true, "drawer": false},
				"margins": {"ancho_dimension": 58.0, "altura_dimension": 0.0, "padding": 0.5},
				"body": [
					{"type": "text", "value": "** COMANDA **", "align": "center", "bold": true},
					{
						"type": "table",
						"columns": [
							{"header": "CANT",     "width": 0.15, "align": "left"},
							{"header": "PRODUCTO", "width": 0.85, "align": "left"}
						],
						"rows": [
							{"cells": [{"text": "2"}, {"text": "ARROZ CHAUFA"}]},
							{"merge": true, "cells": [{"text": "↳ Bien tostado, sin cebollita"}]},
							{"cells": [{"text": "1"}, {"text": "LOMO SALTADO"}]}
						]
					}
				]
			}`,
		},
		{
			name: "ticket_70_custom",
			payload: `{
				"options": {"cut": false, "drawer": false},
				"margins": {"ancho_dimension": 70.0, "altura_dimension": 0.0, "padding": 0.0},
				"body": [
					{"type": "text", "value": "TICKET CUSTOM 70MM", "align": "center"},
					{"type": "barcode", "symbology": "CODE128", "value": "B001-00000042", "align": "center", "height": 80, "hri": "below"}
				]
			}`,
		},
		{
			name: "ticket_58_profile_on_80_paper",
			profile: &printer.DeviceProfile{
				WidthDots:         384,
				DPI:               203,
				CharWidthDots:     12,
				SupportsCut:       true,
				SupportsDrawer:    true,
				SupportsQRNative:  true,
				SupportsPrintArea: true,
				SupportsRaster:    true,
			},
			payload: `{
				"options": {"cut": true, "drawer": true},
				"margins": {"ancho_dimension": 80.0, "altura_dimension": 0.0, "padding": 0.0},
				"body": [
					{"type": "text", "value": "RESTAURANTE USQAY", "align": "center", "bold": true, "size": "double"},
					{"type": "separator", "character": "="},
					{
						"type": "columns",
						"columns": [
							{"text": "Mesa: 5",          "width": 0.5, "align": "left"},
							{"text": "12/06/2026 14:30", "width": 0.5, "align": "right"}
						]
					}
				]
			}`,
		},
	}

	updateGolden := os.Getenv("UPDATE_GOLDEN") == "true"
	goldensDir := filepath.Join("testdata", "goldens")

	if updateGolden {
		if err := os.MkdirAll(goldensDir, 0755); err != nil {
			t.Fatalf("error creando goldensDir: %v", err)
		}
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotBytes, err := RenderThermal(tc.profile, tc.payload)
			if err != nil {
				t.Fatalf("RenderThermal falló: %v", err)
			}

			goldenPath := filepath.Join(goldensDir, tc.name+".prn")

			if updateGolden {
				if err := os.WriteFile(goldenPath, gotBytes, 0644); err != nil {
					t.Fatalf("error guardando golden file %s: %v", goldenPath, err)
				}
				t.Logf("golden file actualizado: %s", goldenPath)
				return
			}

			wantBytes, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("error leyendo golden file %s (¿necesitas correr con UPDATE_GOLDEN=true?): %v", goldenPath, err)
			}

			if !bytes.Equal(gotBytes, wantBytes) {
				t.Errorf("diferencia de bytes detectada respecto a golden file %s\ngot %d bytes, want %d bytes", goldenPath, len(gotBytes), len(wantBytes))
			}
		})
	}
}

func TestRenderStructuredImage(t *testing.T) {
	// Dynamically generate a 1x1 black PNG image
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.Black)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("error encoding png: %v", err)
	}
	b64Image := base64.StdEncoding.EncodeToString(buf.Bytes())

	profile := &printer.DeviceProfile{
		WidthDots:      384,
		CharWidthDots:  12,
		SupportsRaster: true,
	}

	payload := fmt.Sprintf(`{
		"options": {"cut": false, "drawer": false},
		"margins": {"ancho_dimension": 58.0, "altura_dimension": 0.0},
		"body": [
			{
				"type": "image",
				"data": "%s",
				"align": "center",
				"width": 8
			}
		]
	}`, b64Image)

	data, err := RenderThermal(profile, payload)
	if err != nil {
		t.Fatalf("RenderThermal falló: %v", err)
	}

	// El ticket entero (incluida la imagen) se compone en un único lienzo y
	// se emite como una o más tiras GS v 0 (ver flushTicketImage/
	// writeRasterChunked) — ya no hay un comando aislado del tamaño exacto
	// de la imagen fuente, así que solo verificamos que el comando raster
	// aparezca y que el render no haya fallado.
	gsV0 := []byte{0x1D, 0x76, 0x30}
	if !bytes.Contains(data, gsV0) {
		t.Errorf("esperaba encontrar comando GS v 0 (%x) en la salida, got %x", gsV0, data)
	}
	if len(data) == 0 {
		t.Fatal("se esperaba output no vacío")
	}
}
