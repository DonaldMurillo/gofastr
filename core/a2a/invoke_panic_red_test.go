//go:build red

package a2a

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2
// (family enumeration; tier T2). Tests-only; no fix applied.
// Property: a recovered panic value embedding handler-derived text
// reaches the server's slog sink scrubbed of C0/DEL/C1/bidi — the
// scrubControlBytes rule pinned for RecoveryFn + Timeout's late-panic
// log (core/middleware) and battery/log's SlogErrorReporter.
// Surface: core/a2a/exec.go::invoke :344-348 — s.log.Error(...,
// "panic", p, "stack", ...) with the raw recovered value. a2a captures
// its logger from Config.Logger at NewServer time (nil → slog.Default()),
// so the recording sink is installed via the config, not SetDefault.
// Finding: a skill Handler that panics with a value carrying raw control
// bytes (\x1b OSC opener, U+202E bidi override, U+009B CSI) gets them
// logged verbatim; those bytes paint forged lines into the operator's
// tail (terminal-injection). The recovery itself is pinned
// (TestHandlerPanicFails) — the scrub of the recovered value is the
// open gap.
// Fix direction: strip unsafe runes (the core/textsafe.StripUnsafe rule
// core/middleware's scrubControlBytes implements) before logging.
// Probe note: the payload embeds the control bytes RAW (panic value),
// not %q-escaped — %q pre-escapes them and would hide the surface.

// redA2AScrubSink is a minimal slog.Handler storing record attributes
// verbatim (no JSON/text escaping), so a test can detect raw control
// bytes a real handler would have escaped away.
type redA2AScrubSink struct {
	mu    sync.Mutex
	attrs map[string]string
}

func (h *redA2AScrubSink) Enabled(context.Context, slog.Level) bool { return true }

func (h *redA2AScrubSink) Handle(_ context.Context, r slog.Record) error {
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

func (h *redA2AScrubSink) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *redA2AScrubSink) WithGroup(string) slog.Handler      { return h }

func (h *redA2AScrubSink) redGet(key string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.attrs[key]
}

func TestA2AInvokeRedPanicScrubbed(t *testing.T) {
	sink := &redA2AScrubSink{}
	h := newHarness(t, func(c *Config) { c.Logger = slog.New(sink) })

	malicious := "boom \x1b]0;pwned \u202e and \u009bCSI"
	h.setHandler(func(_ context.Context, _ TaskContext) error { panic(malicious) })
	task := h.send("alice")
	if task.Status.State != TaskStateFailed {
		t.Fatalf("setup broken: handler panic did not fail the task (state=%s)", task.Status.State)
	}

	got := sink.redGet("panic")
	if got == "" {
		t.Fatalf("setup broken: handler panic never reached the a2a logger (attrs=%v)", sink.attrs)
	}
	for _, bad := range []string{"\x1b", "\u202e", "\u009b"} {
		if strings.Contains(got, bad) {
			t.Errorf("SECURITY: [a2a-invoke-panic-scrub] recovered panic value reached slog "+
				"unscrubbed: raw %q present in panic attr %q. Attack: handler-derived control/bidi "+
				"bytes forge lines in the operator's log tail (terminal-injection); every pinned "+
				"recover path (RecoveryFn, Timeout, SlogErrorReporter) scrubs before logging.", bad, got)
		}
	}
	if !strings.Contains(got, "boom") || !strings.Contains(got, "pwned") {
		t.Errorf("SECURITY: [a2a-invoke-panic-scrub] positive control: visible panic text was "+
			"destroyed on the way to the log (got %q)", got)
	}
}
