package queue

import (
	"image"

	"usqay-print-client/internal/htmlrender"
	"usqay-print-client/internal/printer"
)

// RenderToImage renderiza un payload JSON a un objeto image.Image en memoria
// delegando en el motor unificado de htmlrender.
func RenderToImage(prof *printer.DeviceProfile, payloadJSON string) (image.Image, error) {
	return htmlrender.RenderPayloadToImage(prof, payloadJSON)
}

// RenderToPNG renderiza un payload JSON y lo guarda en el archivo PNG especificado
// delegando en el motor unificado de htmlrender.
func RenderToPNG(prof *printer.DeviceProfile, payloadJSON string, outputPath string) error {
	return htmlrender.RenderPayloadToPNGFile(prof, payloadJSON, outputPath)
}

// RenderThermal convierte un PrintPayload JSON en bytes ESC/POS usando el
// motor unificado de htmlrender.
func RenderThermal(prof *printer.DeviceProfile, payloadJSON string) ([]byte, error) {
	return htmlrender.RenderThermal(prof, payloadJSON)
}
