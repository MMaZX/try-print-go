// Package logging provides a colorized slog.Handler and a daily-rotating log writer
// for the print server.
package logging

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
)

// ANSI color codes — yellow/amber theme to visually distinguish from the client's cyan/green.
const (
	ansiReset   = "\033[0m"
	ansiGray    = "\033[90m"
	ansiYellow  = "\033[33;1m" // INFO level
	ansiAmber   = "\033[33m"   // server tag [SERVER ]
	ansiMagenta = "\033[35;1m" // WARN level
	ansiRed     = "\033[31;1m" // ERROR level
	ansiCyan    = "\033[36;1m" // success/ok event messages
)

// ConsoleHandler is a colorized slog.Handler for the server process.
// Every log line is prefixed with [SERVER ] in amber so it is immediately
// distinguishable from the client's green [CLIENTE] tag.
type ConsoleHandler struct {
	mu    sync.Mutex
	w     io.Writer
	level slog.Level
	attrs []slog.Attr
}

// NewConsoleHandler creates a ConsoleHandler that writes colorized output to w.
func NewConsoleHandler(w io.Writer, level slog.Level) *ConsoleHandler {
	return &ConsoleHandler{w: w, level: level}
}

func (h *ConsoleHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *ConsoleHandler) Handle(_ context.Context, r slog.Record) error {
	var buf bytes.Buffer

	// Timestamp — dim gray
	fmt.Fprintf(&buf, "%s%s%s ", ansiGray, r.Time.Format("15:04:05"), ansiReset)

	// Level — colored
	buf.WriteString(levelColor(r.Level))
	buf.WriteString(levelLabel(r.Level))
	buf.WriteString(ansiReset)
	buf.WriteString(" ")

	// Server tag — amber, fixed 9-char width to align with the client's [CLIENTE]
	buf.WriteString(ansiAmber)
	buf.WriteString("[SERVER ]")
	buf.WriteString(ansiReset)
	buf.WriteString(" ")

	// Collect all attrs, consuming "kind" to control message color.
	kind := ""
	var extras []slog.Attr
	for _, a := range h.attrs {
		if a.Key == "kind" {
			kind = a.Value.String()
		} else {
			extras = append(extras, a)
		}
	}
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "kind" {
			kind = a.Value.String()
		} else {
			extras = append(extras, a)
		}
		return true
	})

	// Message — cyan when the event is a successful outcome.
	if kind == "ok" {
		buf.WriteString(ansiCyan)
	}
	buf.WriteString(r.Message)
	if kind == "ok" {
		buf.WriteString(ansiReset)
	}

	// Key=value pairs
	if len(extras) > 0 {
		buf.WriteString("  ")
		for i, a := range extras {
			if i > 0 {
				buf.WriteByte(' ')
			}
			fmt.Fprintf(&buf, "%s%s=%s%s", ansiGray, a.Key, ansiReset, fmtValue(a.Value))
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
	return &ConsoleHandler{w: h.w, level: h.level, attrs: next}
}

func (h *ConsoleHandler) WithGroup(_ string) slog.Handler { return h }

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
		return ansiMagenta
	case l >= slog.LevelInfo:
		return ansiYellow
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

// MultiHandler fans out log records to all provided handlers.
type MultiHandler struct {
	handlers []slog.Handler
}

// NewMultiHandler returns a handler that writes every record to each handler.
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
