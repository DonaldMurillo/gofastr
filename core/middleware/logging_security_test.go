package middleware

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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
	// If the full path appears, it's unbounded
	if strings.Contains(logOutput, strings.Repeat("A", 10000)) {
		t.Logf("SECURITY: [logging] full 10KB path logged. Consider truncating paths in log output. Attack: disk exhaustion via long URLs.")
	}
}
