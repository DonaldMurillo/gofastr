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

// newAssetRouter wires an AssetServer onto a fresh router for header checks.
func newAssetRouter(t *testing.T) *router.Router {
	t.Helper()
	fsys := fstest.MapFS{
		"editor.html": &fstest.MapFile{Data: []byte("<!doctype html><p>frame")},
		"editor.js":   &fstest.MapFile{Data: []byte("var x=1;")},
		"editor.css":  &fstest.MapFile{Data: []byte(":root{}")},
	}
	srv := NewAssetServer(fsys, "/__p", []AssetSpec{
		{Name: "editor.html", ContentType: "text/html; charset=utf-8", Framed: true},
		{Name: "editor.js", ContentType: "text/javascript; charset=utf-8", Framed: true},
		{Name: "editor.css", ContentType: "text/css; charset=utf-8", Framed: true},
	})
	srv.AddBytes("/__p/broker.js", "text/javascript; charset=utf-8", false, []byte("(()=>{})()"))
	rt := router.New()
	srv.Register(rt)
	return rt
}

func TestAssetServerFramedHeaders(t *testing.T) {
	rt := newAssetRouter(t)
	srv := httptest.NewServer(rt)
	defer srv.Close()

	for _, path := range []string{"/__p/editor.html", "/__p/editor.js", "/__p/editor.css"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: status=%d", path, resp.StatusCode)
		}
		if len(body) == 0 {
			t.Errorf("%s: empty body", path)
		}
		csp := resp.Header.Get("Content-Security-Policy")
		// The relaxation: frame-ancestors permits the host origin (NOT 'none'), and
		// — crucially — the frame's OWN script/style sub-resources are keyed to the
		// explicit request origin, NOT 'self' (which is null for the opaque frame
		// and gets the assets refused in strict browsers like Safari). CORP cross-origin.
		if !strings.Contains(csp, "frame-ancestors http") {
			t.Errorf("%s: CSP frame-ancestors must permit the host origin: %q", path, csp)
		}
		if strings.Contains(csp, "frame-ancestors 'none'") {
			t.Errorf("%s: CSP must NOT carry frame-ancestors 'none': %q", path, csp)
		}
		if strings.Contains(csp, "script-src 'self'") || strings.Contains(csp, "default-src 'self'") {
			t.Errorf("%s: framed CSP must NOT use 'self' (null for opaque frame) — use the explicit origin: %q", path, csp)
		}
		if !strings.Contains(csp, "script-src http") {
			t.Errorf("%s: framed CSP must allow the frame's own scripts by origin: %q", path, csp)
		}
		if got := resp.Header.Get("Cross-Origin-Resource-Policy"); got != "cross-origin" {
			t.Errorf("%s: CORP=%q want cross-origin", path, got)
		}
		// X-Frame-Options handling: the global SecurityHeaders middleware emits
		// XFO:DENY; framed assets rely on CSP frame-ancestors 'self' SUPERSEDING
		// XFO (DECISIONS.md Phase-0 gotcha), which is the asserted guarantee
		// above. (The broker's h.Del is belt-and-suspenders; a buffering
		// middleware upstream can re-emit XFO, so it is not asserted here.)
	}
}

// The framed CSP must (a) sandbox the document so a TOP-LEVEL load can't run
// unsandboxed same-origin, (b) forbid all network egress via connect-src
// 'none' (the exfil guard), and (c) carry nosniff. These are the load-bearing
// isolation directives — pinned so a refactor can't silently drop them.
func TestAssetServerFramedIsolationDirectives(t *testing.T) {
	rt := newAssetRouter(t)
	srv := httptest.NewServer(rt)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/__p/editor.html")
	if err != nil {
		t.Fatalf("GET editor.html: %v", err)
	}
	resp.Body.Close()
	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "sandbox allow-scripts") {
		t.Errorf("framed CSP must carry `sandbox allow-scripts` so a top-level load stays sandboxed: %q", csp)
	}
	if strings.Contains(csp, "allow-same-origin") {
		t.Errorf("framed CSP sandbox must NEVER allow-same-origin: %q", csp)
	}
	if !strings.Contains(csp, "connect-src 'none'") {
		t.Errorf("framed CSP must forbid network egress (connect-src 'none' is the exfil guard): %q", csp)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("framed asset must carry nosniff, got %q", got)
	}
}

// A request whose scheme/host would inject a CSP directive must be refused,
// not served with a poisoned policy.
func TestAssetServerRejectsOriginInjection(t *testing.T) {
	rt := newAssetRouter(t)

	// Malicious X-Forwarded-Proto trying to splice a directive.
	req := httptest.NewRequest(http.MethodGet, "/__p/editor.html", nil)
	req.Header.Set("X-Forwarded-Proto", "https ; connect-src *")
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("injected X-Forwarded-Proto must 400, got %d (CSP=%q)", rec.Code, rec.Header().Get("Content-Security-Policy"))
	}

	// Malicious Host with a CSP-breaking character.
	req = httptest.NewRequest(http.MethodGet, "/__p/editor.html", nil)
	req.Host = "evil.com; connect-src *"
	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("injected Host must 400, got %d (CSP=%q)", rec.Code, rec.Header().Get("Content-Security-Policy"))
	}

	// A legitimate forwarded scheme still works.
	req = httptest.NewRequest(http.MethodGet, "/__p/editor.html", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("valid https X-Forwarded-Proto should serve, got %d", rec.Code)
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Security-Policy"), "sandbox allow-scripts; default-src https://") {
		t.Errorf("valid https origin should key the CSP to https: %q", rec.Header().Get("Content-Security-Policy"))
	}
}

func TestAssetServerHostScriptHasNoRelaxation(t *testing.T) {
	rt := newAssetRouter(t)
	srv := httptest.NewServer(rt)
	defer srv.Close()

	// The host-page broker script is NOT a framed asset: it must NOT carry the
	// CORP cross-origin relaxation (it is fetched same-origin by the host page,
	// not by the opaque frame).
	resp, err := http.Get(srv.URL + "/__p/broker.js")
	if err != nil {
		t.Fatalf("GET broker.js: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("broker.js status=%d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/javascript; charset=utf-8" {
		t.Errorf("broker.js Content-Type=%q", ct)
	}
	if got := resp.Header.Get("Cross-Origin-Resource-Policy"); got == "cross-origin" {
		t.Errorf("host-page broker must NOT be CORP cross-origin (it is same-origin)")
	}
}
