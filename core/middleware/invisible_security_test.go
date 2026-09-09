package middleware

// Property, found by the 2026-09-05 adversarial red-probe round 4
// (family F25, fixed the same round by widening the scrub into
// core/textsafe.ScrubControlBytes): request-derived values reaching operator log/trace
// sinks must not carry invisible or terminal-control characters in any
// encoding form — the C0+DEL scrub stopped at U+007F, so C1 controls
// (U+0080..U+009F, including 8-bit CSI/OSC and NEL) and the
// zero-width/bidi set (U+200B..U+200F, U+FEFF, U+2060, U+202A..U+202E,
// U+2066..U+2069) passed verbatim into slog entries and OTel span
// attributes. slog's JSON handler does not escape them (verified
// 2026-09-05: raw C2 9B lands in the encoded line), so operator tails,
// collector UIs and line-oriented shippers render the control verbatim:
// 8-bit terminals execute CSI/OSC 9B/9D as escapes and NEL 85 as a line
// break, and RLO/zero-width visually rewrite the logged path.
//
// Surfaces: core/middleware/logging.go's safeLogMethod/safeLogPath (→
// textsafe.ScrubControlBytes; LoggingFn /
// SampledLoggingFn path+method attrs, RecoveryFn error+path+method
// attrs), core/middleware/tracing.go::Tracing (span name + http.method
// / http.target / http.route attributes), core/middleware/idempotency.go
// (Finish-failure log attr "key", scrubbed with the same helper).

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// hasInvisibleChar reports whether s carries a C1 control (U+0080..U+009F,
// including the 8-bit CSI 0x9B, OSC 0x9D and NEL 0x85), a zero-width
// character (U+200B..U+200F, U+FEFF, U+2060), a bidi override
// (U+202A..U+202E) or a bidi isolate (U+2066..U+2069). Deliberately a
// local re-enumeration, not textsafe.ContainsUnsafe, so the test stays
// independent of the predicate it pins.
func hasInvisibleChar(s string) bool {
	for _, r := range s {
		switch {
		case r >= 0x80 && r <= 0x9F:
			return true
		case r >= 0x200B && r <= 0x200F:
			return true
		case r == 0xFEFF, r == 0x2060:
			return true
		case r >= 0x202A && r <= 0x202E:
			return true
		case r >= 0x2066 && r <= 0x2069:
			return true
		}
	}
	return false
}

// invisibleFinishFailStore lets Begin succeed and Finish fail, driving the
// idempotency middleware's Finish-failure log path (which scrubs the
// request-borne Idempotency-Key with textsafe.ScrubControlBytes).
type invisibleFinishFailStore struct{}

func (invisibleFinishFailStore) Begin(context.Context, string, string) (*IdempotentResponse, bool, error) {
	return nil, false, nil
}
func (invisibleFinishFailStore) Finish(context.Context, string, string, *IdempotentResponse) error {
	return errors.New("store down")
}

// TestLogTraceScrubInvisibleChars loops the four attack shapes through every
// textsafe.ScrubControlBytes consumer reachable from request input: the access log
// middleware, the recovery log, the OTel tracing sink, and the idempotency
// Finish-failure log.
func TestLogTraceScrubInvisibleChars(t *testing.T) {
	// Four distinct shapes, percent-encoded for the URL surfaces:
	// RLO (visual reordering), 8-bit CSI (ANSI escape start), NEL (line
	// break), ZWSP (hidden content). All arrive DECODED in r.URL.Path.
	targets := []string{
		"/x%E2%80%AEadmin", // U+202E RIGHT-TO-LEFT OVERRIDE
		"/x%C2%9B31m",      // U+009B CSI, 8-bit escape introducer
		"/x%C2%85entry",    // U+0085 NEL, next-line control
		"/x%E2%80%8B",      // U+200B ZERO WIDTH SPACE
	}

	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("LoggingFn path attr", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&buf, nil))
		h := LoggingFn(func() *slog.Logger { return logger })(ok)
		for _, target := range targets {
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, target, nil))
		}
		if hasInvisibleChar(buf.String()) {
			t.Errorf("SECURITY: [log-invisible] LoggingFn emitted log lines carrying raw C1/zero-width/bidi characters from the request path: %q — an operator tail in an 8-bit terminal executes 0x9B/0x9D as escapes, 0x85 as a newline, and RLO reorders the rendered line", buf.String())
		}
	})

	t.Run("RecoveryFn panic and path attrs", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&buf, nil))
		h := RecoveryFn(func() *slog.Logger { return logger })(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("boom " + r.URL.Path)
		}))
		for _, target := range targets {
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, target, nil))
		}
		if hasInvisibleChar(buf.String()) {
			t.Errorf("SECURITY: [log-invisible] RecoveryFn emitted panic/path attrs carrying raw C1/zero-width/bidi characters: %q — forged panic values repaint operator tails verbatim", buf.String())
		}
	})

	t.Run("Tracing span name and attributes", func(t *testing.T) {
		rec := spanRecordingTracer(t)
		h := Tracing()(ok)
		for _, target := range targets {
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, target, nil))
		}
		for _, span := range rec.Ended() {
			if hasInvisibleChar(span.Name()) {
				t.Errorf("SECURITY: [trace-invisible] span name carries raw C1/zero-width/bidi characters: %q", span.Name())
			}
			for _, kv := range span.Attributes() {
				if s := kv.Value.Emit(); hasInvisibleChar(s) {
					t.Errorf("SECURITY: [trace-invisible] span attribute %q carries raw C1/zero-width/bidi characters: %q — collector UIs and exporters render the control verbatim", string(kv.Key), s)
				}
			}
		}
	})

	t.Run("Idempotency Finish-failure key attr", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&buf, nil))
		h := Idempotency(IdempotencyConfig{
			Store:     invisibleFinishFailStore{},
			Principal: testPrincipal,
			Logger:    logger,
		})(ok)
		for _, target := range targets {
			// The same invisible characters carried (decoded) in the
			// Idempotency-Key header value: header values legally carry
			// bytes >= 0x80, so C1/bidi UTF-8 sequences arrive raw.
			decoded, uerr := url.PathUnescape(strings.TrimPrefix(target, "/x"))
			if uerr != nil {
				t.Fatalf("unescape %q: %v", target, uerr)
			}
			req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader("{}"))
			req.Header.Set(IdempotencyKeyHeader, "k"+decoded)
			h.ServeHTTP(httptest.NewRecorder(), req)
		}
		if hasInvisibleChar(buf.String()) {
			t.Errorf("SECURITY: [log-invisible] idempotency Finish-failure log emitted the request-borne key carrying raw C1/zero-width/bidi characters: %q", buf.String())
		}
	})
}
