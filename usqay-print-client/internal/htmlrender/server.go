package htmlrender

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"usqay-print-client/internal/htmlrender/fonts"
)

// assetServer sirve, sobre un loopback HTTP local, tanto el documento HTML de cada
// ticket como sus assets estáticos (la fuente TTF embebida). Al navegar Chromium a
// http://127.0.0.1:<port>/ticket/<token> el documento y la fuente comparten origen,
// lo que evita los bloqueos de CORS y de Private Network Access que Chrome/Edge
// recientes aplican a las subpeticiones originadas desde una URL `data:`.
type assetServer struct {
	base    string
	mu      sync.Mutex
	pages   map[string]string
	counter atomic.Uint64
}

var (
	sharedAssetServer *assetServer
	assetServerOnce   sync.Once
	assetServerErr    error
)

// getAssetServer inicializa (una sola vez) el servidor loopback de assets.
func getAssetServer() (*assetServer, error) {
	assetServerOnce.Do(func() {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			assetServerErr = fmt.Errorf("abrir listener loopback para assets: %w", err)
			return
		}

		s := &assetServer{
			base:  "http://" + ln.Addr().String(),
			pages: make(map[string]string),
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/assets/font.ttf", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "font/ttf")
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			_, _ = w.Write(fonts.CaskaydiaCoveTTF)
		})
		mux.HandleFunc("/ticket/", func(w http.ResponseWriter, r *http.Request) {
			token := strings.TrimPrefix(r.URL.Path, "/ticket/")
			s.mu.Lock()
			html, ok := s.pages[token]
			s.mu.Unlock()
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = io.WriteString(w, html)
		})

		srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		go func() { _ = srv.Serve(ln) }()

		sharedAssetServer = s
	})

	if assetServerErr != nil {
		return nil, assetServerErr
	}
	return sharedAssetServer, nil
}

// publish registra el HTML de un ticket y devuelve su URL absoluta junto con un
// token para liberarlo una vez capturado.
func (s *assetServer) publish(html string) (pageURL, token string) {
	token = strconv.FormatUint(s.counter.Add(1), 10)
	s.mu.Lock()
	s.pages[token] = html
	s.mu.Unlock()
	return s.base + "/ticket/" + token, token
}

// unpublish descarta el HTML asociado a un token.
func (s *assetServer) unpublish(token string) {
	s.mu.Lock()
	delete(s.pages, token)
	s.mu.Unlock()
}
