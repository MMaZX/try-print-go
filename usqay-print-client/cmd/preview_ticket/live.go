package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"usqay-print-client/internal/htmlrender"
	"usqay-print-client/internal/printer"
)

// livePollInterval es cada cuánto se consulta el mtime/tamaño del .json.
// Un archivo suelto no justifica fsnotify; el polling mantiene cero dependencias
// y queda naturalmente "debounced" a este intervalo.
const livePollInterval = 300 * time.Millisecond

// liveMeta son las métricas de un render que la página muestra en la barra
// superior y que también sirven de sonda de estado (campo Error).
type liveMeta struct {
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	RenderMS    float64 `json:"render_ms"`
	DispatchMS  float64 `json:"dispatch_ms"`
	PNGBytes    int     `json:"png_bytes"`
	ESCPOSBytes int     `json:"escpos_bytes"`
	Error       string  `json:"error,omitempty"`
	RenderedAt  string  `json:"rendered_at"`
}

// runLive es el punto de entrada del modo --live. Prepara el motor Chromium
// persistente y despacha al servidor HTTP o al regenerador de PNG.
func runLive(jsonPath string, port int, pngOnly bool) error {
	if _, err := htmlrender.GetDefaultEngine(); err != nil {
		return fmt.Errorf("inicializar motor de render: %w", err)
	}
	defer htmlrender.CloseDefaultEngine()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if pngOnly {
		return runLivePNG(ctx, jsonPath)
	}
	return runLiveServer(ctx, jsonPath, port)
}

// renderOnce lee el .json del disco y lo renderiza a PNG en memoria, midiendo
// el render y el despacho ESC/POS. Nunca hace panic: un JSON a medio guardar o
// inválido vuelve como error y se refleja en meta.Error.
func renderOnce(jsonPath string) ([]byte, liveMeta, error) {
	meta := liveMeta{RenderedAt: time.Now().Format("15:04:05")}

	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		meta.Error = fmt.Sprintf("leer JSON: %v", err)
		return nil, meta, err
	}

	renderStart := time.Now()
	img, err := htmlrender.RenderPayloadToImage(nil, string(raw))
	if err != nil {
		meta.Error = fmt.Sprintf("renderizar: %v", err)
		return nil, meta, err
	}
	meta.RenderMS = msSince(renderStart)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		meta.Error = fmt.Sprintf("codificar PNG: %v", err)
		return nil, meta, err
	}

	bounds := img.Bounds()
	meta.Width = bounds.Dx()
	meta.Height = bounds.Dy()
	meta.PNGBytes = buf.Len()

	// Despacho ESC/POS a partir de la imagen ya en RAM (mismo cálculo que el
	// modo benchmark), útil para vigilar el peso real del ticket.
	dispatchStart := time.Now()
	_, _, payload, _ := htmlrender.BuildHTMLFromJSON(nil, string(raw))
	if payload != nil {
		activeProf := printer.Default58mmProfile()
		if payload.PaperProperties.Width > 0 {
			activeProf = printer.DeriveProfileFromWidth(payload.PaperProperties.Width)
		}
		if escBytes, escErr := htmlrender.ConvertImageToESC(activeProf, payload, img); escErr == nil {
			meta.ESCPOSBytes = len(escBytes)
		}
	}
	meta.DispatchMS = msSince(dispatchStart)

	return buf.Bytes(), meta, nil
}

func msSince(t time.Time) float64 {
	return float64(time.Since(t).Microseconds()) / 1000.0
}

// fileSig es una huella barata (mtime + tamaño) para detectar cambios y para
// cachear el último render.
func fileSig(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return "missing"
	}
	return fmt.Sprintf("%d-%d", fi.ModTime().UnixNano(), fi.Size())
}

// watchFile llama a onChange cada vez que cambia la huella de path. Tolera que
// el archivo desaparezca un instante (los editores guardan con rename atómico).
func watchFile(ctx context.Context, path string, onChange func()) {
	last := fileSig(path)
	ticker := time.NewTicker(livePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sig := fileSig(path)
			if sig == "missing" || sig == last {
				continue
			}
			last = sig
			onChange()
		}
	}
}

// --- Modo --png-only ---

func runLivePNG(ctx context.Context, jsonPath string) error {
	outPath := strings.TrimSuffix(jsonPath, filepath.Ext(jsonPath)) + ".png"

	render := func() {
		pngBytes, meta, err := renderOnce(jsonPath)
		if err != nil {
			fmt.Printf("⚠️  %s  %s\n", meta.RenderedAt, meta.Error)
			return
		}
		if err := os.WriteFile(outPath, pngBytes, 0644); err != nil {
			fmt.Printf("⚠️  %s  escribir PNG: %v\n", meta.RenderedAt, err)
			return
		}
		fmt.Printf("✅ %s  %s  %dx%d px  %.1f KB  render %.1f ms\n",
			meta.RenderedAt, filepath.Base(outPath), meta.Width, meta.Height,
			float64(meta.PNGBytes)/1024.0, meta.RenderMS)
	}

	fmt.Printf("👀 Observando %s → %s  (Ctrl+C para salir)\n", jsonPath, outPath)
	render()
	watchFile(ctx, jsonPath, render)
	return nil
}

// --- Modo servidor HTTP ---

// sseHub reparte una señal de "recargá" a todas las pestañas conectadas a /events.
type sseHub struct {
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

func newSSEHub() *sseHub {
	return &sseHub{subs: make(map[chan struct{}]struct{})}
}

func (h *sseHub) add() chan struct{} {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *sseHub) remove(ch chan struct{}) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
}

func (h *sseHub) broadcast() {
	h.mu.Lock()
	for ch := range h.subs {
		select {
		case ch <- struct{}{}:
		default: // el cliente todavía no consumió la señal anterior; alcanza con una
		}
	}
	h.mu.Unlock()
}

// renderCache memoriza el último render por huella de archivo para que la
// pareja de peticiones /meta + /preview.png de cada refresco no renderice dos
// veces el mismo contenido.
type renderCache struct {
	mu   sync.Mutex
	sig  string
	png  []byte
	meta liveMeta
	err  error
}

func (c *renderCache) get(path string) ([]byte, liveMeta, error) {
	sig := fileSig(path)

	c.mu.Lock()
	defer c.mu.Unlock()
	if sig == c.sig && (c.png != nil || c.err != nil) {
		return c.png, c.meta, c.err
	}

	pngBytes, meta, err := renderOnce(path)
	c.sig, c.png, c.meta, c.err = sig, pngBytes, meta, err
	return pngBytes, meta, err
}

func runLiveServer(ctx context.Context, jsonPath string, port int) error {
	hub := newSSEHub()
	cache := &renderCache{}
	pageHTML := strings.ReplaceAll(livePageHTML, "{{FILE}}", filepath.Base(jsonPath))

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(pageHTML))
	})

	mux.HandleFunc("/preview.png", func(w http.ResponseWriter, r *http.Request) {
		pngBytes, _, err := cache.get(jsonPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(pngBytes)
	})

	mux.HandleFunc("/meta", func(w http.ResponseWriter, r *http.Request) {
		_, meta, _ := cache.get(jsonPath)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(meta)
	})

	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming no soportado", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Connection", "keep-alive")

		ch := hub.add()
		defer hub.remove(ch)

		fmt.Fprint(w, "retry: 1000\n\n")
		flusher.Flush()

		keepAlive := time.NewTicker(15 * time.Second)
		defer keepAlive.Stop()

		for {
			select {
			case <-r.Context().Done():
				return
			case <-ctx.Done():
				return
			case <-ch:
				fmt.Fprint(w, "data: reload\n\n")
				flusher.Flush()
			case <-keepAlive.C:
				fmt.Fprint(w, ": keepalive\n\n")
				flusher.Flush()
			}
		}
	})

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("abrir puerto %d: %w", port, err)
	}

	srv := &http.Server{Handler: mux}

	go watchFile(ctx, jsonPath, hub.broadcast)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	fmt.Printf("🎨 Vista previa en vivo: http://%s\n", ln.Addr().String())
	fmt.Printf("👀 Observando %s  (Ctrl+C para salir)\n", jsonPath)

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// livePageHTML es la página de vista previa. {{FILE}} se sustituye por el
// nombre del archivo observado. Sin dependencias: EventSource nativo, y ante
// cada aviso vuelve a pedir /meta y /preview.png con un cache-buster.
const livePageHTML = `<!doctype html>
<html lang="es">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Preview · {{FILE}}</title>
<style>
  :root { color-scheme: light dark; }
  * { box-sizing: border-box; }
  body { margin: 0; font: 13px/1.4 system-ui, -apple-system, Segoe UI, sans-serif; background: #5c5c5c; color: #f0f0f0; }
  header { position: sticky; top: 0; z-index: 2; display: flex; flex-wrap: wrap; gap: 6px 16px; align-items: baseline;
           padding: 8px 14px; background: #222; border-bottom: 1px solid #000; }
  header .file { font-weight: 600; }
  header .m { opacity: .72; font-variant-numeric: tabular-nums; }
  header .stamp { margin-left: auto; opacity: .72; font-variant-numeric: tabular-nums; }
  header .dot { width: 8px; height: 8px; border-radius: 50%; background: #4caf50; align-self: center; }
  header .dot.err { background: #ff5252; }
  #err { display: none; margin: 0; padding: 10px 14px; background: #3a1f1f; color: #ff8a80;
         white-space: pre-wrap; font-family: ui-monospace, Menlo, Consolas, monospace; font-size: 12px; }
  main { display: flex; justify-content: center; padding: 24px 16px 48px; }
  #preview { background: #fff; box-shadow: 0 6px 30px rgba(0,0,0,.45); max-width: 100%; height: auto;
             image-rendering: pixelated; }
</style>
</head>
<body>
<header>
  <span class="dot" id="dot"></span>
  <span class="file">{{FILE}}</span>
  <span class="m" id="dims">—</span>
  <span class="m" id="timings">—</span>
  <span class="m" id="escpos">—</span>
  <span class="stamp" id="stamp">—</span>
</header>
<pre id="err"></pre>
<main><img id="preview" alt="vista previa del ticket"></main>
<script>
  var img = document.getElementById('preview');
  var err = document.getElementById('err');
  var dot = document.getElementById('dot');

  function refresh() {
    var v = Date.now();
    fetch('/meta?v=' + v, { cache: 'no-store' })
      .then(function (r) { return r.json(); })
      .then(function (m) {
        if (m.error) {
          dot.classList.add('err');
          err.style.display = 'block';
          err.textContent = m.error;
          document.getElementById('stamp').textContent = m.rendered_at || '';
          return;
        }
        dot.classList.remove('err');
        err.style.display = 'none';
        img.src = '/preview.png?v=' + v;
        document.getElementById('dims').textContent = m.width + '×' + m.height + ' px';
        document.getElementById('timings').textContent = 'render ' + m.render_ms.toFixed(1) + ' ms';
        document.getElementById('escpos').textContent = (m.escpos_bytes / 1024).toFixed(1) + ' KB ESC/POS';
        document.getElementById('stamp').textContent = m.rendered_at;
      })
      .catch(function (e) {
        dot.classList.add('err');
        err.style.display = 'block';
        err.textContent = String(e);
      });
  }

  refresh();
  var es = new EventSource('/events');
  es.onmessage = refresh;
</script>
</body>
</html>`
