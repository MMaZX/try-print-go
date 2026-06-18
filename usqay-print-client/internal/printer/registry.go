package printer

import "sync"

// Registry maps impresora UUIDs (ImpresoraNameID) to live Printer instances.
// It is populated when the server sends a TypeConfig message after connecting,
// and read by the Worker on every print attempt.
type Registry struct {
	mu    sync.RWMutex
	store map[string]Printer
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{store: make(map[string]Printer)}
}

// Set registers a printer under the given UUID, replacing any previous entry.
func (r *Registry) Set(id string, p Printer) {
	r.mu.Lock()
	r.store[id] = p
	r.mu.Unlock()
}

// Clear removes all entries (called before loading a fresh config).
func (r *Registry) Clear() {
	r.mu.Lock()
	r.store = make(map[string]Printer)
	r.mu.Unlock()
}

// Resolve returns the Printer for the given UUID.
// If id is empty or not found but only one printer exists, that printer is
// returned as a fallback so single-printer setups work without explicit routing.
func (r *Registry) Resolve(id string) (Printer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if p, ok := r.store[id]; ok {
		return p, true
	}
	if len(r.store) == 1 {
		for _, p := range r.store {
			return p, true
		}
	}
	return nil, false
}

// Len returns the number of registered printers.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.store)
}

// IDs returns all registered printer IDs.
func (r *Registry) IDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.store))
	for id := range r.store {
		ids = append(ids, id)
	}
	return ids
}
