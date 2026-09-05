package ticketimage

import (
	"image/color"
	"image/draw"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/adcondev/poster/pkg/constants"
)

// TextStyle represents text formatting options for the emulator
type TextStyle struct {
	Bold      bool
	Underline int     // 0: none, 1: single, 2: double
	Inverse   bool    // White on black
	ScaleW    float64 // Width multiplier (1.0 - 8.0)
	ScaleH    float64 // Height multiplier (1.0 - 8.0)
}

// DefaultTextStyle returns a TextStyle with default values
func DefaultTextStyle() TextStyle {
	return TextStyle{
		Bold:      false,
		Underline: 0,
		Inverse:   false,
		ScaleW:    1.0,
		ScaleH:    1.0,
	}
}

// TextRenderer handles text rendering for the emulator
type TextRenderer struct {
	canvas *DynamicCanvas
	fonts  *FontManager
	state  *PrinterState
	black  color.Color
	white  color.Color
}

// NewTextRenderer creates a new TextRenderer
func NewTextRenderer(canvas *DynamicCanvas, fonts *FontManager, state *PrinterState) *TextRenderer {
	return &TextRenderer{
		canvas: canvas,
		fonts:  fonts,
		state:  state,
		black:  colorBlack,
		white:  colorWhite,
	}
}

// RenderText renders a string of text at the current cursor position
func (tr *TextRenderer) RenderText(text string) {
	if len(text) == 0 {
		return
	}

	// Filter control characters except common ones
	text = strings.Map(func(r rune) rune {
		if r < 32 && r != '\t' && r != '\n' && r != '\r' {
			return -1 // Remove character
		}
		return r
	}, text)

	// Get scaled metrics for current font and size
	metrics := tr.fonts.GetScaledMetrics(tr.state.FontName, tr.state.ScaleW, tr.state.ScaleH)
	charWidth := metrics.GlyphWidth
	charHeight := metrics.GlyphHeight

	// Count actual characters (runes), not bytes - important for UTF-8 text
	runeCount := utf8.RuneCountInString(text)

	// Calculate text width for alignment using rune count
	textWidth := float64(runeCount) * charWidth

	// Determine starting X position based on alignment
	startX := tr.calculateAlignedX(textWidth)

	// Ensure canvas has enough height
	requiredY := tr.state.CursorY + charHeight
	tr.canvas.EnsureHeight(requiredY)

	// Render each character
	x := startX
	for _, char := range text {
		tr.renderChar(char, x, tr.state.CursorY, charWidth, charHeight)
		x += charWidth
	}

	// Update cursor position
	tr.state.CursorX = x
	tr.canvas.UpdateMaxY(tr.state.CursorY + charHeight)
}

// RenderTextAt draws text at an explicit x offset on the current line,
// ignoring state.Align, and returns the x position right after the drawn
// text. Agregado local (no existe en el poster original): RenderText/
// RenderLine siempre recalculan su propio X según alineación y el ancho del
// string que reciben — no acumulan sobre un CursorX previo — así que no
// sirven para concatenar celdas de estilo mixto (negrita/tamaño distinto)
// en una misma fila de tabla/columnas. Esto sí acumula, igual que hacía la
// emisión de texto ESC/POS que reemplazamos (un byte stream concatena solo).
func (tr *TextRenderer) RenderTextAt(text string, x float64) float64 {
	if len(text) == 0 {
		return x
	}

	metrics := tr.fonts.GetScaledMetrics(tr.state.FontName, tr.state.ScaleW, tr.state.ScaleH)
	charWidth := metrics.GlyphWidth
	charHeight := metrics.GlyphHeight

	requiredY := tr.state.CursorY + charHeight
	tr.canvas.EnsureHeight(requiredY)

	cx := x
	for _, char := range text {
		tr.renderChar(char, cx, tr.state.CursorY, charWidth, charHeight)
		cx += charWidth
	}

	tr.canvas.UpdateMaxY(tr.state.CursorY + charHeight)
	return cx
}

// RenderLine renders text and moves to next line
func (tr *TextRenderer) RenderLine(text string) {
	tr.RenderText(text)
	tr.NewLine()
}

// FIXME: The NewLine method now directly manipulates cursor position instead of delegating to PrinterState.NewLine(metrics).
// This duplicates line height calculation logic.
// Consider whether the PrinterState.NewLine method should also be updated to use GetScaledMetrics,
// or if this logic should remain centralized in one place.

// NewLine moves to the beginning of the next line
func (tr *TextRenderer) NewLine() {
	metrics := tr.fonts.GetScaledMetrics(tr.state.FontName, tr.state.ScaleW, tr.state.ScaleH)
	lineHeight := metrics.LineHeight
	if lineHeight < tr.state.LineSpacing {
		lineHeight = tr.state.LineSpacing
	}
	tr.state.CursorY += lineHeight
	tr.state.CursorX = 0
}

// Feed advances paper by specified number of lines
func (tr *TextRenderer) Feed(lines int) {
	metrics := tr.fonts.GetMetrics(tr.state.FontName)
	tr.state.Feed(lines, metrics)
	tr.canvas.UpdateMaxY(tr.state.CursorY)
}

// calculateAlignedX calculates the X starting position based on alignment
func (tr *TextRenderer) calculateAlignedX(textWidth float64) float64 {
	paperWidth := float64(tr.state.PaperPxWidth)

	switch tr.state.Align {
	case constants.Center.String():
		return (paperWidth - textWidth) / 2
	case constants.Right.String():
		return paperWidth - textWidth
	default: // AlignLeft
		return 0
	}
}

// renderChar renders a single character with current style
func (tr *TextRenderer) renderChar(char rune, x, y, width, height float64) {
	// Handle inverse mode (white on black)
	if tr.state.IsInverse {
		// Draw black background
		tr.canvas.DrawRect(
			int(x), int(y-height),
			int(width)+1, int(height)+1,
			tr.black,
		)
		// Draw character in white
		tr.drawChar(char, x, y, tr.white)
	} else {
		// Normal:  black on white
		tr.drawChar(char, x, y, tr.black)
	}

	// Handle underline
	if tr.state.IsUnderline > 0 {
		underlineY := int(y) + 2
		thickness := tr.state.IsUnderline
		tr.canvas.DrawLine(int(x), underlineY, int(x+width), thickness, tr.black)
	}

	// Handle bold (draw twice with offset for extra weight)
	if tr.state.IsBold {
		if tr.state.IsInverse {
			tr.drawChar(char, x+1, y, tr.white)
		} else {
			tr.drawChar(char, x+1, y, tr.black)
		}
	}
}

// drawChar draws a character using TrueType fonts (scaled or unscaled) or
// vector primitives for kitchen and typographic symbols (like ↳ or •).
func (tr *TextRenderer) drawChar(char rune, x, y float64, col color.Color) {
	metrics := tr.fonts.GetScaledMetrics(tr.state.FontName, tr.state.ScaleW, tr.state.ScaleH)

	// Símbolos vectoriales nativos para comandas de cocina y notas
	if char == '↳' || char == '\u21b3' || char == '⤷' || char == '\u2937' {
		drawVectorArrow(tr.canvas.Image(), int(x), int(y), metrics.GlyphWidth, metrics.GlyphHeight, col)
		return
	}
	if char == '•' || char == '\u2022' {
		drawVectorBullet(tr.canvas.Image(), int(x), int(y), metrics.GlyphWidth, metrics.GlyphHeight, col)
		return
	}

	tr.fonts.DrawCharScaled(
		tr.canvas.Image(),
		tr.state.FontName,
		char,
		int(x),
		int(y),
		tr.state.ScaleW,
		tr.state.ScaleH,
		col,
	)
}

// drawVectorArrow dibuja una flecha de notas o modificadores de plato '↳'
// de forma vectorial nítida sobre el lienzo.
func drawVectorArrow(img draw.Image, x, y int, targetW, targetH float64, col color.Color) {
	w := targetW
	h := targetH
	thickness := int(math.Round(w * 0.14))
	if thickness < 2 {
		thickness = 2
	}

	startX := x + int(w*0.25)
	startY := y - int(h*0.72)
	cornerY := y - int(h*0.28)
	endX := x + int(w*0.82)

	// 1. Línea vertical que desciende
	for py := startY; py <= cornerY; py++ {
		for t := 0; t < thickness; t++ {
			img.Set(startX+t, py, col)
		}
	}

	// 2. Línea horizontal hacia la derecha
	for px := startX; px <= endX; px++ {
		for t := 0; t < thickness; t++ {
			img.Set(px, cornerY-t, col)
		}
	}

	// 3. Punta de flecha (apuntando a la derecha)
	arrowSize := int(w * 0.28)
	if arrowSize < 3 {
		arrowSize = 3
	}
	for i := 0; i <= arrowSize; i++ {
		for t := 0; t < thickness; t++ {
			img.Set(endX-i, (cornerY-i)+t, col)
			img.Set(endX-i, (cornerY+i)+t, col)
		}
	}
}

// drawVectorBullet dibuja una viñeta '•' centrada geométricamente.
func drawVectorBullet(img draw.Image, x, y int, targetW, targetH float64, col color.Color) {
	radius := int(targetW * 0.16)
	if radius < 2 {
		radius = 2
	}
	centerX := x + int(targetW*0.5)
	centerY := y - int(targetH*0.4)

	for dy := -radius; dy <= radius; dy++ {
		for dx := -radius; dx <= radius; dx++ {
			if dx*dx+dy*dy <= radius*radius {
				img.Set(centerX+dx, centerY+dy, col)
			}
		}
	}
}

// FIXME: The WrapText method now calculates charsPerLine directly instead of using state.CharsPerLine(charWidth).

// WrapText wraps text to fit within paper width and renders each line
func (tr *TextRenderer) WrapText(text string) {
	metrics := tr.fonts.GetScaledMetrics(tr.state.FontName, tr.state.ScaleW, tr.state.ScaleH)
	charWidth := metrics.GlyphWidth
	charsPerLine := int(float64(tr.state.PaperPxWidth) / charWidth)

	if charsPerLine <= 0 {
		charsPerLine = 1
	}

	// Split into words
	words := strings.Fields(text)
	if len(words) == 0 {
		return
	}

	var currentLine strings.Builder
	currentLen := 0

	for _, word := range words {
		// Count runes, not bytes - important for UTF-8 text
		wordLen := utf8.RuneCountInString(word)

		// If word is longer than line, split it
		if wordLen > charsPerLine {
			// Flush current line first
			if currentLen > 0 {
				tr.RenderLine(currentLine.String())
				currentLine.Reset()
				currentLen = 0
			}
			// Split long word by runes (not bytes)
			runes := []rune(word)
			for i := 0; i < len(runes); i += charsPerLine {
				end := i + charsPerLine
				if end > len(runes) {
					end = len(runes)
				}
				tr.RenderLine(string(runes[i:end]))
			}
			continue
		}

		// Check if word fits on current line
		spaceNeeded := wordLen
		if currentLen > 0 {
			spaceNeeded++ // space before word
		}

		if currentLen+spaceNeeded > charsPerLine {
			// Word doesn't fit - render current line and start new one
			tr.RenderLine(currentLine.String())
			currentLine.Reset()
			currentLine.WriteString(word)
			currentLen = wordLen
		} else {
			// Word fits
			if currentLen > 0 {
				currentLine.WriteByte(' ')
				currentLen++
			}
			currentLine.WriteString(word)
			currentLen += wordLen
		}
	}

	// Render remaining text
	if currentLen > 0 {
		tr.RenderLine(currentLine.String())
	}
}
