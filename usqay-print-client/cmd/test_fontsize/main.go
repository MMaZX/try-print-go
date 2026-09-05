// test_fontsize renders one sample block per candidate font-calibration
// width (Config.FontAWidth in internal/ticketimage), stacks them into a
// single ticket image, and prints it. Herramienta de diagnóstico temporal
// para elegir la calibración de Cascadia Code — no es parte del pipeline de
// producción (ese sigue siendo internal/queue.RenderThermal).
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"os"

	"github.com/adcondev/poster/pkg/commands/bitimage"
	"github.com/adcondev/poster/pkg/composer"
	"github.com/adcondev/poster/pkg/graphics"

	"usqay-print-client/internal/printer"
	"usqay-print-client/internal/ticketimage"
)

const pangram = "El veloz murcielago hindu comia feliz cardillo y kiwi. 0123456789"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "uso: test_fontsize.exe <printer_name> [ancho1 ancho2 ...]")
		os.Exit(1)
	}
	printerName := os.Args[1]

	widths := []float64{16, 18, 20, 24, 28, 30, 12, 14}
	if len(os.Args) > 2 {
		widths = nil
		for _, a := range os.Args[2:] {
			var v float64
			if _, err := fmt.Sscanf(a, "%f", &v); err == nil {
				widths = append(widths, v)
			}
		}
	}

	const paperPxWidth = 576 // 80mm

	var samples []image.Image
	totalHeight := 0
	for _, w := range widths {
		eng, err := ticketimage.NewEngine(ticketimage.Config{
			PaperPxWidth:            paperPxWidth,
			DPI:                     203,
			AutoAdjustCursorOnScale: true,
			FontAWidth:              w,
			FontAHeight:             w * 2,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error creando engine para width=%v: %v\n", w, err)
			os.Exit(1)
		}

		eng.AlignLeft()
		eng.SetBold(true)
		eng.PrintLine(fmt.Sprintf("FontAWidth=%.0fpx (height=%.0fpx)", w, w*2))
		eng.SetBold(false)
		eng.PrintWrapped(pangram)
		eng.PrintLine("0123456789012345678901234567890123456789012345")
		eng.Separator("=", paperPxWidth/12)

		img := eng.Render()
		samples = append(samples, img)
		totalHeight += img.Bounds().Dy()
	}

	combined := image.NewRGBA(image.Rect(0, 0, paperPxWidth, totalHeight))
	draw.Draw(combined, combined.Bounds(), image.White, image.Point{}, draw.Src)
	y := 0
	for _, img := range samples {
		b := img.Bounds()
		draw.Draw(combined, image.Rect(0, y, b.Dx(), y+b.Dy()), img, b.Min, draw.Over)
		y += b.Dy()
	}

	pipeline := graphics.NewPipeline(&graphics.ImgOptions{
		PixelWidth:     paperPxWidth,
		Threshold:      128,
		Dithering:      graphics.Threshold,
		Scaling:        graphics.BiLinear,
		PreserveAspect: true,
	})
	bmp, err := pipeline.Process(combined)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error binarizando: %v\n", err)
		os.Exit(1)
	}

	var buf bytes.Buffer
	buf.Write([]byte{0x1B, 0x40}) // ESC @

	proto := composer.NewEscpos()
	rowBytes := bmp.GetWidthBytes()
	raster := bmp.GetRasterData()
	const chunkRows = 128
	for yy := 0; yy < bmp.Height; yy += chunkRows {
		h := chunkRows
		if yy+h > bmp.Height {
			h = bmp.Height - yy
		}
		start := yy * rowBytes
		end := start + h*rowBytes
		cmd, err := proto.BitImage.PrintRasterBitImage(bitimage.Normal, uint16(rowBytes), uint16(h), raster[start:end])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error componiendo raster: %v\n", err)
			os.Exit(1)
		}
		buf.Write(cmd)
	}
	buf.Write([]byte{0x1B, 0x64, 0x04}) // feed
	buf.Write([]byte{0x1D, 0x56, 0x42, 0x00}) // corte parcial

	fmt.Printf("bytes=%d altura_px=%d\n", buf.Len(), totalHeight)

	p := printer.NewDirectDevicePrinter(printerName, true)
	if err := p.Print(buf.Bytes()); err != nil {
		fmt.Fprintf(os.Stderr, "error imprimiendo: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("OK")
}
