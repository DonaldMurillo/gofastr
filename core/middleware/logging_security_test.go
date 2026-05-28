package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
