package queue

import (
	"strings"
	"testing"
)

func TestRenderLegacyComanda(t *testing.T) {
	legacyPayload := `{
		"mesa": 5,
		"items": [
			{"nombre": "LOMO SALTADO", "cantidad": 1, "precio": 35.50},
			{"nombre": "ARROZ CHAUFA", "cantidad": 2, "precio": 0.00}
		]
	}`

	data, err := render("comanda", legacyPayload)
	if err != nil {
		t.Fatalf("Error rendering legacy comanda: %v", err)
	}

	text := string(data)
	if !strings.Contains(text, "Mesa #5") {
		t.Errorf("Expected 'Mesa #5', got: %q", text)
	}
	if !strings.Contains(text, "LOMO SALTADO") {
		t.Errorf("Expected 'LOMO SALTADO' in output, got: %q", text)
	}
	if !strings.Contains(text, "ARROZ CHAUFA") {
		t.Errorf("Expected 'ARROZ CHAUFA' in output, got: %q", text)
	}
}

func TestRenderStructuredComanda(t *testing.T) {
	structuredPayload := `{
		"options": {
			"cut": true,
			"drawer": false
		},
		"margins": {
			"ancho_dimension": 80.0,
			"altura_dimension": 0.0
		},
		"body": [
			{
				"type": "text",
				"value": "** COMANDA **",
				"align": "center",
				"bold": true,
				"size": "double"
			},
			{
				"type": "separator",
				"character": "="
			},
			{
				"type": "text",
				"value": "PEDIDO #1",
				"align": "left",
				"bold": true
			},
			{
				"type": "table_comanda",
				"rows": [
					{
						"cant": 1,
						"producto": "LOMO SALTADO",
						"notas": "Término medio",
						"categoria_id": 1
					},
					{
						"cant": 2,
						"producto": "ARROZ CHAUFA",
						"notas": null,
						"categoria_id": 1
					}
				]
			},
			{
				"type": "separator"
			},
			{
				"type": "qr",
				"value": "https://usqay.com/ticket/1"
			}
		]
	}`

	// Test ESC/POS rendering
	data, err := render("comanda", structuredPayload)
	if err != nil {
		t.Fatalf("Error rendering structured comanda (escpos): %v", err)
	}
	if len(data) == 0 {
		t.Fatal("Expected non-empty ESC/POS byte array")
	}

	// Test text rendering (easier to assertions on strings)
	textBytes, err := renderText("comanda", structuredPayload)
	if err != nil {
		t.Fatalf("Error rendering structured comanda (text): %v", err)
	}
	text := string(textBytes)

	// Assertions
	if !strings.Contains(text, "** COMANDA **") {
		t.Errorf("Expected header '** COMANDA **', got: %q", text)
	}
	if !strings.Contains(text, "PEDIDO #1") {
		t.Errorf("Expected 'PEDIDO #1', got: %q", text)
	}
	if !strings.Contains(text, "1   LOMO SALTADO") && !strings.Contains(text, "1    LOMO SALTADO") {
		t.Errorf("Expected item 'LOMO SALTADO' with qty, got: %q", text)
	}
	if !strings.Contains(text, "* Término medio") {
		t.Errorf("Expected observation '* Término medio', got: %q", text)
	}
	if !strings.Contains(text, "2   ARROZ CHAUFA") && !strings.Contains(text, "2    ARROZ CHAUFA") {
		t.Errorf("Expected item 'ARROZ CHAUFA' with qty, got: %q", text)
	}
	if !strings.Contains(text, "[QR CODE: https://usqay.com/ticket/1]") {
		t.Errorf("Expected QR code URL, got: %q", text)
	}
}

func TestRenderStructuredTablePrecuenta(t *testing.T) {
	structuredPayload := `{
		"options": {
			"cut": true,
			"drawer": true
		},
		"margins": {
			"ancho_dimension": 80.0,
			"altura_dimension": 0.0
		},
		"body": [
			{
				"type": "text",
				"value": "PRE-CUENTA",
				"align": "center",
				"bold": true,
				"size": "medium"
			},
			{
				"type": "separator",
				"character": "-"
			},
			{
				"type": "table",
				"headers": ["CANT", "PRODUCTO", "TOTAL"],
				"widths": [0.1, 0.65, 0.25],
				"aligns": ["left", "left", "right"],
				"rows": [
					{
						"cant": 1,
						"producto": "LOMO SALTADO",
						"total": "S/ 35.50",
						"categoria_id": 1
					},
					{
						"cant": 2,
						"producto": "COCA COLA ZERO",
						"total": "S/ 10.00",
						"categoria_id": 2
					}
				]
			},
			{
				"type": "separator",
				"character": "="
			}
		]
	}`

	textBytes, err := renderText("precuenta", structuredPayload)
	if err != nil {
		t.Fatalf("Error rendering structured table (text): %v", err)
	}
	text := string(textBytes)

	// Verify columns format
	if !strings.Contains(text, "CANT") || !strings.Contains(text, "PRODUCTO") || !strings.Contains(text, "TOTAL") {
		t.Errorf("Expected table headers in output, got: %q", text)
	}
	if !strings.Contains(text, "LOMO SALTADO") || !strings.Contains(text, "S/ 35.50") {
		t.Errorf("Expected table row contents in output, got: %q", text)
	}
	if !strings.Contains(text, "COCA COLA ZERO") || !strings.Contains(text, "S/ 10.00") {
		t.Errorf("Expected table row contents in output, got: %q", text)
	}
}

func TestGetRowValueSemantics(t *testing.T) {
	row := map[string]any{
		"cantidad":        2,
		"nombre_producto": "CEBICHE CLASSIC",
		"precio":          "S/ 45.00",
	}

	qtyVal := getRowValue(row, "CANT")
	if qtyVal != "2" {
		t.Errorf("Expected '2', got %q", qtyVal)
	}

	prodVal := getRowValue(row, "PRODUCTO")
	if prodVal != "CEBICHE CLASSIC" {
		t.Errorf("Expected 'CEBICHE CLASSIC', got %q", prodVal)
	}

	totalVal := getRowValue(row, "TOTAL")
	if totalVal != "S/ 45.00" {
		t.Errorf("Expected 'S/ 45.00', got %q", totalVal)
	}
}
