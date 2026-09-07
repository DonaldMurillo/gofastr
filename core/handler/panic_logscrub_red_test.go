//go:build red

package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// RED TEST — open finding, 2026-09-06 adversarial pass (round 5;
// tests-only; no fix applied).
// Property: a recovered panic value embedding request-derived text reaches
// the default slog sink scrubbed of C0/DEL/C1/bidi — the scrubControlBytes
// rule. RecoveryFn and Timeout's late-panic log (core/middleware) both
// scrub+truncate; HandlerAdapter's own recover truncates only.
// Surfaces: core/handler/handler.go HandlerAdapter recover path —
// truncateLog(fmt.Sprint(rec), maxPanicLogLen) with no scrub.
// Finding: a handler that panics with a value carrying raw request-derived
// control bytes (\r, NUL, DEL, U+202E bidi override, U+009B CSI) gets them
// logged verbatim by slog.Default(); those bytes paint forged lines into
// the operator's tail (terminal-injection).
// Fix direction: strip unsafe runes (core/textsafe.StripUnsafe, the same
// rule core/middleware's scrubControlBytes implements) BEFORE truncateLog.
// Probe note: the panic value embeds the control bytes RAW (fmt %s), not
// %q-escaped — %q pre-escapes them, which would hide the surface.

// redPanicScrubSink is a minimal slog.Handler storing record attributes
// verbatim (no JSON/text escaping), so a test can detect raw control bytes
// a real handler would have escaped away. Unique-prefix replica of
// middleware's captureHandler pattern (logging_security_test.go).
type redPanicScrubSink struct {
	mu    sync.Mutex
	attrs map[string]string
}

func (h *redPanicScrubSink) Enabled(context.Context, slog.Level) bool { return true }

func (h *redPanicScrubSink) Handle(_ context.Context, r slog.Record) error {
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

func (h *redPanicScrubSink) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *redPanicScrubSink) WithGroup(string) slog.Handler      { return h }

func (h *redPanicScrubSink) redGet(key string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.attrs[key]
}

func TestHandlerAdapterRedPanicLogScrub(t *testing.T) {
	prev := slog.Default()
	sink := &redPanicScrubSink{}
	slog.SetDefault(slog.New(sink))
	t.Cleanup(func() { slog.SetDefault(prev) })

	// Request-derived text carrying raw C0/DEL/C1/bidi bytes, embedded in
	// the panic value the way a handler echoing user input would build it.
	malicious := "x\r\n\u202E\u009bINJECT\x00\x7f"
	h := HandlerAdapter[struct{}, struct{}](func(_ context.Context, _ struct{}) (struct{}, error) {
		panic(fmt.Sprintf("bad input %s", malicious))
	})

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("setup broken: recovered panic did not produce 500 (got %d)", rec.Code)
	}
	got := sink.redGet("error")
	if got == "" {
		t.Fatalf("setup broken: panic value never reached the slog sink (attrs=%v)", sink.attrs)
	}
	for _, bad := range []string{"\r", "\u202E", "\u009B", "\x00", "\x7f"} {
		if strings.Contains(got, bad) {
			t.Errorf("SECURITY: [handler-panic-log-scrub] recovered panic value reached slog "+
				"unscrubbed: raw %q present in error attr %q. Attack: request-derived control/bidi "+
				"bytes forge lines in the operator's log tail (terminal-injection); every other "+
				"recover path scrubs before logging.", bad, got)
		}
	}

	// Positive control: benign panic text survives the log path intact.
	h2 := HandlerAdapter[struct{}, struct{}](func(_ context.Context, _ struct{}) (struct{}, error) {
		panic("benign panic message")
	})
	rec2 := httptest.NewRecorder()
	h2(rec2, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec2.Code != http.StatusInternalServerError {
		t.Fatalf("setup broken: benign panic did not produce 500 (got %d)", rec2.Code)
	}
	if !strings.Contains(sink.redGet("error"), "benign panic message") {
		t.Errorf("SECURITY: [handler-panic-log-scrub] positive control: benign panic text was "+
			"destroyed on the way to the log (got %q)", sink.redGet("error"))
	}
}
