package queue

import (
	"bytes"
	"encoding/base64"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"
	"strings"

	"github.com/fogleman/gg"
	_ "golang.org/x/image/bmp"

	"usqay-print-client/internal/escpos"
)

// IRBlock represents a measured layout block that can render itself to ESC/POS.
type IRBlock interface {
	Measure(widthChars int)
	Render(b *escpos.Builder, widthChars int)
}

// IRText is a block of simple aligned text.
type IRText struct {
	Value string
	Align string
	Bold  bool
	Size  string
}

func (t *IRText) Measure(widthChars int) {}

func (t *IRText) Render(b *escpos.Builder, widthChars int) {
	applyAlign(b, t.Align)
	b.Bold(t.Bold).Size(t.Size).Line(t.Value)
	b.Bold(false).Size("normal")
}

// IRSeparator is a horizontal line of repeating characters.
type IRSeparator struct {
	Character string
}

func (s *IRSeparator) Measure(widthChars int) {}

func (s *IRSeparator) Render(b *escpos.Builder, widthChars int) {
	char := orDefault(s.Character, "-")
	b.Left().Line(strings.Repeat(char, widthChars))
}

// IRSpacer represents vertical whitespace spacing.
type IRSpacer struct {
	Lines int
}

func (s *IRSpacer) Measure(widthChars int) {}

func (s *IRSpacer) Render(b *escpos.Builder, widthChars int) {
	b.Feed(positiveOrDefault(s.Lines, 1))
}

// IRTable represents a structured table.
type IRTable struct {
	Columns []TableColumn
	Rows    []TableRow
}

func (t *IRTable) Measure(widthChars int) {}

func (t *IRTable) Render(b *escpos.Builder, widthChars int) {
	renderTableESCPOS(b, TableBlock{Columns: t.Columns, Rows: t.Rows}, widthChars)
}

// IRColumns represents a side-by-side columns block.
type IRColumns struct {
	Columns []ColumnsCell
}

func (c *IRColumns) Measure(widthChars int) {}

func (c *IRColumns) Render(b *escpos.Builder, widthChars int) {
	renderColumnsESCPOS(b, ColumnsBlock{Columns: c.Columns}, widthChars)
}

// IRQR represents a QR code block.
type IRQR struct {
	Value      string
	Align      string
	Size       int
	NativeQR   bool
}

func (q *IRQR) Measure(widthChars int) {}

func (q *IRQR) Render(b *escpos.Builder, widthChars int) {
	applyAlign(b, q.Align)
	if q.NativeQR {
		b.QR(q.Value, positiveOrDefault(q.Size, 6))
	} else {
		b.Line("[QR: " + q.Value + "]")
	}
	b.Left()
}

// IRBarcode represents a 1D barcode block.
type IRBarcode struct {
	Symbology string
	Value     string
	Align     string
	Height    int
	Width     int
	HRI       string
}

func (bc *IRBarcode) Measure(widthChars int) {}

func (bc *IRBarcode) Render(b *escpos.Builder, widthChars int) {
	applyAlign(b, bc.Align)
	b.Barcode(bc.Symbology, bc.Value, bc.Height, bc.Width, bc.HRI)
	b.Left()
}

// IRImage represents a raster image block.
type IRImage struct {
	Data           string
	Align          string
	Width          int
	Dither         bool
	SupportsRaster bool
}

func (img *IRImage) Measure(widthChars int) {}

func (img *IRImage) Render(b *escpos.Builder, widthChars int) {
	if !img.SupportsRaster {
		applyAlign(b, img.Align)
		b.Line("[IMAGE]")
		b.Left()
		return
	}

	if img.Data == "" {
		return
	}

	// 1. Limpiar y decodificar base64
	b64Data := img.Data
	if idx := strings.Index(b64Data, ";base64,"); idx != -1 {
		b64Data = b64Data[idx+8:]
	}
	b64Data = strings.TrimSpace(b64Data)

	imgBytes, err := base64.StdEncoding.DecodeString(b64Data)
	if err != nil {
		slog.Error("error decodificando imagen base64", "err", err)
		return
	}

	// 2. Decodificar la imagen
	srcImg, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		slog.Error("error decodificando formato de imagen", "err", err)
		return
	}

	// 3. Redimensionar usando fogleman/gg
	bounds := srcImg.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()
	if srcW <= 0 || srcH <= 0 {
		return
	}

	// El ancho del papel en dots
	printWidthDots := widthChars * 12

	targetW := img.Width
	if targetW <= 0 || targetW > printWidthDots {
		targetW = printWidthDots
	}

	targetH := int(float64(srcH) * float64(targetW) / float64(srcW))
	if targetH <= 0 {
		return
	}

	// Crear contexto gg para redimensionar la imagen de forma limpia
	dc := gg.NewContext(targetW, targetH)
	dc.Scale(float64(targetW)/float64(srcW), float64(targetH)/float64(srcH))
	dc.DrawImage(srcImg, 0, 0)
	dstImg := dc.Image()

	// 4. Convertir a monocromo con umbral (threshold) o Floyd-Steinberg dithering
	var raster []byte
	if img.Dither {
		raster = ditherFloydSteinberg(dstImg, targetW, targetH)
	} else {
		raster = thresholdMonochrome(dstImg, targetW, targetH)
	}

	// 5. Enviar comando raster al builder
	applyAlign(b, img.Align)
	b.RasterImage(0, targetW, targetH, raster)
	b.Left()
}

func thresholdMonochrome(img image.Image, w, h int) []byte {
	rowBytes := (w + 7) / 8
	raster := make([]byte, rowBytes*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			lum := (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 257.0
			if lum < 128.0 {
				byteIdx := y*rowBytes + (x / 8)
				bitIdx := 7 - (x % 8)
				raster[byteIdx] |= (1 << bitIdx)
			}
		}
	}
	return raster
}

func ditherFloydSteinberg(img image.Image, w, h int) []byte {
	pixels := make([][]float64, h)
	for y := 0; y < h; y++ {
		pixels[y] = make([]float64, w)
		for x := 0; x < w; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			lum := (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 257.0
			pixels[y][x] = lum
		}
	}

	rowBytes := (w + 7) / 8
	raster := make([]byte, rowBytes*h)

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			oldPixel := pixels[y][x]
			var newPixel float64
			var bitVal byte
			if oldPixel < 128.0 {
				newPixel = 0.0
				bitVal = 1
			} else {
				newPixel = 255.0
				bitVal = 0
			}

			if bitVal == 1 {
				byteIdx := y*rowBytes + (x / 8)
				bitIdx := 7 - (x % 8)
				raster[byteIdx] |= (1 << bitIdx)
			}

			quantError := oldPixel - newPixel

			if x+1 < w {
				pixels[y][x+1] += quantError * 7.0 / 16.0
			}
			if y+1 < h {
				if x-1 >= 0 {
					pixels[y+1][x-1] += quantError * 3.0 / 16.0
				}
				pixels[y+1][x] += quantError * 5.0 / 16.0
				if x+1 < w {
					pixels[y+1][x+1] += quantError * 1.0 / 16.0
				}
			}
		}
	}
	return raster
}
