package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// net/http panics on a WriteHeader code outside 100-999 before sending
// anything. A wrapper that latched the bad code before forwarding it
// swallowed Recovery's 500, so the client got a 200 and the log a status
// nothing sent. statusRecorder records only a status the writer below
// accepted.
func TestStatusRecorderSkipsAStatusTheWriterRefused(t *testing.T) {
	bad := RecoveryFn(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(1000)
	}))
	var logged bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logged, nil))
	for name, h := range map[string]http.Handler{
		"logging":     LoggingFn(func() *slog.Logger { return logger })(bad),
		"idempotency": Idempotency(IdempotencyConfig{Principal: testPrincipal})(bad),
		"metrics":     MetricsMiddleware(NewMetrics())(bad),
		"tracing":     Tracing()(bad),
	} {
		srv := httptest.NewServer(h)
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/x", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(IdempotencyKeyHeader, "invalid-status-"+name)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		resp.Body.Close()
		srv.Close()
		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("%s: status = %d, want 500", name, resp.StatusCode)
		}
	}
	if !strings.Contains(logged.String(), "status=500") {
		t.Errorf("logging recorded a status the client never got:\n%s", logged.String())
	}
}
