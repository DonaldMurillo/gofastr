package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// earlyHints sends 103 Early Hints, then its real answer.
var earlyHints = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Link", "</app.css>; rel=preload; as=style")
	w.WriteHeader(http.StatusEarlyHints)
	w.WriteHeader(http.StatusCreated)
	_, _ = io.WriteString(w, "made")
})

// getFinal serves h and returns the final status and body a client sees.
func getFinal(t *testing.T, h http.Handler) (int, string) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	resp, err := srv.Client().Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body)
}

// A wrapper that latched the 103 as the response dropped the handler's
// 201, and the client got 200.
func TestWrappersKeepStatusAfterHints(t *testing.T) {
	cases := map[string]http.Handler{
		"metrics": MetricsMiddleware(NewMetrics())(earlyHints),
		"tracing": Tracing()(earlyHints),
		"timeout": Timeout(time.Minute)(earlyHints),
		"logging": Logging()(earlyHints),
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			if code, body := getFinal(t, h); code != http.StatusCreated || body != "made" {
				t.Fatalf("got %d %q, want 201 \"made\"", code, body)
			}
		})
	}
}
