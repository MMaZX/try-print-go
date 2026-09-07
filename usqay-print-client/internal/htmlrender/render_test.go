package htmlrender

import (
	"strings"
	"testing"
)

func TestFindBrowserExecutable(t *testing.T) {
	path, err := FindBrowserExecutable()
	if err != nil {
		t.Skipf("No hay navegador Chromium disponible en este entorno: %v", err)
	}
	if path == "" {
		t.Fatalf("se esperaba una ruta de navegador no vacía")
	}
	t.Logf("Navegador Chromium encontrado en: %s", path)
}

func TestBuildHTMLFromJSON(t *testing.T) {
	sampleJSON := `{
		"options": { "cut": true, "drawer": false },
		"paper_properties": { "width": 80.0, "padding": [2.0, 2.0, 2.0, 2.0] },
		"body": [
			{ "type": "text", "value": "USQAY RESTOBAR", "align": "center", "bold": true, "size": "medium" },
			{ "type": "separator", "character": "=" },
			{ "type": "columns", "columns": [
				{ "text": "Mesa: 05", "width": 50, "align": "left" },
				{ "text": "Mozo: Carlos", "width": 50, "align": "right" }
			]},
			{ "type": "separator", "character": "-" },
			{ "type": "table", "columns": [
				{ "header": "Cant", "width": 20, "align": "left" },
				{ "header": "Descripción", "width": 50, "align": "left" },
				{ "header": "Total", "width": 30, "align": "right" }
			], "rows": [
				{ "cells": [
					{ "text": "2" },
					{ "text": "Hamburguesa Clásica con Queso" },
					{ "text": "50.00" }
				]}
			]},
			{ "type": "spacer", "lines": 1 },
			{ "type": "qr", "value": "https://usqay-pos.com/comprobante/123", "align": "center", "size": 4 },
			{ "type": "barcode", "value": "775123456789", "align": "center", "height": 50, "width": 2, "hri": "below" }
		]
	}`

	htmlContent, widthDots, payload, err := BuildHTMLFromJSON(nil, sampleJSON)
	if err != nil {
		t.Fatalf("BuildHTMLFromJSON falló: %v", err)
	}

	if widthDots != 576 {
		t.Errorf("esperado widthDots=576 para papel 80mm, obtenido %d", widthDots)
	}
	if !payload.Options.Cut {
		t.Errorf("esperado cut=true")
	}
	if !strings.Contains(htmlContent, "USQAY RESTOBAR") {
		t.Errorf("HTML no contiene el título del ticket")
	}
	if !strings.Contains(htmlContent, "Hamburguesa Clásica con Queso") {
		t.Errorf("HTML no contiene el ítem de la tabla")
	}
	if !strings.Contains(htmlContent, "data:image/png;base64,") {
		t.Errorf("HTML no contiene la imagen en base64 generada para el QR")
	}
	if !strings.Contains(htmlContent, "<svg") {
		t.Errorf("HTML no contiene el SVG generado para el Barcode")
	}
}

func TestRenderThermalIntegration(t *testing.T) {
	_, err := FindBrowserExecutable()
	if err != nil {
		t.Skipf("Omitiendo test de integración con navegador: %v", err)
	}

	sampleJSON := `{
		"options": { "cut": true, "drawer": true },
		"paper_properties": { "width": 58.0 },
		"body": [
			{ "type": "text", "value": "TEST TICKET", "align": "center", "bold": true, "size": "normal" },
			{ "type": "separator", "character": "-" },
			{ "type": "text", "value": "Total: S/ 10.00", "align": "right", "bold": true, "size": "medium" }
		]
	}`

	data, err := RenderThermal(nil, sampleJSON)
	if err != nil {
		t.Fatalf("RenderThermal falló: %v", err)
	}

	if len(data) == 0 {
		t.Fatalf("se esperaban bytes ESC/POS no vacíos")
	}

	t.Logf("Bytes ESC/POS generados exitosamente: %d bytes", len(data))
}
