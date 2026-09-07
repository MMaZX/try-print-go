package htmlrender

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"time"

	"github.com/adcondev/poster/pkg/commands/bitimage"
	"github.com/adcondev/poster/pkg/commands/character"
	"github.com/adcondev/poster/pkg/composer"
	"github.com/adcondev/poster/pkg/graphics"
	"github.com/adcondev/poster/pkg/profile"
	"github.com/adcondev/poster/pkg/service"
	"github.com/chromedp/chromedp"

	"usqay-print-client/internal/printer"
)

const defaultRenderTimeout = 5 * time.Second

type memoryConnector struct {
	buf bytes.Buffer
}

func (m *memoryConnector) Write(p []byte) (n int, err error) { return m.buf.Write(p) }
func (m *memoryConnector) Read(p []byte) (n int, err error)  { return m.buf.Read(p) }
func (m *memoryConnector) Close() error                      { return nil }

// RenderHTMLToPNG renderiza un string HTML con CSS a una imagen PNG en memoria usando chromedp.
func RenderHTMLToPNG(ctx context.Context, htmlContent string, widthDots int) ([]byte, error) {
	browserPath, err := FindBrowserExecutable()
	if err != nil {
		return nil, fmt.Errorf("localizar navegador: %w", err)
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(browserPath),
		chromedp.Flag("headless", "new"),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-software-rasterizer", true),
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("window-size", fmt.Sprintf("%d,%d", widthDots, 1000)),
	)

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	defer cancelAlloc()

	taskCtx, cancelTask := chromedp.NewContext(allocCtx)
	defer cancelTask()

	if _, hasDeadline := taskCtx.Deadline(); !hasDeadline {
		var cancelTimeout context.CancelFunc
		taskCtx, cancelTimeout = context.WithTimeout(taskCtx, defaultRenderTimeout)
		defer cancelTimeout()
	}

	dataURI := "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(htmlContent))

	var buf []byte
	err = chromedp.Run(taskCtx,
		chromedp.Navigate(dataURI),
		chromedp.WaitVisible("#ticket", chromedp.ByID),
		chromedp.Screenshot("#ticket", &buf, chromedp.NodeVisible, chromedp.ByID),
	)
	if err != nil {
		// Fallback a FullScreenshot si el selector de nodo falla
		fallbackErr := chromedp.Run(taskCtx,
			chromedp.Navigate(dataURI),
			chromedp.FullScreenshot(&buf, 100),
		)
		if fallbackErr != nil {
			return nil, fmt.Errorf("captura chromedp: %w (fallback: %v)", err, fallbackErr)
		}
	}

	return buf, nil
}

// RenderHTMLToImage renderiza un string HTML a un objeto image.Image en memoria.
func RenderHTMLToImage(ctx context.Context, htmlContent string, widthDots int) (image.Image, error) {
	pngBytes, err := RenderHTMLToPNG(ctx, htmlContent, widthDots)
	if err != nil {
		return nil, err
	}
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, fmt.Errorf("decodificar PNG: %w", err)
	}
	return img, nil
}

// RenderPayloadToImage convierte un PrintPayload JSON en image.Image en memoria
// delegando en el generador de plantillas HTML y chromedp.
func RenderPayloadToImage(prof *printer.DeviceProfile, payloadJSON string) (image.Image, error) {
	htmlContent, widthDots, _, err := BuildHTMLFromJSON(prof, payloadJSON)
	if err != nil {
		return nil, fmt.Errorf("construir HTML: %w", err)
	}
	return RenderHTMLToImage(context.Background(), htmlContent, widthDots)
}

// RenderPayloadToPNG renderiza el payload JSON a formato PNG directamente sobre un io.Writer.
func RenderPayloadToPNG(prof *printer.DeviceProfile, payloadJSON string, w io.Writer) error {
	img, err := RenderPayloadToImage(prof, payloadJSON)
	if err != nil {
		return err
	}
	return png.Encode(w, img)
}

// RenderPayloadToPNGFile renderiza el payload JSON y lo guarda en el archivo destino especificado.
func RenderPayloadToPNGFile(prof *printer.DeviceProfile, payloadJSON string, outputPath string) error {
	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("crear archivo PNG %q: %w", outputPath, err)
	}
	defer f.Close()
	return RenderPayloadToPNG(prof, payloadJSON, f)
}

// RenderThermal convierte un PrintPayload JSON en bytes ESC/POS (raster GS v 0)
// para enviar directamente al spooler de la impresora térmica.
func RenderThermal(prof *printer.DeviceProfile, payloadJSON string) ([]byte, error) {
	htmlContent, widthDots, payload, err := BuildHTMLFromJSON(prof, payloadJSON)
	if err != nil {
		return nil, fmt.Errorf("procesar payload JSON: %w", err)
	}

	var activeProf printer.DeviceProfile
	if prof != nil {
		activeProf = *prof
	} else if payload.PaperProperties.Width > 0 {
		activeProf = printer.DeriveProfileFromWidth(payload.PaperProperties.Width)
	} else {
		activeProf = printer.Default58mmProfile()
	}

	// Renderizar la imagen continua del ticket en memoria
	img, err := RenderHTMLToImage(context.Background(), htmlContent, widthDots)
	if err != nil {
		return nil, fmt.Errorf("renderizar ticket a imagen: %w", err)
	}

	conn := &memoryConnector{}
	proto := composer.NewEscpos()
	posterProfile := &profile.Escpos{
		Model:            "usqay-thermal-html",
		PaperWidth:       payload.PaperProperties.Width,
		DPI:              activeProf.DPI,
		DotsPerLine:      activeProf.WidthDots,
		SupportsGraphics: activeProf.SupportsRaster,
		SupportsBarcode:  true,
		HasQR:            activeProf.SupportsQRNative,
		SupportsCutter:   activeProf.SupportsCut,
		SupportsDrawer:   activeProf.SupportsDrawer,
		CodeTable:        character.PC850,
	}

	svc, err := service.NewPrinter(proto, posterProfile, conn)
	if err != nil {
		return nil, fmt.Errorf("crear printer poster: %w", err)
	}
	if err := svc.Initialize(); err != nil {
		return nil, fmt.Errorf("inicializar impresora: %w", err)
	}

	dots := activeProf.WidthDots
	totalPaperDots := dots
	if payload.PaperProperties.Width > 0 {
		t := (payload.PaperProperties.Width - 58.0) / (80.0 - 58.0)
		d := 384.0 + t*(576.0-384.0)
		totalPaperDots = int(math.Round(d))
	}

	leftMarginDots := 0
	printWidthDots := dots
	if totalPaperDots > dots {
		leftMarginDots = (totalPaperDots - dots) / 2
	}

	leftPadDots := int(math.Round(payload.PaperProperties.LeftMM() * 8.0))
	rightPadDots := int(math.Round(payload.PaperProperties.RightMM() * 8.0))
	if leftPadDots > 0 || rightPadDots > 0 {
		leftMarginDots += leftPadDots
		printWidthDots -= (leftPadDots + rightPadDots)
		if printWidthDots < 96 {
			printWidthDots = 96
		}
	}

	if activeProf.SupportsPrintArea {
		if err := svc.Write(proto.LeftMargin(uint16(leftMarginDots))); err != nil {
			return nil, fmt.Errorf("fijar margen izquierdo: %w", err)
		}
		if err := svc.Write(proto.PrintWidth(uint16(printWidthDots))); err != nil {
			return nil, fmt.Errorf("fijar ancho de impresión: %w", err)
		}
	}

	// Apertura de gaveta de dinero
	if payload.Options.Drawer && activeProf.SupportsDrawer {
		drawerPulse := []byte{0x1B, 0x70, 0x00, 0x19, 0xFA}
		if err := svc.Write(drawerPulse); err != nil {
			return nil, fmt.Errorf("abrir cajón: %w", err)
		}
	}

	// Binarización y rasterizado de la imagen
	bounds := img.Bounds()
	if bounds.Dx() > 0 && bounds.Dy() > 0 {
		pipeline := graphics.NewPipeline(&graphics.ImgOptions{
			PixelWidth:     bounds.Dx(),
			Threshold:      128,
			Dithering:      graphics.Threshold,
			Scaling:        graphics.BiLinear,
			PreserveAspect: true,
		})
		bmp, err := pipeline.Process(img)
		if err != nil {
			return nil, fmt.Errorf("binarizar imagen: %w", err)
		}
		if err := writeRasterChunked(svc, proto, bmp); err != nil {
			return nil, fmt.Errorf("escribir tiras raster: %w", err)
		}
	}

	// Corte de papel o avance final
	if payload.Options.Cut && activeProf.SupportsCut {
		if err := svc.PartialFeedAndCut(3); err != nil {
			return nil, fmt.Errorf("cortar papel: %w", err)
		}
	} else if err := svc.FeedLines(3); err != nil {
		return nil, fmt.Errorf("avanzar papel: %w", err)
	}

	return conn.buf.Bytes(), nil
}

func writeRasterChunked(svc *service.Printer, proto *composer.EscposProtocol, bmp *graphics.MonochromeBitmap) error {
	rowBytes := bmp.GetWidthBytes()
	raster := bmp.GetRasterData()
	const maxRows = 128

	for y := 0; y < bmp.Height; y += maxRows {
		chunkH := maxRows
		if y+chunkH > bmp.Height {
			chunkH = bmp.Height - y
		}
		start := y * rowBytes
		end := start + chunkH*rowBytes

		cmd, err := proto.BitImage.PrintRasterBitImage(bitimage.Normal, uint16(rowBytes), uint16(chunkH), raster[start:end])
		if err != nil {
			return fmt.Errorf("componer raster: %w", err)
		}
		if err := svc.Write(cmd); err != nil {
			return fmt.Errorf("escribir raster: %w", err)
		}
	}
	return nil
}
