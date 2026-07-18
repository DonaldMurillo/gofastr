package crud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/mcp"
)

// TestRedispatch_RunsThroughRouter proves the exported seam re-enters the
// router's middleware chain (the load-bearing property — a bare CRUD handler
// call would bypass auth) and returns the parsed JSON envelope.
func TestRedispatch_RunsThroughRouter(t *testing.T) {
	got := make(chan string, 2)
	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Middleware observably ran: the gate flag the broker relies on.
		got <- "method:" + r.Method
		got <- "path:" + r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"ok":true},"total":1}`))
	})

	out, err := Redispatch(context.Background(), mux, http.MethodGet, "/api/widgets", nil)
	if err != nil {
		t.Fatalf("Redispatch: %v", err)
	}
	env, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("result not envelope: %T", out)
	}
	if env["total"].(float64) != 1 {
		t.Errorf("total = %v, want 1", env["total"])
	}
	if method, path := <-got, <-got; method != "method:GET" || path != "path:/api/widgets" {
		t.Errorf("router saw %q %q", method, path)
	}
}

// TestRedispatch_ReinjectsOriginatingHeaders proves the header copy from the
// mcp.WithRequest-stashed originating request — without it a reverse call
// arrives with no Cookie/Authorization and owner-scoped CRUD returns 401
// (design §5 caveat b).
func TestRedispatch_ReinjectsOriginatingHeaders(t *testing.T) {
	var sawCookie, sawAuthz string
	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawCookie = r.Header.Get("Cookie")
		sawAuthz = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	})
	orig := httptest.NewRequest(http.MethodGet, "/api/widgets", nil)
	orig.Header.Set("Cookie", "sid=abc")
	orig.Header.Set("Authorization", "Bearer t")
	ctx := mcp.WithRequest(context.Background(), orig)

	if _, err := Redispatch(ctx, mux, http.MethodGet, "/api/widgets", nil); err != nil {
		t.Fatalf("Redispatch: %v", err)
	}
	if sawCookie != "sid=abc" {
		t.Errorf("Cookie = %q, want sid=abc", sawCookie)
	}
	if sawAuthz != "Bearer t" {
		t.Errorf("Authorization = %q, want Bearer t", sawAuthz)
	}
}

// TestRedispatch_StatusErrorIsSurfaceable proves a CRUD chokepoint 403/401
// reaches the broker as an error (so it can deny the reverse call) rather than
// a silently-successful empty result.
func TestRedispatch_StatusErrorIsSurfaceable(t *testing.T) {
	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("access denied"))
	})
	_, err := Redispatch(context.Background(), mux, http.MethodGet, "/api/widgets", nil)
	if err == nil {
		t.Fatal("expected error for 403, got nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should name status 403: %v", err)
	}
	// Body is JSON-encoded into the message? It is raw text here — assert it
	// carries the denial text regardless of encoding.
	if b, _ := json.Marshal(err.Error()); !strings.Contains(string(b), "access denied") {
		t.Errorf("error should carry body text: %v", err)
	}
}
