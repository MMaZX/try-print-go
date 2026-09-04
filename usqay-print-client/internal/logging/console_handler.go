package logging

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/mattn/go-isatty"
)

// ANSI color codes for terminal output.
const (
	ansiReset   = "\033[0m"
	ansiGray    = "\033[90m"
	ansiCyan    = "\033[36m"
	ansiYellow  = "\033[33;1m"
	ansiRed     = "\033[31;1m"
	ansiGreen   = "\033[32;1m"
	ansiBlue    = "\033[34;1m"
	ansiMagenta = "\033[35;1m"
	ansiWhite   = "\033[97;1m"
)

// ConsoleHandler is a colorized slog.Handler for terminal output.
// It recognizes the "src" attribute and renders it as a colored source tag:
//
//	"CLIENTE" → green  [CLIENTE]
//	"SERVER"  → blue   [SERVER]
//	"WORKER"  → magenta [WORKER]
//
// Any other value or an absent "src" defaults to [CLIENTE].
type ConsoleHandler struct {
	mu    sync.Mutex
	w     io.Writer
	level slog.Level
	attrs []slog.Attr
	color bool
}

// NewConsoleHandler creates a ConsoleHandler that writes to w, colorized only
// when w is a terminal that renders ANSI escapes. Writing raw escape codes to
// a non-ANSI console (e.g. Windows' legacy conhost over SSH/pwsh without
// virtual terminal processing enabled) prints them as literal garbage instead
// of colors, so detection happens once here instead of assuming support.
func NewConsoleHandler(w io.Writer, level slog.Level) *ConsoleHandler {
	return &ConsoleHandler{w: w, level: level, color: supportsColor(w)}
}

// supportsColor reports whether w is a terminal capable of rendering ANSI
// escape codes. On Windows it also tries to enable virtual terminal
// processing on the console handle, since that's off by default outside
// Windows Terminal.
func supportsColor(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fd := f.Fd()
	if !isatty.IsTerminal(fd) && !isatty.IsCygwinTerminal(fd) {
		return false
	}
	return enableVirtualTerminal(fd)
}

// c returns code when colors are enabled for this handler, or "" otherwise.
func (h *ConsoleHandler) c(code string) string {
	if h.color {
		return code
	}
	return ""
}

func (h *ConsoleHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *ConsoleHandler) Handle(_ context.Context, r slog.Record) error {
	var buf bytes.Buffer

	// Timestamp — dim gray
	fmt.Fprintf(&buf, "%s%s%s ", h.c(ansiGray), r.Time.Format("15:04:05"), h.c(ansiReset))

	// Level — colored, fixed 5-char width
	buf.WriteString(h.c(levelColor(r.Level)))
	buf.WriteString(levelLabel(r.Level))
	buf.WriteString(h.c(ansiReset))
	buf.WriteString(" ")

	// Extract "src" from pre-attached attrs and record attrs.
	src := "CLIENTE"
	var extras []slog.Attr
	for _, a := range h.attrs {
		if a.Key == "src" {
			src = a.Value.String()
		} else {
			extras = append(extras, a)
		}
	}
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "src" {
			src = a.Value.String()
		} else {
			extras = append(extras, a)
		}
		return true
	})

	// Source tag — colored, fixed 9-char width so messages align
	buf.WriteString(h.c(srcColor(src)))
	buf.WriteString(srcLabel(src))
	buf.WriteString(h.c(ansiReset))
	buf.WriteString(" ")

	// Message
	buf.WriteString(r.Message)

	// Key=value pairs
	if len(extras) > 0 {
		buf.WriteString("  ")
		for i, a := range extras {
			if i > 0 {
				buf.WriteByte(' ')
			}
			fmt.Fprintf(&buf, "%s%s=%s%s", h.c(ansiGray), a.Key, h.c(ansiReset), fmtValue(a.Value))
		}
	}
	buf.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.w.Write(buf.Bytes())
	return err
}

func (h *ConsoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(next, h.attrs)
	copy(next[len(h.attrs):], attrs)
	return &ConsoleHandler{w: h.w, level: h.level, attrs: next, color: h.color}
}

func (h *ConsoleHandler) WithGroup(_ string) slog.Handler { return h }

// fmtValue renders a slog.Value for console output. Strings containing spaces
// are quoted; other kinds use slog's native String() representation.
func fmtValue(v slog.Value) string {
	if v.Kind() == slog.KindString {
		s := v.String()
		if strings.ContainsAny(s, " \t\n\r") {
			return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
		}
		return s
	}
	return v.String()
}

func levelColor(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return ansiRed
	case l >= slog.LevelWarn:
		return ansiYellow
	case l >= slog.LevelInfo:
		return ansiCyan
	default:
		return ansiGray
	}
}

func levelLabel(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "ERROR"
	case l >= slog.LevelWarn:
		return "WARN "
	case l >= slog.LevelInfo:
		return "INFO "
	default:
		return "DEBUG"
	}
}

func srcColor(src string) string {
	switch src {
	case "SERVER":
		return ansiBlue
	case "WORKER":
		return ansiMagenta
	case "CONFIG":
		return ansiWhite
	default:
		return ansiGreen
	}
}

// srcLabel returns a fixed-width 9-char tag so messages line up.
func srcLabel(src string) string {
	switch src {
	case "SERVER":
		return "[SERVER] "
	case "WORKER":
		return "[WORKER] "
	case "CONFIG":
		return "[CONFIG] "
	default:
		return "[CLIENTE]"
	}
}

// MultiHandler fans out log records to all provided handlers.
type MultiHandler struct {
	handlers []slog.Handler
}

// NewMultiHandler returns a handler that writes every record to each of the
// given handlers. Enables writing colorized output to the console while also
// writing plain text to a file without ANSI escape codes.
func NewMultiHandler(handlers ...slog.Handler) slog.Handler {
	return MultiHandler{handlers: handlers}
}

func (m MultiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m MultiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range m.handlers {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r.Clone()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	hs := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		hs[i] = h.WithAttrs(attrs)
	}
	return MultiHandler{handlers: hs}
}

func (m MultiHandler) WithGroup(name string) slog.Handler {
	hs := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		hs[i] = h.WithGroup(name)
	}
	return MultiHandler{handlers: hs}
}
