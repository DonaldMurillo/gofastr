package pluginhost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// mustPanic fails unless fn panics with a message containing want.
func mustPanic(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("must panic with %q in the message, did not panic", want)
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, want) {
			t.Fatalf("panic must mention %q, got %v", want, r)
		}
	}()
	fn()
}

// The full profile appends exactly its allowlisted tokens to script-src and
// widens connect-src to 'self'; every other directive stays the fixed
// skeleton. The duplicate 'wasm-unsafe-eval' (via WASM and via keywords) is
// deduped and WASM's shorthand lands first, so the header is deterministic.
func TestWorkerCSPPolicyShape(t *testing.T) {
	got := workerCSP(WorkerCSP{
		ScriptKeywords: []string{"'unsafe-eval'", "'wasm-unsafe-eval'"},
		ConnectSources: []string{"'self'"},
		WASM:           true,
	})
	want := "default-src 'self'" +
		"; script-src 'self' 'wasm-unsafe-eval' 'unsafe-eval'" +
		"; connect-src 'self'" +
		"; worker-src 'self'" +
		"; object-src 'none'"
	if got != want {
		t.Errorf("worker CSP:\n got %q\nwant %q", got, want)
	}
}

// A zero profile is the strictest worker policy: same-origin scripts, no
// network, no compilation. Defaults stay strict even though a worker could
// ask for more.
func TestWorkerCSPDefaultsStrict(t *testing.T) {
	got := workerCSP(WorkerCSP{})
	want := "default-src 'self'; script-src 'self'; connect-src 'none'; worker-src 'self'; object-src 'none'"
	if got != want {
		t.Errorf("zero worker CSP:\n got %q\nwant %q", got, want)
	}
}

// workerCSP is the authoritative assembly point: a profile that somehow
// skipped [validateWorkerProfile] cannot smuggle a keyword or a connect
// source into the header — an out-of-allowlist token is dropped and the
// policy degrades to the strict skeleton, including connect-src 'none'.
func TestWorkerCSPRefiltersSmuggledTokens(t *testing.T) {
	got := workerCSP(WorkerCSP{
		ScriptKeywords: []string{"'unsafe-inline'", "wasm-unsafe-eval", "https://cdn.example"},
		ConnectSources: []string{"https://cdn.example", "*"},
	})
	want := "default-src 'self'; script-src 'self'; connect-src 'none'; worker-src 'self'; object-src 'none'"
	if got != want {
		t.Errorf("smuggled worker CSP:\n got %q\nwant %q", got, want)
	}
}

// A registered worker reaches the wire with its narrow policy, the explicit
// cache posture, and nosniff — and none of the framed relaxation: a worker
// is fetched same-origin by the host page, so it must not get CORP
// cross-origin.
func TestWorkerAssetServesNarrowCSP(t *testing.T) {
	srv := NewAssetServer(fstest.MapFS{}, "/__w", nil)
	srv.AddBytes("/__w/depth.js", "text/javascript; charset=utf-8", false, []byte("onmessage=()=>{}"),
		WithWorkerCSP(WorkerCSP{
			ScriptKeywords: []string{"'unsafe-eval'"},
			ConnectSources: []string{"'self'"},
			WASM:           true,
		}),
		WithCache(CachePrivateNoStore),
	)
	rt := router.New()
	srv.Register(rt)
	hs := httptest.NewServer(rt)
	defer hs.Close()

	resp, err := http.Get(hs.URL + "/__w/depth.js")
	if err != nil {
		t.Fatalf("GET depth.js: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("depth.js status=%d", resp.StatusCode)
	}
	csp := resp.Header.Get("Content-Security-Policy")
	if want := "; script-src 'self' 'wasm-unsafe-eval' 'unsafe-eval';"; !strings.Contains(csp, want) {
		t.Errorf("worker CSP must carry exactly the opted-in keywords: %q", csp)
	}
	if got := resp.Header.Get("Cache-Control"); got != "private, no-store" {
		t.Errorf("Cache-Control=%q want private, no-store", got)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("nosniff=%q", got)
	}
	if got := resp.Header.Get("Cross-Origin-Resource-Policy"); got == "cross-origin" {
		t.Errorf("worker must NOT get the framed CORP relaxation (same-origin fetch): %q", got)
	}
}

// The worker relaxation is per-response, never per-document: one server
// behind the REAL production security middleware serves a host document, a
// host script, a framed asset, and a worker carrying 'unsafe-eval'. Only
// the worker response shows it; the host document's CSP stays the app
// default byte-for-byte, keeps CORP same-origin and XFO DENY; the framed
// asset keeps the framed policy.
func TestHostDocumentUntouchedByWorker(t *testing.T) {
	const strictCSP = "default-src 'self'; img-src 'self' data:; object-src 'none'; " +
		"form-action 'self'; frame-ancestors 'none'; base-uri 'self'"
	fsys := fstest.MapFS{"editor.html": &fstest.MapFile{Data: []byte("<!doctype html><p>frame")}}
	srv := NewAssetServer(fsys, "/__p", []AssetSpec{
		{Name: "editor.html", ContentType: "text/html; charset=utf-8", Framed: true},
	})
	srv.AddBytes("/__p/app.html", "text/html; charset=utf-8", false, []byte("<!doctype html><p>host"))
	srv.AddBytes("/__p/broker.js", "text/javascript; charset=utf-8", false, []byte("(()=>{})()"))
	srv.AddBytes("/__p/depth.js", "text/javascript; charset=utf-8", false, []byte("onmessage=()=>{}"),
		WithWorkerCSP(WorkerCSP{
			ScriptKeywords: []string{"'unsafe-eval'"},
			ConnectSources: []string{"'self'"},
			WASM:           true,
		}),
		WithCache(CachePrivateNoStore))
	rt := router.New()
	srv.Register(rt)
	// The app's own middleware, exactly as a GoFastr host wires it: the
	// strict document CSP, XFO DENY, CORP same-origin on every response.
	// Handler-side Set calls win over the middleware's, which is how both
	// the framed and worker policies take effect on their own responses.
	hs := httptest.NewServer(middleware.SecurityHeaders(middleware.SecurityHeadersConfig{})(rt))
	defer hs.Close()

	get := func(path string) *http.Response {
		t.Helper()
		resp, err := http.Get(hs.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close()
		return resp
	}

	host := get("/__p/app.html")
	if csp := host.Header.Get("Content-Security-Policy"); csp != strictCSP {
		t.Errorf("host document CSP must stay the app default, byte-for-byte:\n got %q\nwant %q", csp, strictCSP)
	}
	if strings.Contains(host.Header.Get("Content-Security-Policy"), "unsafe-eval") {
		t.Error("host document CSP must never carry unsafe-eval")
	}

	broker := get("/__p/broker.js")
	if csp := broker.Header.Get("Content-Security-Policy"); csp != strictCSP {
		t.Errorf("host script CSP must stay the app default:\n got %q\nwant %q", csp, strictCSP)
	}

	worker := get("/__p/depth.js")
	wcsp := worker.Header.Get("Content-Security-Policy")
	if !strings.Contains(wcsp, "'unsafe-eval'") {
		t.Errorf("worker response must carry the opted-in keyword: %q", wcsp)
	}
	if !strings.Contains(wcsp, "connect-src 'self'") {
		t.Errorf("worker response must carry the opted-in connect source: %q", wcsp)
	}
	if got := worker.Header.Get("Cross-Origin-Resource-Policy"); got != "same-origin" {
		t.Errorf("worker keeps CORP same-origin, got %q", got)
	}
	if got := worker.Header.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("worker response must keep the global XFO (no framing relaxation), got %q", got)
	}

	frame := get("/__p/editor.html")
	fcsp := frame.Header.Get("Content-Security-Policy")
	if !strings.HasPrefix(fcsp, "sandbox allow-scripts") {
		t.Errorf("framed asset keeps the framed policy, not the worker's: %q", fcsp)
	}
	if strings.Contains(fcsp, "unsafe-eval") {
		t.Errorf("framed policy must not carry unsafe-eval: %q", fcsp)
	}
}

// Registration fails loudly: a token outside the allowlists, a worker
// profile on a framed asset, or an unknown cache profile panics at the
// AddBytes call site rather than serving a policy nobody asked for.
func TestAddBytesRejectsInvalidWorkerCSP(t *testing.T) {
	fresh := func() *AssetServer { return NewAssetServer(nil, "/__w", nil) }
	mustPanic(t, "allowlist", func() {
		fresh().AddBytes("/__w/a.js", "", false, nil,
			WithWorkerCSP(WorkerCSP{ScriptKeywords: []string{"'unsafe-inline'"}}))
	})
	mustPanic(t, "allowlist", func() {
		fresh().AddBytes("/__w/a.js", "", false, nil,
			WithWorkerCSP(WorkerCSP{ConnectSources: []string{"https://cdn.example"}}))
	})
	mustPanic(t, "framed", func() {
		fresh().AddBytes("/__w/a.js", "", true, nil,
			WithWorkerCSP(WorkerCSP{WASM: true}))
	})
	mustPanic(t, "cache profile", func() {
		fresh().AddBytes("/__w/a.js", "", false, nil, WithCache(CacheProfile(9)))
	})
}

// The named cache postures reach the wire as exactly these headers, and the
// default is byte-identical to the header every asset served before
// profiles existed — the default path cannot drift.
func TestCacheProfilesServeExactHeaders(t *testing.T) {
	for _, tc := range []struct {
		profile CacheProfile
		want    string
	}{
		{CacheDefault, "no-store, max-age=0"},
		{CachePublicImmutable, "public, max-age=31536000, immutable"},
		{CachePrivateRevalidate, "private, no-cache"},
		{CachePrivateNoStore, "private, no-store"},
	} {
		srv := NewAssetServer(nil, "/__c", nil)
		srv.AddBytes("/__c/a.js", "text/javascript; charset=utf-8", false, []byte("var x=1;"),
			WithCache(tc.profile))
		rt := router.New()
		srv.Register(rt)
		hs := httptest.NewServer(rt)
		resp, err := http.Get(hs.URL + "/__c/a.js")
		if err != nil {
			t.Fatalf("profile %d: GET a.js: %v", tc.profile, err)
		}
		resp.Body.Close()
		hs.Close()
		if got := resp.Header.Get("Cache-Control"); got != tc.want {
			t.Errorf("profile %d: Cache-Control=%q want %q", tc.profile, got, tc.want)
		}
	}

	// An asset registered with NO options keeps the pre-profile header.
	srv := NewAssetServer(nil, "/__c", nil)
	srv.AddBytes("/__c/plain.js", "text/javascript; charset=utf-8", false, []byte("var x=1;"))
	rt := router.New()
	srv.Register(rt)
	hs := httptest.NewServer(rt)
	defer hs.Close()
	resp, err := http.Get(hs.URL + "/__c/plain.js")
	if err != nil {
		t.Fatalf("GET plain.js: %v", err)
	}
	resp.Body.Close()
	if got := resp.Header.Get("Cache-Control"); got != "no-store, max-age=0" {
		t.Errorf("no-option Cache-Control=%q want the historical no-store, max-age=0", got)
	}
}
