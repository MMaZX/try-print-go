package printer

import (
	"fmt"
	"net"
	"time"
)

const defaultTimeout = 5 * time.Second

// NetworkPrinter sends raw ESC/POS bytes over a direct TCP connection.
// Compatible with any ESC/POS printer that accepts raw data on port 9100.
type NetworkPrinter struct {
	addr    string
	timeout time.Duration
}

// NewNetworkPrinter creates a printer that connects to addr (e.g. "192.168.1.100:9100").
func NewNetworkPrinter(addr string) *NetworkPrinter {
	return &NetworkPrinter{addr: addr, timeout: defaultTimeout}
}

func (p *NetworkPrinter) Print(data []byte) error {
	conn, err := net.DialTimeout("tcp", p.addr, p.timeout)
	if err != nil {
		return fmt.Errorf("conectar a %s: %w", p.addr, err)
	}
	defer conn.Close()

	if err := conn.SetWriteDeadline(time.Now().Add(p.timeout)); err != nil {
		return fmt.Errorf("set write deadline: %w", err)
	}
	if _, err := conn.Write(data); err != nil {
		return fmt.Errorf("escribir a %s: %w", p.addr, err)
	}
	return nil
}
