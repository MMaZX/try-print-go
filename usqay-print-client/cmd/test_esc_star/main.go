package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"os"
	"time"

	"github.com/fogleman/gg"
	"usqay-print-client/internal/printer"
)

// generateTestImage creates a small 384x120 test image with a black box, a circle, and text.
func generateTestImage() image.Image {
	dc := gg.NewContext(384, 120)
	dc.SetColor(color.White)
	dc.Clear()

	// Marco exterior
	dc.SetColor(color.Black)
	dc.SetLineWidth(3)
	dc.DrawRectangle(10, 10, 364, 100)
	dc.Stroke()

	// Círculo relleno
	dc.DrawCircle(60, 60, 35)
	dc.Fill()

	// Texto
	dc.DrawString("TEST GRAFICO 58MM", 120, 50)
	dc.DrawString("USQAY POS PRINTER", 120, 80)

	return dc.Image()
}

// buildESCStar converts an image into universal ESC * 33 (24-dot bit image) slices.
func buildESCStar(img image.Image, w, h int) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0x1B, 0x40}) // ESC @ (Reset)
	buf.Write([]byte{0x1B, 0x33, 0x18}) // ESC 3 24 (Line spacing 24 dots)

	// Procesar de a 24 líneas verticales
	for y := 0; y < h; y += 24 {
		nL := byte(w & 0xFF)
		nH := byte((w >> 8) & 0xFF)

		// ESC * 33 nL nH
		buf.Write([]byte{0x1B, 0x2A, 33, nL, nH})

		for x := 0; x < w; x++ {
			var b1, b2, b3 byte
			for bit := 0; bit < 8; bit++ {
				py := y + bit
				if py < h {
					r, g, b, _ := img.At(x, py).RGBA()
					lum := (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 257.0
					if lum < 180.0 {
						b1 |= (1 << (7 - bit))
					}
				}
			}
			for bit := 0; bit < 8; bit++ {
				py := y + 8 + bit
				if py < h {
					r, g, b, _ := img.At(x, py).RGBA()
					lum := (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 257.0
					if lum < 180.0 {
						b2 |= (1 << (7 - bit))
					}
				}
			}
			for bit := 0; bit < 8; bit++ {
				py := y + 16 + bit
				if py < h {
					r, g, b, _ := img.At(x, py).RGBA()
					lum := (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 257.0
					if lum < 180.0 {
						b3 |= (1 << (7 - bit))
					}
				}
			}
			buf.Write([]byte{b1, b2, b3})
		}
		buf.Write([]byte{0x0A}) // LF
	}

	buf.Write([]byte{0x1B, 0x32}) // ESC 2 (Reset line spacing)
	buf.Write([]byte{0x1B, 0x64, 0x03}) // Feed 3
	return buf.Bytes()
}

func main() {
	devicePath := "/dev/usb/lp0"
	if len(os.Args) > 1 {
		devicePath = os.Args[1]
	}

	p := printer.NewDirectDevicePrinter(devicePath, true)
	img := generateTestImage()

	fmt.Println("==================================================")
	fmt.Println("🧪 DIAGNÓSTICO: PROBANDO MÉTODO UNIVERSAL ESC * 33")
	fmt.Println("==================================================")

	start := time.Now()
	data := buildESCStar(img, 384, 120)
	renderTime := time.Since(start).Seconds()

	printStart := time.Now()
	err := p.Print(data)
	printTime := time.Since(printStart).Seconds()

	if err != nil {
		fmt.Printf("❌ Error imprimiendo ESC *: %v\n", err)
		return
	}

	fmt.Printf("✅ ESC * 33 enviado con éxito!\n")
	fmt.Printf("   Render: %.3fs | Print: %.3fs | Bytes: %d\n", renderTime, printTime, len(data))
	fmt.Println("==================================================")
}
