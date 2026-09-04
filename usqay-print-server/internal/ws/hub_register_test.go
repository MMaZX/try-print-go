package ws

import (
	"context"
	"testing"
	"time"

	"usqay-print-server/internal/config"
)

// newTestClient builds a minimal Client suitable for exercising Hub.Register
// and Kick without a real WebSocket connection.
func newTestClient(hub *Hub) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		hub:    hub,
		send:   make(chan any, 32),
		ctx:    ctx,
		cancel: cancel,
	}
}

// TestHub_Register_AlwaysKicksOldNeverRejectsNew cubre la regla de negocio
// documentada en CLAUDE.md sección 4: si un terminal_id ya tiene una
// conexión activa, el servidor patea la anterior y registra la nueva —
// nunca rechaza la nueva. Antes de este fix, Register rechazaba la conexión
// nueva cuando la vieja respondía a un VerifyAlive, produciendo un EOF crudo
// del lado del cliente en cada reintento.
func TestHub_Register_AlwaysKicksOldNeverRejectsNew(t *testing.T) {
	hub := NewHub(&config.Config{})

	oldClient := newTestClient(hub)
	hub.Register("empresa-a", "1", oldClient)

	newClient := newTestClient(hub)
	hub.Register("empresa-a", "1", newClient)

	hub.mu.RLock()
	current := hub.clients[agentKey{businessID: "empresa-a", terminalID: "1"}]
	hub.mu.RUnlock()
	if current != newClient {
		t.Fatalf("cliente activo tras el segundo Register no es el nuevo — la conexión nueva no debería ser rechazada nunca")
	}

	select {
	case msg := <-oldClient.send:
		kick, ok := msg.(KickMsg)
		if !ok {
			t.Fatalf("mensaje enviado a la conexión vieja = %T, want KickMsg", msg)
		}
		if kick.Type != TypeKick {
			t.Errorf("KickMsg.Type = %q, want %q", kick.Type, TypeKick)
		}
	case <-time.After(time.Second):
		t.Fatal("la conexión vieja no recibió un mensaje de kick")
	}

	select {
	case <-oldClient.ctx.Done():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("el contexto de la conexión vieja no se canceló tras el kick")
	}
}

// TestHub_Register_SameTerminalIDDifferentBusiness_NoCollision cubre el bug
// de multi-tenancy real: terminal_id es autoincrement por empresa (BD tenant),
// así que dos empresas distintas legítimamente pueden tener ambas un
// terminal_id="1". Antes de la key compuesta, ambas conexiones peleaban por
// el mismo slot del Hub — se pateaban entre sí en loop infinito y, peor,
// EnqueueLaravel podía enrutar un trabajo a la impresora de la otra empresa.
func TestHub_Register_SameTerminalIDDifferentBusiness_NoCollision(t *testing.T) {
	hub := NewHub(&config.Config{})

	clientA := newTestClient(hub)
	hub.Register("empresa-a", "1", clientA)

	clientB := newTestClient(hub)
	hub.Register("empresa-b", "1", clientB)

	hub.mu.RLock()
	stillA := hub.clients[agentKey{businessID: "empresa-a", terminalID: "1"}]
	stillB := hub.clients[agentKey{businessID: "empresa-b", terminalID: "1"}]
	hub.mu.RUnlock()

	if stillA != clientA {
		t.Errorf("empresa-a:1 fue desplazada por el registro de empresa-b:1 — no deberían colisionar")
	}
	if stillB != clientB {
		t.Errorf("empresa-b:1 no quedó registrada correctamente")
	}

	// Ninguna de las dos debería haber recibido un kick.
	select {
	case msg := <-clientA.send:
		t.Errorf("empresa-a:1 recibió un mensaje inesperado (no debería haber sido pateada): %#v", msg)
	default:
	}
	select {
	case msg := <-clientB.send:
		t.Errorf("empresa-b:1 recibió un mensaje inesperado (no debería haber sido pateada): %#v", msg)
	default:
	}
}
