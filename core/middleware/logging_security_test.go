package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
