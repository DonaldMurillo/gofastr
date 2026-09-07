//go:build red

package semantic

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2
// (family enumeration; tier T2). Tests-only; no fix applied.
// Property: a recovered panic value embedding metadata-derived text
// reaches the default slog sink scrubbed of C0/DEL/C1/bidi — the
// scrubControlBytes rule pinned for RecoveryFn + Timeout's late-panic
// log (core/middleware) and battery/log's SlogErrorReporter.
// Surface: battery/semantic/watcher.go::safeMetadata :95-99 —
// slog.Default().Error(..., "panic", rec) with the raw recovered value,
// fired on the watcher loop where no HTTP recover net exists.
// Finding: a host-supplied MetadataFunc that panics with a value
// carrying raw control bytes (\x1b OSC opener, U+202E bidi override,
// U+009B CSI) gets them logged verbatim by slog.Default(); those bytes
// paint forged lines into the operator's tail (terminal-injection).
// The recovery itself is pinned (TestWatcherMetadataFuncPanicIndexes-
// WithNilMetadata) — the scrub of the recovered value is the open gap.
// Fix direction: strip unsafe runes (the core/textsafe.StripUnsafe rule
// core/middleware's scrubControlBytes implements) before logging.
// Probe note: the payload embeds the control bytes RAW (panic value),
// not %q-escaped — %q pre-escapes them and would hide the surface.

// redSemScrubSink is a minimal slog.Handler storing record attributes
// verbatim (no JSON/text escaping), so a test can detect raw control
// bytes a real handler would have escaped away.
type redSemScrubSink struct {
	mu    sync.Mutex
	attrs map[string]string
}

func (h *redSemScrubSink) Enabled(context.Context, slog.Level) bool { return true }

func (h *redSemScrubSink) Handle(_ context.Context, r slog.Record) error {
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

func (h *redSemScrubSink) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *redSemScrubSink) WithGroup(string) slog.Handler      { return h }

func (h *redSemScrubSink) redGet(key string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.attrs[key]
}

func TestSemanticRedMetaPanicScrubbed(t *testing.T) {
	prev := slog.Default()
	sink := &redSemScrubSink{}
	slog.SetDefault(slog.New(sink))
	t.Cleanup(func() { slog.SetDefault(prev) })

	malicious := "boom \x1b]0;pwned \u202e and \u009bCSI"
	w := NewWatcher(nil, WatchOptions{
		MetadataFunc: func(string) map[string]any { panic(malicious) },
	})
	_ = w.safeMetadata("/tmp/red-probe.md")

	got := sink.redGet("panic")
	if got == "" {
		t.Fatalf("setup broken: MetadataFunc panic never reached the slog sink (attrs=%v)", sink.attrs)
	}
	for _, bad := range []string{"\x1b", "\u202e", "\u009b"} {
		if strings.Contains(got, bad) {
			t.Errorf("SECURITY: [semantic-metadata-panic-scrub] recovered panic value reached slog "+
				"unscrubbed: raw %q present in panic attr %q. Attack: metadata-derived control/bidi "+
				"bytes forge lines in the operator's log tail (terminal-injection) on the watcher loop, "+
				"where no HTTP recover net exists; every pinned recover path (RecoveryFn, Timeout, "+
				"SlogErrorReporter) scrubs before logging.", bad, got)
		}
	}
	if !strings.Contains(got, "boom") || !strings.Contains(got, "pwned") {
		t.Errorf("SECURITY: [semantic-metadata-panic-scrub] positive control: visible panic text was "+
			"destroyed on the way to the log (got %q)", got)
	}
}
