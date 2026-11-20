// Package scrubtest provides the verbatim slog capture sink shared by the
// panic/log-scrub security tests across the tree: a minimal slog.Handler
// that stores record attributes unescaped, so a test can detect raw
// control bytes that a real JSON/text handler would have escaped away.
package scrubtest

import (
	"context"
	"log/slog"
	"sync"
	"testing"
)

// Sink is a minimal slog.Handler storing record attributes verbatim (no
// JSON/text escaping), so a test can detect raw control bytes a real
// handler would have escaped away. The zero value is ready to use.
type Sink struct {
	mu    sync.Mutex
	attrs map[string]string
}

func (h *Sink) Enabled(context.Context, slog.Level) bool { return true }

func (h *Sink) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.attrs == nil {
		h.attrs = map[string]string{}
	}
	r.Attrs(func(a slog.Attr) bool {
		h.attrs[a.Key] = a.Value.String()
		return true
	})
	return nil
}

func (h *Sink) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *Sink) WithGroup(string) slog.Handler      { return h }

// Get returns the last recorded value of key ("" when the key never
// appeared).
func (h *Sink) Get(key string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.attrs[key]
}

// Attrs returns a copy of every recorded attribute, for setup-broken
// diagnostics that print what the sink did capture.
func (h *Sink) Attrs() map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]string, len(h.attrs))
	for k, v := range h.attrs {
		out[k] = v
	}
	return out
}

// Reset drops every recorded attribute.
func (h *Sink) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.attrs = map[string]string{}
}

// InstallDefault makes sink the process-wide default logger for the
// duration of t, restoring the previous default on cleanup — the standard
// preamble of the scrub probes that read slog.Default().
func InstallDefault(t testing.TB, sink *Sink) {
	t.Helper()
	prev := slog.Default()
	slog.SetDefault(slog.New(sink))
	t.Cleanup(func() { slog.SetDefault(prev) })
}
