package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestSampledLogging_SanitizesMethod ensures the production-recommended
// sampled logger percent-encodes CR/LF/ESC in r.Method on BOTH branches
// (the always-log error/slow path and the 1-in-N sampled path), matching
// LoggingFn. Forged control bytes must never reach the log stream raw.
func TestSampledLogging_SanitizesMethod(t *testing.T) {
	forge := func(t *testing.T, status int) {
		t.Helper()
		var buf strings.Builder
		logger := slog.New(slog.NewJSONHandler(&buf, nil))
		srv := SampledLoggingFn(2, time.Hour, func() *slog.Logger { return logger })(
			http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}),
		)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Method = "GE\r\nT\x1b]"
		srv.ServeHTTP(httptest.NewRecorder(), req)

		out := buf.String()
		// Raw control bytes must never appear.
		if strings.Contains(out, "GE\r\nT") || strings.Contains(out, "\x1b") {
			t.Fatalf("sampled logger emitted raw control bytes in method: %q", out)
		}
		// safeLogMethod percent-encodes the control bytes, so the method
		// must land as GE%0d%0aT%1b] — NOT JSON-escaped \r\n (which a text
		// grep / naive log shipper would still render as a fake line).
		if !strings.Contains(out, "GE%0d%0aT%1b]") {
			t.Fatalf("sampled logger did not percent-encode control bytes in method (safeLogMethod not applied): %q", out)
		}
	}

	// status 500 -> always-log (error) branch.
	forge(t, http.StatusInternalServerError)
	// status 200 -> 1-in-N sampled branch (first request always logged).
	forge(t, http.StatusOK)
}

// TestLogging_SanitizesMethod ensures r.Method is percent-encoded the
// same way the URL path already is — CRLF / ESC in a forged method
// would otherwise paint fake log lines or terminal-escape mischief
// into operator tails.
func TestLogging_SanitizesMethod(t *testing.T) {
	var buf strings.Builder
	mw := LoggingWithWriter(&buf)
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Method = "GE\r\nT"
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	out := buf.String()
	if strings.Contains(out, "\r") || strings.Contains(out, "GE\\r\\nT") == false && strings.Contains(out, "GE%0d%0aT") == false {
		// Either escaped or percent-encoded is fine; raw CRLF is not.
		if strings.Contains(out, "GE\r\nT") {
			t.Fatalf("logger emitted raw CRLF method: %q", out)
		}
	}
}

// TestLogging_LogInjection verifies that request paths with newlines don't
// inject fake log entries. Attack: log forging via CRLF in URL path.
func TestLogging_LogInjection(t *testing.T) {
	var buf strings.Builder
	mw := LoggingWithWriter(&buf)
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test%0d%0aFAKE-ENTRY:+error=system+compromised", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	logOutput := buf.String()
	if strings.Contains(logOutput, "FAKE-ENTRY") && !strings.Contains(logOutput, "/test%0d%0a") {
		t.Errorf("SECURITY: [logging] log injection possible — URL-decoded newline in log. Attack: log forging via CRLF in path.")
	}
}

// TestLogging_LongPathTruncated verifies that very long request paths
// don't produce huge log entries. Attack: log exhaustion via long URLs.
func TestLogging_LongPathTruncated(t *testing.T) {
	var buf strings.Builder
	mw := LoggingWithWriter(&buf)
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	longPath := "/" + strings.Repeat("A", 10000)
	req := httptest.NewRequest(http.MethodGet, longPath, nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	logOutput := buf.String()
	// If the full path appears, the logging sink does not bound the path
	// length — disk exhaustion via long URLs. (Recovery/timeout already
	// truncated; logging did not — see TestLogSinksScrubAndBound.)
	if strings.Contains(logOutput, strings.Repeat("A", 10000)) {
		t.Errorf("SECURITY: [logging] full 10KB path logged unbounded. Attack: disk exhaustion via long URLs.")
	}
}

// captureHandler is a minimal slog.Handler that stores the last record's
// attributes verbatim (no JSON/text escaping), so a test can detect raw
// control bytes that a real JSON handler would have escaped away.
type captureHandler struct {
	mu    sync.Mutex
	attrs map[string]string
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.attrs == nil {
		h.attrs = map[string]string{}
	}
	r.Attrs(func(a slog.Attr) bool { h.attrs[a.Key] = a.Value.String(); return true })
	return nil
}
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

func (h *captureHandler) get(key string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.attrs[key]
}
func (h *captureHandler) reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.attrs = map[string]string{}
}

// TestLogSinksScrubAndBound pins the two halves of the log-injection
// property across EVERY sink that logs a request-derived value: control
// bytes must be scrubbed (r.URL.Path is percent-DECODED, so %0d%0a is a real
// CRLF) and the path must be length-bounded. Logging scrubbed but never
// truncated; recovery/timeout truncated but never scrubbed — each sink held
// only one half. This loops the property × surface so a new sink can't drift
// the same way.
func TestLogSinksScrubAndBound(t *testing.T) {
	prev := slog.Default()
	cap := &captureHandler{}
	slog.SetDefault(slog.New(cap))
	t.Cleanup(func() { slog.SetDefault(prev) })

	sinks := []struct {
		name    string
		handler http.Handler
		async   bool // timeout logs from a watcher goroutine after ServeHTTP returns
	}{
		{"logging", Logging()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})), false},
		{"recovery", Recovery()(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			panic("boom")
		})), false},
		{"timeout", Timeout(50 * time.Millisecond)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			<-r.Context().Done() // wait for the deadline, then panic late
			panic("late")
		})), true},
	}

	// Distinct attack bytes: CR, LF, NUL, DEL.
	for _, sk := range sinks {
		t.Run(sk.name, func(t *testing.T) {
			for _, b := range []byte{'\r', '\n', 0x00, 0x7f} {
				cap.reset()
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.URL.Path = "/test" + string(b) + "END"
				req.Method = "GE" + string(b) + "T"
				sk.handler.ServeHTTP(httptest.NewRecorder(), req)

				if sk.async {
					// The timed-out-handler log fires from a watcher goroutine.
					deadline := time.Now().Add(2 * time.Second)
					for time.Now().Before(deadline) && cap.get("path") == "" {
						time.Sleep(5 * time.Millisecond)
					}
				}

				if got := cap.get("path"); got == "" {
					t.Fatalf("byte=%#x: no path attribute captured", b)
				} else if strings.ContainsRune(got, rune(b)) {
					t.Errorf("byte=%#x: raw control byte reached path log attr %q", b, got)
				}
				if got := cap.get("method"); strings.ContainsRune(got, rune(b)) {
					t.Errorf("byte=%#x: raw control byte reached method log attr %q", b, got)
				}
			}

			// Length bound: a 10 KB path must not land in full.
			cap.reset()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.URL.Path = "/" + strings.Repeat("A", 10000)
			sk.handler.ServeHTTP(httptest.NewRecorder(), req)
			if sk.async {
				deadline := time.Now().Add(2 * time.Second)
				for time.Now().Before(deadline) && cap.get("path") == "" {
					time.Sleep(5 * time.Millisecond)
				}
			}
			if strings.Contains(cap.get("path"), strings.Repeat("A", 10000)) {
				t.Errorf("10KB path logged unbounded: %d bytes", len(cap.get("path")))
			}
		})
	}
}
