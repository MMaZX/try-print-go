// Package fonts embeds the TrueType font(s) used by ticketimage to render
// receipts as raster images. Vendorizado desde
// github.com/adcondev/poster@v1.8.1-0.20260302085655-dd5fdc173b7d
// (pkg/emulator/fonts, MIT) con el único cambio de reemplazar las fuentes
// empaquetadas por la que pidió el negocio: Cascadia Code (variante Mono del
// parche Nerd Fonts, para que el ancho de carácter sea realmente fijo y no
// desalinee tablas/columnas). Fuente bajo SIL OFL 1.1, ver
// LICENSE-CascadiaCode-OFL.txt en este mismo directorio.
package fonts

import (
	"embed"
	"fmt"
)

//go:embed *.ttf
var fs embed.FS

// LoadFontData loads font data from embedded resources.
func LoadFontData(filename string) ([]byte, error) {
	data, err := fs.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("embedded font not found: %s: %w", filename, err)
	}
	return data, nil
}
