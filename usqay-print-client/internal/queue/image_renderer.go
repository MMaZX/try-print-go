package queue

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"
	"math"
	"strings"

	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
	qrcode "github.com/skip2/go-qrcode"
	_ "golang.org/x/image/bmp"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"

	"usqay-print-client/internal/printer"
)

var (
	fontRegular *truetype.Font
	fontBold    *truetype.Font
	fontMono    *truetype.Font
)

func init() {
	var err error
	fontRegular, err = truetype.Parse(goregular.TTF)
	if err != nil {
		slog.Error("error cargando font regular", "err", err)
	}
	fontBold, err = truetype.Parse(gobold.TTF)
	if err != nil {
		slog.Error("error cargando font bold", "err", err)
	}
	fontMono, err = truetype.Parse(gomono.TTF)
	if err != nil {
		slog.Error("error cargando font mono", "err", err)
	}
}

// getFontFace returns a font.Face with the requested size and bold weight.
func getFontFace(size float64, bold bool) font.Face {
	f := fontRegular
	if bold {
		f = fontBold
	}
	if f == nil {
		return nil
	}
	return truetype.NewFace(f, &truetype.Options{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
}

func getFontSizePixels(sizeStr string, widthDots int) float64 {
	isSmallRoll := widthDots <= 400
	switch strings.ToLower(sizeStr) {
	case "double":
		if isSmallRoll {
			return 28.0
		}
		return 38.0
	case "medium":
		if isSmallRoll {
			return 22.0
		}
		return 28.0
	default:
		if isSmallRoll {
			return 17.0
		}
		return 20.0
	}
}

// RenderImage interprets a PrintPayload JSON and renders the entire document
// into a 100% pixel-perfect raster image, emitting ESC/POS raster bit image commands (GS v 0).
func RenderImage(prof *printer.DeviceProfile, payloadJSON string) ([]byte, error) {
	var p PrintPayload
	if err := json.Unmarshal([]byte(payloadJSON), &p); err != nil {
		return nil, fmt.Errorf("parsear payload JSON: %w", err)
	}

	// 1. Determinar ancho en dots
	widthDots := 576 // 80mm por defecto @ 203 DPI
	if p.Margins.DimensionPapel > 0 {
		widthDots = int(math.Round(p.Margins.DimensionPapel * (203.0 / 25.4)))
	} else if p.Margins.AnchoDimension > 0 {
		widthDots = int(math.Round(p.Margins.AnchoDimension * (203.0 / 25.4)))
	} else if prof != nil && prof.WidthDots > 0 {
		widthDots = prof.WidthDots
	}

	// Asegurar múltiplo de 8 para alineación de bytes raster
	if widthDots%8 != 0 {
		widthDots = (widthDots / 8) * 8
	}
	if widthDots < 240 {
		widthDots = 576
	}

	paddingDots := 16.0 // ~2mm por defecto
	if p.Margins.Padding > 0 {
		paddingDots = p.Margins.Padding * (203.0 / 25.4)
	}
	usableWidth := float64(widthDots) - (2 * paddingDots)

	// 2. Primera pasada: Medir altura total requerida
	measurer := gg.NewContext(widthDots, 100)
	totalHeight := paddingDots

	type preparedBlock struct {
		blockType string
		raw       json.RawMessage
		height    float64
	}

	var prepared []preparedBlock

	for _, rawBlock := range p.Body {
		var env blockEnvelope
		if err := json.Unmarshal(rawBlock, &env); err != nil {
			continue
		}

		h := measureBlockHeight(measurer, env.Type, rawBlock, usableWidth, widthDots)
		prepared = append(prepared, preparedBlock{
			blockType: env.Type,
			raw:       rawBlock,
			height:    h,
		})
		totalHeight += h
	}

	totalHeight += paddingDots + 20 // Margen inferior

	canvasH := int(math.Ceil(totalHeight))
	if canvasH < 50 {
		canvasH = 50
	}

	// 3. Segunda pasada: Dibujar en el Canvas
	dc := gg.NewContext(widthDots, canvasH)
	dc.SetColor(color.White)
	dc.Clear()
	dc.SetColor(color.Black)

	currentY := paddingDots

	for _, pb := range prepared {
		currentY = drawBlock(dc, pb.blockType, pb.raw, currentY, paddingDots, usableWidth, widthDots)
	}

	// 4. Binarizar el lienzo con alto contraste para máxima nitidez térmica
	img := dc.Image()
	raster := thresholdMonochromeCrisp(img, widthDots, canvasH)

	// 4.1. Auto-recortar espacio en blanco inferior
	rowBytes := widthDots / 8
	lastBlackY := 0
	for y := canvasH - 1; y >= 0; y-- {
		start := y * rowBytes
		end := start + rowBytes
		hasBlack := false
		for _, b := range raster[start:end] {
			if b != 0 {
				hasBlack = true
				break
			}
		}
		if hasBlack {
			lastBlackY = y
			break
		}
	}

	// Ajustar altura efectiva con un pequeño margen
	effectiveH := lastBlackY + 8
	if effectiveH > canvasH {
		effectiveH = canvasH
	}
	if effectiveH < 20 {
		effectiveH = canvasH
	}

	// 5. Construir bytes ESC/POS en tiras raster
	var buf bytes.Buffer
	buf.Write([]byte{0x1B, 0x40}) // ESC @ - Reset

	// Dividir en tiras de 128 líneas para máxima compatibilidad de buffer
	const maxChunkH = 128

	for y := 0; y < effectiveH; y += maxChunkH {
		chunkH := maxChunkH
		if y+chunkH > effectiveH {
			chunkH = effectiveH - y
		}

		startOffset := y * rowBytes
		endOffset := startOffset + (chunkH * rowBytes)
		chunkData := raster[startOffset:endOffset]

		xL := byte(rowBytes & 0xFF)
		xH := byte((rowBytes >> 8) & 0xFF)
		yL := byte(chunkH & 0xFF)
		yH := byte((chunkH >> 8) & 0xFF)

		// GS v 0 0 xL xH yL yH data...
		buf.Write([]byte{0x1D, 0x76, 0x30, 0x00, xL, xH, yL, yH})
		buf.Write(chunkData)
	}

	// Feed exacto para que el contenido pase la cuchilla física (~20-25mm)
	buf.Write([]byte{0x1B, 0x64, 0x04}) // ESC d 4 (Avanzar 4 líneas)

	if p.Options.Cut {
		buf.Write([]byte{0x1D, 0x56, 0x42, 0x00}) // GS V 66 0 (Corte parcial estándar)
	}
	if p.Options.Drawer {
		buf.Write([]byte{0x1B, 0x70, 0x00, 0x19, 0xFA}) // ESC p 0 25 250 (Pulso cajón)
	}

	return buf.Bytes(), nil
}

func measureBlockHeight(dc *gg.Context, bType string, raw json.RawMessage, usableW float64, widthDots int) float64 {
	switch bType {
	case "text":
		var b TextBlock
		if err := json.Unmarshal(raw, &b); err != nil {
			return 0
		}
		fontSize := getFontSizePixels(b.Size, widthDots)
		face := getFontFace(fontSize, b.Bold)
		if face != nil {
			dc.SetFontFace(face)
		}
		lines := dc.WordWrap(b.Value, usableW)
		if len(lines) == 0 {
			lines = []string{""}
		}
		lineHeight := fontSize * 1.35
		return float64(len(lines))*lineHeight + 4.0

	case "separator":
		return 16.0

	case "spacer":
		var b SpacerBlock
		if err := json.Unmarshal(raw, &b); err != nil {
			return 20.0
		}
		lines := b.Lines
		if lines <= 0 {
			lines = 1
		}
		return float64(lines) * 20.0

	case "table":
		var b TableBlock
		if err := json.Unmarshal(raw, &b); err != nil {
			return 0
		}
		var totalH float64
		colDefs := normalizeColumns(b.Columns, len(b.Rows))

		// Altura de encabezados
		hasHeaders := false
		for _, col := range colDefs {
			if col.Header != nil && *col.Header != "" {
				hasHeaders = true
				break
			}
		}
		if hasHeaders {
			totalH += 24.0 // Altura fila encabezado
		}

		// Altura de filas
		for _, row := range b.Rows {
			if row.Merge && len(row.Cells) > 0 {
				fontSize := getFontSizePixels(row.Cells[0].Size, widthDots)
				face := getFontFace(fontSize, row.Bold || isCellBold(row.Cells[0].Bold, row.Bold))
				dc.SetFontFace(face)
				lines := dc.WordWrap(row.Cells[0].Text, usableW-16.0)
				totalH += float64(len(lines))*fontSize*1.3 + 10.0
				continue
			}

			maxRowH := 20.0
			for cIdx, cell := range row.Cells {
				if cIdx >= len(colDefs) {
					break
				}
				colW := colDefs[cIdx].Width * usableW
				fontSize := getFontSizePixels(cell.Size, widthDots)
				face := getFontFace(fontSize, isCellBold(cell.Bold, row.Bold))
				dc.SetFontFace(face)
				lines := dc.WordWrap(cell.Text, colW-4.0)
				cellH := float64(len(lines)) * fontSize * 1.3
				if cellH > maxRowH {
					maxRowH = cellH
				}
			}
			totalH += maxRowH + 6.0
		}
		return totalH + 8.0

	case "columns":
		var b ColumnsBlock
		if err := json.Unmarshal(raw, &b); err != nil {
			return 0
		}
		maxH := 20.0
		for _, col := range b.Columns {
			colW := col.Width * usableW
			fontSize := getFontSizePixels(col.Size, widthDots)
			face := getFontFace(fontSize, col.Bold)
			dc.SetFontFace(face)
			lines := dc.WordWrap(col.Text, colW-4.0)
			h := float64(len(lines)) * fontSize * 1.3
			if h > maxH {
				maxH = h
			}
		}
		return maxH + 6.0

	case "qr":
		var b QRBlock
		_ = json.Unmarshal(raw, &b)
		qrSize := 160.0
		if b.Size > 0 {
			qrSize = float64(b.Size) * 28.0
			if qrSize > usableW {
				qrSize = usableW
			}
		}
		return qrSize + 16.0

	case "barcode":
		var b BarcodeBlock
		_ = json.Unmarshal(raw, &b)
		h := 80.0
		if b.Height > 0 {
			h = float64(b.Height)
		}
		return h + 30.0

	case "image":
		var b ImageBlock
		if err := json.Unmarshal(raw, &b); err != nil || b.Data == "" {
			return 0
		}
		srcImg := decodeBase64Image(b.Data)
		if srcImg == nil {
			return 0
		}
		bounds := srcImg.Bounds()
		targetW := usableW
		if b.Width > 0 && float64(b.Width) < usableW {
			targetW = float64(b.Width)
		}
		targetH := float64(bounds.Dy()) * targetW / float64(bounds.Dx())
		return targetH + 10.0

	default:
		return 0
	}
}

func drawBlock(dc *gg.Context, bType string, raw json.RawMessage, currentY, paddingX, usableW float64, widthDots int) float64 {
	switch bType {
	case "text":
		var b TextBlock
		if err := json.Unmarshal(raw, &b); err != nil {
			return currentY
		}
		fontSize := getFontSizePixels(b.Size, widthDots)
		face := getFontFace(fontSize, b.Bold)
		dc.SetFontFace(face)
		lines := dc.WordWrap(b.Value, usableW)
		lineHeight := fontSize * 1.35

		for _, line := range lines {
			drawAlignedText(dc, line, b.Align, paddingX, currentY+fontSize, usableW)
			currentY += lineHeight
		}
		return currentY + 4.0

	case "separator":
		var b SeparatorBlock
		_ = json.Unmarshal(raw, &b)
		dc.SetLineWidth(1.5)
		if b.Character == "=" {
			dc.DrawLine(paddingX, currentY+4, paddingX+usableW, currentY+4)
			dc.Stroke()
			dc.DrawLine(paddingX, currentY+8, paddingX+usableW, currentY+8)
			dc.Stroke()
			return currentY + 16.0
		}
		// Línea discontinua elegante para separadores estándar
		dc.SetDash(6, 4)
		dc.DrawLine(paddingX, currentY+6, paddingX+usableW, currentY+6)
		dc.Stroke()
		dc.SetDash() // Reset dash
		return currentY + 16.0

	case "spacer":
		var b SpacerBlock
		if err := json.Unmarshal(raw, &b); err != nil {
			return currentY + 20.0
		}
		lines := b.Lines
		if lines <= 0 {
			lines = 1
		}
		return currentY + (float64(lines) * 20.0)

	case "table":
		var b TableBlock
		if err := json.Unmarshal(raw, &b); err != nil {
			return currentY
		}
		colDefs := normalizeColumns(b.Columns, len(b.Rows))

		// Encabezado
		hasHeaders := false
		for _, col := range colDefs {
			if col.Header != nil && *col.Header != "" {
				hasHeaders = true
				break
			}
		}
		if hasHeaders {
			headerSize := 19.0
			if widthDots <= 400 {
				headerSize = 16.0
			}
			dc.SetFontFace(getFontFace(headerSize, true))
			curX := paddingX
			for _, col := range colDefs {
				colW := col.Width * usableW
				if col.Header != nil {
					drawAlignedText(dc, *col.Header, col.Align, curX, currentY+headerSize, colW)
				}
				curX += colW
			}
			currentY += headerSize + 4.0
			dc.SetLineWidth(1.0)
			dc.DrawLine(paddingX, currentY, paddingX+usableW, currentY)
			dc.Stroke()
			currentY += 6.0
		}

		// Filas
		for _, row := range b.Rows {
			if row.Merge && len(row.Cells) > 0 {
				fontSize := getFontSizePixels(row.Cells[0].Size, widthDots)
				bold := row.Bold || isCellBold(row.Cells[0].Bold, row.Bold)
				dc.SetFontFace(getFontFace(fontSize, bold))
				lines := dc.WordWrap(row.Cells[0].Text, usableW-16.0)
				lineH := fontSize * 1.3

				// Dibujar caja destacada para notas de cocina
				boxH := float64(len(lines))*lineH + 8.0
				dc.SetLineWidth(1.0)
				dc.SetDash(4, 3)
				dc.DrawRectangle(paddingX+2, currentY, usableW-4, boxH)
				dc.Stroke()
				dc.SetDash()

				for lIdx, line := range lines {
					dc.DrawString(line, paddingX+8, currentY+14+(float64(lIdx)*lineH))
				}
				currentY += boxH + 6.0
				continue
			}

			maxRowH := 20.0
			curX := paddingX
			type cellDrawData struct {
				lines    []string
				align    string
				x, w     float64
				fontSize float64
				bold     bool
			}
			var cellsToDraw []cellDrawData

			for cIdx, cell := range row.Cells {
				if cIdx >= len(colDefs) {
					break
				}
				colDef := colDefs[cIdx]
				colW := colDef.Width * usableW
				fontSize := getFontSizePixels(cell.Size, widthDots)
				bold := isCellBold(cell.Bold, row.Bold)
				align := cell.Align
				if align == "" {
					align = colDef.Align
				}

				dc.SetFontFace(getFontFace(fontSize, bold))
				lines := dc.WordWrap(cell.Text, colW-4.0)
				cellH := float64(len(lines)) * fontSize * 1.3
				if cellH > maxRowH {
					maxRowH = cellH
				}

				cellsToDraw = append(cellsToDraw, cellDrawData{
					lines:    lines,
					align:    align,
					x:        curX,
					w:        colW,
					fontSize: fontSize,
					bold:     bold,
				})
				curX += colW
			}

			for _, cData := range cellsToDraw {
				dc.SetFontFace(getFontFace(cData.fontSize, cData.bold))
				lineH := cData.fontSize * 1.3
				for lIdx, line := range cData.lines {
					drawAlignedText(dc, line, cData.align, cData.x, currentY+cData.fontSize+(float64(lIdx)*lineH), cData.w)
				}
			}
			currentY += maxRowH + 6.0
		}
		return currentY + 4.0

	case "columns":
		var b ColumnsBlock
		if err := json.Unmarshal(raw, &b); err != nil {
			return currentY
		}
		curX := paddingX
		maxH := 20.0
		for _, col := range b.Columns {
			colW := col.Width * usableW
			fontSize := getFontSizePixels(col.Size, widthDots)
			dc.SetFontFace(getFontFace(fontSize, col.Bold))
			lines := dc.WordWrap(col.Text, colW-4.0)
			lineH := fontSize * 1.3
			for lIdx, line := range lines {
				drawAlignedText(dc, line, col.Align, curX, currentY+fontSize+(float64(lIdx)*lineH), colW)
			}
			h := float64(len(lines)) * lineH
			if h > maxH {
				maxH = h
			}
			curX += colW
		}
		return currentY + maxH + 6.0

	case "qr":
		var b QRBlock
		_ = json.Unmarshal(raw, &b)
		if b.Value == "" {
			return currentY
		}
		qrSize := 160
		if b.Size > 0 {
			qrSize = b.Size * 28
			if float64(qrSize) > usableW {
				qrSize = int(usableW)
			}
		}
		qrImg, err := qrcode.Encode(b.Value, qrcode.Medium, qrSize)
		if err == nil {
			srcImg, _, err := image.Decode(bytes.NewReader(qrImg))
			if err == nil {
				x := paddingX
				switch strings.ToLower(b.Align) {
				case "center":
					x = paddingX + (usableW-float64(qrSize))/2.0
				case "right":
					x = paddingX + usableW - float64(qrSize)
				}
				dc.DrawImage(srcImg, int(x), int(currentY))
				return currentY + float64(qrSize) + 16.0
			}
		}
		return currentY

	case "image":
		var b ImageBlock
		if err := json.Unmarshal(raw, &b); err != nil || b.Data == "" {
			return currentY
		}
		srcImg := decodeBase64Image(b.Data)
		if srcImg == nil {
			return currentY
		}
		bounds := srcImg.Bounds()
		targetW := usableW
		if b.Width > 0 && float64(b.Width) < usableW {
			targetW = float64(b.Width)
		}
		targetH := float64(bounds.Dy()) * targetW / float64(bounds.Dx())

		x := paddingX
		switch strings.ToLower(b.Align) {
		case "center":
			x = paddingX + (usableW-targetW)/2.0
		case "right":
			x = paddingX + usableW - targetW
		}

		// Escalar y dibujar
		dc.Push()
		dc.Translate(x, currentY)
		dc.Scale(targetW/float64(bounds.Dx()), targetH/float64(bounds.Dy()))
		dc.DrawImage(srcImg, 0, 0)
		dc.Pop()

		return currentY + targetH + 10.0

	default:
		return currentY
	}
}

func drawAlignedText(dc *gg.Context, text, align string, x, y, width float64) {
	strW, _ := dc.MeasureString(text)
	var drawX float64
	switch strings.ToLower(align) {
	case "center":
		drawX = x + (width-strW)/2.0
	case "right":
		drawX = x + width - strW
	default:
		drawX = x
	}
	dc.DrawString(text, drawX, y)
}

func normalizeColumns(cols []TableColumn, rowCount int) []TableColumn {
	if len(cols) > 0 {
		return cols
	}
	// Fallback 2 columnas automáticas si no hay definición
	return []TableColumn{
		{Width: 0.6, Align: "left"},
		{Width: 0.4, Align: "right"},
	}
}

func isCellBold(cellBold *bool, rowBold bool) bool {
	if cellBold != nil {
		return *cellBold
	}
	return rowBold
}

func decodeBase64Image(b64Data string) image.Image {
	if idx := strings.Index(b64Data, ";base64,"); idx != -1 {
		b64Data = b64Data[idx+8:]
	}
	b64Data = strings.TrimSpace(b64Data)
	imgBytes, err := base64.StdEncoding.DecodeString(b64Data)
	if err != nil {
		return nil
	}
	srcImg, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return nil
	}
	return srcImg
}

// thresholdMonochromeCrisp converts an RGB canvas into a crisp 1-bit monochrome bitmap
// optimized for 203 DPI thermal printer heads by capturing anti-aliased font strokes.
func thresholdMonochromeCrisp(img image.Image, w, h int) []byte {
	rowBytes := (w + 7) / 8
	raster := make([]byte, rowBytes*h)
	// Umbral de 205.0: Convierte cualquier píxel gris de suavizado tipográfico
	// en un punto negro sólido para que las letras no se vean huecas ni mordidas.
	const threshold = 205.0

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			lum := (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 257.0
			if lum < threshold {
				byteIdx := y*rowBytes + (x / 8)
				bitIdx := 7 - (x % 8)
				raster[byteIdx] |= (1 << bitIdx)
			}
		}
	}
	return raster
}
