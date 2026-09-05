package ticketimage

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"log"
	"math"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/adcondev/poster/pkg/constants"
	emfonts "usqay-print-client/internal/ticketimage/fonts"
)

// FontMetrics contains calculated font dimensions
type FontMetrics struct {
	GlyphWidth  float64
	GlyphHeight float64
	LineHeight  float64
	Ascent      float64
	Descent     float64
}

// ScaledFont holds a font face with its calculated metrics
type ScaledFont struct {
	face    font.Face
	metrics FontMetrics
	ttFont  *opentype.Font // Keep reference for scaling
	ptSize  float64        // Punto TrueType elegido para face (1x, sin escalar)
}

// scaledFaceEntry es una face TrueType ya calibrada para una combinación de
// escala uniforme (scaleW == scaleH), junto con el punto TrueType con el
// que se generó — hace falta guardar el punto para poder pedir después una
// variante sobremuestreada de la misma face (ver drawSupersampledGlyph).
type scaledFaceEntry struct {
	face   font.Face
	ptSize float64
}

// fontSupersample es el factor de sobremuestreo con el que se dibuja cada
// glyph antes de reducirlo al tamaño final (ver drawSupersampledGlyph).
// Renderizar el contorno antialiaseado a mayor resolución y después
// reducirlo conserva más forma del glyph real que rasterizarlo directo al
// tamaño final de 203 DPI, que es lo que se veía "sucio"/poco nítido en la
// impresora térmica real.
const fontSupersample = 3.0

// FontManager handles font loading and scaling for thermal printer emulation
type FontManager struct {
	fonts map[string]*ScaledFont
	// FIXME: he scaledFaces map is accessed from multiple methods without synchronization.
	scaledFaces map[string]scaledFaceEntry // Cache:  "fontName_scaleW_scaleH" -> face+ptSize
	hiResFaces  map[string]font.Face       // Cache: "fontName_hi_ptSize" -> face sobremuestreada
	useFallback bool
}

// NewFontManager creates a new FontManager instance
func NewFontManager() *FontManager {
	return &FontManager{
		fonts:       make(map[string]*ScaledFont),
		scaledFaces: make(map[string]scaledFaceEntry),
		hiResFaces:  make(map[string]font.Face),
		useFallback: false,
	}
}

// LoadFont loads and calibrates a font to match target pixel dimensions
func (fm *FontManager) LoadFont(name, filename string, targetWidth, targetHeight float64) error {
	ttfData, err := emfonts.LoadFontData(filename)
	if err != nil {
		log.Printf("[FontManager] ERROR:  Failed to load font data for '%s': %v", name, err)
		fm.useFallback = true
		return fmt.Errorf("loading font data: %w", err)
	}

	f, err := opentype.Parse(ttfData)
	if err != nil {
		log.Printf("[FontManager] ERROR:  Failed to parse TTF for '%s': %v", name, err)
		fm.useFallback = true
		return fmt.Errorf("parsing ttf: %w", err)
	}

	// Heuristic search for optimal font size
	var bestFace font.Face
	bestSize := 0.0
	minDiff := math.MaxFloat64

	// Search range for thermal printer fonts (6pt to 72pt)
	for size := 6.0; size <= 72.0; size += 0.5 {
		opts := &opentype.FaceOptions{
			Size:    size,
			DPI:     72.0,
			Hinting: font.HintingFull,
		}
		face, err := opentype.NewFace(f, opts)
		if err != nil {
			continue
		}

		// Use 'M' as reference for width in monospace
		advance, ok := face.GlyphAdvance('M')
		if !ok {
			continue
		}

		currentWidth := float64(advance) / 64.0
		metrics := face.Metrics()
		currentHeight := float64(metrics.Height) / 64.0

		// Calculate difference - prioritize width match
		widthDiff := math.Abs(currentWidth - targetWidth)
		heightDiff := math.Abs(currentHeight - targetHeight)
		totalDiff := widthDiff*2 + heightDiff // Weight width more

		if totalDiff < minDiff {
			minDiff = totalDiff
			bestFace = face
			bestSize = size
		}
	}

	// Accept if we found ANY face (we'll use target metrics for spacing)
	if bestFace == nil {
		log.Printf("[FontManager] ERROR: Could not create any font face for '%s'", name)
		fm.useFallback = true
		return fmt.Errorf("could not fit font %s to dimensions %.2fx%.2f", filename, targetWidth, targetHeight)
	}

	// Store with TARGET metrics for consistent spacing (important!)
	fm.fonts[name] = &ScaledFont{
		face:   bestFace,
		ttFont: f,
		ptSize: bestSize,
		metrics: FontMetrics{
			GlyphWidth:  targetWidth,
			GlyphHeight: targetHeight,
			LineHeight:  targetHeight + 6,
			Ascent:      targetHeight * 0.8,
			Descent:     targetHeight * 0.2,
		},
	}

	// Pre-cache the 1x1 scale for this font
	cacheKey := fmt.Sprintf("%s_1.0_1.0", name)
	fm.scaledFaces[cacheKey] = scaledFaceEntry{face: bestFace, ptSize: bestSize}

	return nil
}

// GetFont retrieves a loaded font by name
func (fm *FontManager) GetFont(name string) (*ScaledFont, error) {
	if f, ok := fm.fonts[name]; ok {
		return f, nil
	}
	return nil, fmt.Errorf("font %s not loaded", name)
}

// GetMetrics returns metrics for a font, using fallback if necessary
func (fm *FontManager) GetMetrics(name string) FontMetrics {
	if f, ok := fm.fonts[name]; ok {
		return f.metrics
	}
	// Return default metrics for fallback
	if name == "B" {
		return FontMetrics{
			GlyphWidth:  constants.FontBWidth,
			GlyphHeight: constants.FontBHeight,
			LineHeight:  constants.FontBHeight + 4,
			Ascent:      constants.FontBHeight * 0.8,
			Descent:     constants.FontBHeight * 0.2,
		}
	}
	return FontMetrics{
		GlyphWidth:  constants.FontAWidth,
		GlyphHeight: constants.FontAHeight,
		LineHeight:  constants.FontAHeight + 6,
		Ascent:      constants.FontAHeight * 0.8,
		Descent:     constants.FontAHeight * 0.2,
	}
}

// GetScaledMetrics returns font metrics adjusted for the given scale factors
func (fm *FontManager) GetScaledMetrics(name string, scaleW, scaleH float64) FontMetrics {
	base := fm.GetMetrics(name)
	return FontMetrics{
		GlyphWidth:  base.GlyphWidth * scaleW,
		GlyphHeight: base.GlyphHeight * scaleH,
		LineHeight:  base.LineHeight * scaleH,
		Ascent:      base.Ascent * scaleH,
		Descent:     base.Descent * scaleH,
	}
}

// GetOrCreateScaledFace returns a cached scaled font face for uniform
// scaling (scaleW == scaleH), creating it if necessary. Devuelve también el
// punto TrueType elegido, necesario para pedir después la variante
// sobremuestreada de la misma face (ver hiResFace/drawSupersampledGlyph).
// Solo se usa para escala uniforme: para escala anisotrópica (scaleW !=
// scaleH, p.ej. size="medium" en el schema = ancho×1 alto×2) elegir una
// face de mayor punto ensancha el glyph tanto como lo alarga, y el cursor
// solo avanza width*scaleW → las letras quedan pisadas. Ver DrawCharScaled.
func (fm *FontManager) GetOrCreateScaledFace(fontName string, scaleW, scaleH float64) (font.Face, float64, error) {
	// Generate cache key
	key := fmt.Sprintf("%s_%.1f_%.1f", fontName, scaleW, scaleH)

	// Check cache first
	if entry, ok := fm.scaledFaces[key]; ok {
		return entry.face, entry.ptSize, nil
	}

	// Get the base font
	sf, err := fm.GetFont(fontName)
	if err != nil {
		return nil, 0, err
	}

	// Calculate target height for the scaled font
	// Use the larger scale factor to determine the font size
	scaleFactor := scaleH
	if scaleW > scaleH {
		scaleFactor = scaleW
	}
	targetHeight := sf.metrics.GlyphHeight * scaleFactor

	// Search for the best matching font size
	var bestFace font.Face
	bestSize := 0.0
	minDiff := math.MaxFloat64

	// Extended range for larger scales (up to 8x means we need larger sizes)
	maxSize := 72.0 + (scaleFactor * 20.0)
	if maxSize > 200.0 {
		maxSize = 200.0
	}

	for size := 6.0; size <= maxSize; size += 0.5 {
		opts := &opentype.FaceOptions{
			Size:    size,
			DPI:     72.0,
			Hinting: font.HintingFull,
		}
		face, err := opentype.NewFace(sf.ttFont, opts)
		if err != nil {
			continue
		}

		metrics := face.Metrics()
		currentHeight := float64(metrics.Height) / 64.0

		diff := math.Abs(currentHeight - targetHeight)
		if diff < minDiff {
			minDiff = diff
			bestFace = face
			bestSize = size
		}

		// Early exit if we found a very close match
		if diff < 0.5 {
			break
		}
	}

	if bestFace == nil {
		return nil, 0, fmt.Errorf("could not create scaled face for %s at %.1fx%.1f", fontName, scaleW, scaleH)
	}

	// Cache the result
	fm.scaledFaces[key] = scaledFaceEntry{face: bestFace, ptSize: bestSize}
	log.Printf("[FontManager] Created and cached scaled face:  %s at %.1fx%.1f", fontName, scaleW, scaleH)

	return bestFace, bestSize, nil
}

// hiResFace returns a cached font face rendered at fontSupersample× the
// given point size, for supersampled glyph drawing (ver
// drawSupersampledGlyph).
func (fm *FontManager) hiResFace(fontName string, ttFont *opentype.Font, ptSize float64) (font.Face, error) {
	key := fmt.Sprintf("%s_hi_%.2f", fontName, ptSize)
	if face, ok := fm.hiResFaces[key]; ok {
		return face, nil
	}

	face, err := opentype.NewFace(ttFont, &opentype.FaceOptions{
		Size:    ptSize * fontSupersample,
		DPI:     72.0,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, err
	}

	fm.hiResFaces[key] = face
	return face, nil
}

// DrawChar draws a single character at the specified position (1x1 scale)
// Returns the advance width
func (fm *FontManager) DrawChar(dst draw.Image, fontName string, char rune, x, y int, col color.Color) float64 {
	return fm.DrawCharScaled(dst, fontName, char, x, y, 1.0, 1.0, col)
}

// DrawCharScaled draws a character with the specified scaling factors.
// Returns the advance width (scaled).
//
// El glyph siempre se dibuja primero con una face sobremuestreada
// (fontSupersample×) y después se reduce a su tamaño final con un filtro
// de calidad (ver drawSupersampledGlyph) — eso mejora la nitidez percibida
// en la impresora térmica (203 DPI fijo) y, para escala anisotrópica
// (scaleW != scaleH), hace la deformación por eje a partir de la face base
// sin escalar, que es lo que mantiene el ancho dibujado en línea con el
// avance de cursor (ver GetOrCreateScaledFace).
func (fm *FontManager) DrawCharScaled(dst draw.Image, fontName string, char rune, x, y int, scaleW, scaleH float64, col color.Color) float64 {
	metrics := fm.GetScaledMetrics(fontName, scaleW, scaleH)
	targetW, targetH := metrics.GlyphWidth, metrics.GlyphHeight

	if fm.useFallback {
		fm.drawFallbackCharScaled(dst, char, x, y, int(targetW), int(targetH), col)
		return targetW
	}

	sf, err := fm.GetFont(fontName)
	if err != nil {
		fm.drawFallbackCharScaled(dst, char, x, y, int(targetW), int(targetH), col)
		return targetW
	}

	var srcPtSize, srcBoxW, srcBoxH float64
	if scaleW == scaleH {
		_, ptSize, ferr := fm.GetOrCreateScaledFace(fontName, scaleW, scaleH)
		if ferr != nil {
			fm.drawFallbackCharScaled(dst, char, x, y, int(targetW), int(targetH), col)
			return targetW
		}
		srcPtSize, srcBoxW, srcBoxH = ptSize, targetW, targetH
	} else {
		srcPtSize, srcBoxW, srcBoxH = sf.ptSize, sf.metrics.GlyphWidth, sf.metrics.GlyphHeight
	}

	hiFace, err := fm.hiResFace(fontName, sf.ttFont, srcPtSize)
	if err != nil {
		fm.drawFallbackCharScaled(dst, char, x, y, int(targetW), int(targetH), col)
		return targetW
	}

	drawSupersampledGlyph(dst, hiFace, char, x, y, srcBoxW, srcBoxH, targetW, targetH, col)
	return targetW
}

// drawSupersampledGlyph renders char with hiFace (ya sobremuestreada,
// calibrada para producir naturalmente una caja de srcBoxW×srcBoxH a 1x) en
// un tile temporal a fontSupersample× esa caja, y después reduce ese tile
// sobre dst en la caja final targetW×targetH con un filtro de calidad
// (CatmullRom). Reducir un contorno antialiaseado renderizado a mayor
// resolución conserva más forma real del glyph que rasterizarlo directo al
// tamaño final de 203 DPI. Cuando srcBox y targetBox tienen relación de
// aspecto distinta (escala anisotrópica: scaleW != scaleH), el propio
// resize hace el estiramiento por eje correcto.
func drawSupersampledGlyph(dst draw.Image, hiFace font.Face, char rune, x, y int, srcBoxW, srcBoxH, targetW, targetH float64, col color.Color) {
	tileW := int(math.Ceil(srcBoxW * fontSupersample))
	tileH := int(math.Ceil(srcBoxH * fontSupersample))
	if tileW < 1 {
		tileW = 1
	}
	if tileH < 1 {
		tileH = 1
	}

	tile := image.NewRGBA(image.Rect(0, 0, tileW, tileH))
	draw.Draw(tile, tile.Bounds(), &image.Uniform{C: colorWhite}, image.Point{}, draw.Src)

	d := &font.Drawer{
		Dst:  tile,
		Src:  image.NewUniform(col),
		Face: hiFace,
		Dot:  fixed.Point26_6{X: 0, Y: fixed.I(tileH)},
	}
	d.DrawString(string(char))

	dstW := int(math.Ceil(targetW))
	dstH := int(math.Ceil(targetH))
	if dstW < 1 {
		dstW = 1
	}
	if dstH < 1 {
		dstH = 1
	}

	destRect := image.Rect(x, y-dstH, x+dstW, y)
	xdraw.CatmullRom.Scale(dst, destRect, tile, tile.Bounds(), xdraw.Over, nil)
}

// drawFallbackCharScaled renders a bitmap character scaled to fit the given dimensions
func (fm *FontManager) drawFallbackCharScaled(dst draw.Image, char rune, x, y, w, h int, col color.Color) {
	pattern := getFallbackPattern(char)

	// The 5x7 bitmap pattern needs to be scaled to fit w x h
	pixelW := w / 5 // 5 columns in the pattern
	pixelH := h / 7 // 7 rows in the pattern
	if pixelW < 1 {
		pixelW = 1
	}
	if pixelH < 1 {
		pixelH = 1
	}

	for py := 0; py < 7; py++ {
		for px := 0; px < 5; px++ {
			if pattern[py]&(1<<(4-px)) != 0 {
				// Draw scaled pixel
				for sy := 0; sy < pixelH; sy++ {
					for sx := 0; sx < pixelW; sx++ {
						drawX := x + px*pixelW + sx
						drawY := y - h + py*pixelH + sy
						dst.Set(drawX, drawY, col)
					}
				}
			}
		}
	}
}

// FIXME: ClearScaledFaceCache recreates the map but doesn't close the existing font.Face resources.

// ClearScaledFaceCache clears the cached scaled font faces
// Useful when resetting the engine or changing fonts
func (fm *FontManager) ClearScaledFaceCache() {
	fm.scaledFaces = make(map[string]scaledFaceEntry)
	fm.hiResFaces = make(map[string]font.Face)
}
