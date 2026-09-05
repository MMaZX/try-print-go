// Package ticketimage renders a ticket as a raster image using a real
// TrueType font (Cascadia Code, ver fonts/embed.go) instead of ESC/POS text
// commands interpreted by la fuente de firmware de la impresora. La imagen
// resultante (Engine.Render/WritePNG) se pasa por graphics.Pipeline
// (dithering) y se manda como GS v 0 igual que un logo — es el mismo camino
// que ya usábamos para imágenes, aplicado ahora a todo el ticket.
//
// Vendorizado desde github.com/adcondev/poster@v1.8.1-0.20260302085655-dd5fdc173b7d
// (pkg/emulator, MIT — ver LICENSE-poster-MIT.txt) porque esa librería no
// expone una forma de cargar una fuente externa a la suya propia: el
// go:embed de las fuentes vive dentro de su paquete privado. Se vendorizó
// para poder reemplazar esas fuentes por la que pidió el negocio
// (Cascadia Code) sin depender de que el autor original la agregue.
package ticketimage
