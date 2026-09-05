package queue

import (
	"image"

	"usqay-print-client/internal/printer"
	"usqay-print-client/internal/ticketrender"
)

// RenderToImage renderiza un payload JSON a un objeto image.Image en memoria
// delegando en el motor unificado de ticketrender.
func RenderToImage(prof *printer.DeviceProfile, payloadJSON string) (image.Image, error) {
	return ticketrender.RenderPayloadToImage(prof, payloadJSON)
}

// RenderToPNG renderiza un payload JSON y lo guarda en el archivo PNG especificado
// delegando en el motor unificado de ticketrender.
func RenderToPNG(prof *printer.DeviceProfile, payloadJSON string, outputPath string) error {
	return ticketrender.RenderPayloadToPNGFile(prof, payloadJSON, outputPath)
}

// RenderThermal convierte un PrintPayload JSON en bytes ESC/POS usando el
// motor unificado de ticketrender.
func RenderThermal(prof *printer.DeviceProfile, payloadJSON string) ([]byte, error) {
	return ticketrender.RenderThermal(prof, payloadJSON)
}
