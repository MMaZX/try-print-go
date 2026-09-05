package ticketrender

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRenderPayloadToImageAndPNG(t *testing.T) {
	payloadPath := filepath.Join("..", "..", "dist", "test", "02_ticket_completo_80mm.json")
	raw, err := os.ReadFile(payloadPath)
	if err != nil {
		t.Fatalf("no se pudo leer archivo de prueba: %v", err)
	}

	// 1. Probar renderizado a image.Image
	img, err := RenderPayloadToImage(nil, string(raw))
	if err != nil {
		t.Fatalf("RenderPayloadToImage falló: %v", err)
	}

	bounds := img.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		t.Fatalf("dimensiones de imagen inválidas: %dx%d", bounds.Dx(), bounds.Dy())
	}

	// 2. Probar exportación a buffer PNG
	var buf bytes.Buffer
	if err := RenderPayloadToPNG(nil, string(raw), &buf); err != nil {
		t.Fatalf("RenderPayloadToPNG falló: %v", err)
	}

	if buf.Len() == 0 {
		t.Fatalf("el buffer PNG generado está vacío")
	}

	// 3. Probar renderizado a bytes térmicos ESC/POS
	escposBytes, err := RenderThermal(nil, string(raw))
	if err != nil {
		t.Fatalf("RenderThermal falló: %v", err)
	}

	if len(escposBytes) == 0 {
		t.Fatalf("los bytes ESC/POS generados están vacíos")
	}
}
