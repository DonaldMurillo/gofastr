package pluginhost

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// RegisterBrokerRoute is idempotent: many plugins may call it from Init, but
// only the first registration lands (a duplicate router pattern panics).
func TestRegisterBrokerRoute_Idempotent(t *testing.T) {
	rt := router.New()
	RegisterBrokerRoute(rt)
	RegisterBrokerRoute(rt) // must NOT panic
	RegisterBrokerRoute(rt)

	n := 0
	for _, rr := range rt.Routes() {
		if rr.Method == BrokerRouteMethod && rr.Pattern == BrokerScriptURL {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("broker route registered %d times, want exactly 1", n)
	}

	// And it serves the JS.
	srv := httptest.NewServer(rt)
	defer srv.Close()
	resp, err := http.Get(srv.URL + BrokerScriptURL)
	if err != nil {
		t.Fatalf("GET broker: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("broker status=%d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Errorf("broker Content-Type=%q", ct)
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("broker must carry nosniff")
	}
	if len(body) == 0 {
		t.Error("broker body empty")
	}
}

// A non-framed FS-served asset must NOT get the framing/CORP/CSP relaxation —
// only framed plugin-frame assets do.
func TestAssetServer_NonFramedFSAssetHasNoRelaxation(t *testing.T) {
	fsys := fstest.MapFS{
		"adapter.js": &fstest.MapFile{Data: []byte("/* host adapter */")},
	}
	srv := NewAssetServer(fsys, "/__p", []AssetSpec{
		{Name: "adapter.js", ContentType: "text/javascript; charset=utf-8", Framed: false},
	})
	rt := router.New()
	srv.Register(rt)

	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/__p/adapter.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	if rec.Header().Get("Content-Security-Policy") != "" {
		t.Error("non-framed asset must not carry a framed CSP")
	}
	if rec.Header().Get("Cross-Origin-Resource-Policy") == "cross-origin" {
		t.Error("non-framed asset must not carry CORP cross-origin")
	}
	// nosniff still applies to every asset.
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("every asset carries nosniff")
	}
}
