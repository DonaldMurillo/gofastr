package pluginhost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// adversarialAssets wires one AssetServer with a filesystem that contains
// files NO route should ever be able to reach (secret.txt, deep/nest.js),
// plus both AddBytes flavours and the two platform routes, so every surface
// shares one fixture.
func adversarialAssets(t *testing.T) *router.Router {
	t.Helper()
	fsys := fstest.MapFS{
		"editor.html":  &fstest.MapFile{Data: []byte("EDITOR-HTML")},
		"editor.js":    &fstest.MapFile{Data: []byte("EDITOR-JS")},
		"secret.txt":   &fstest.MapFile{Data: []byte("SHOULD-NOT-SERVE")},
		"deep/nest.js": &fstest.MapFile{Data: []byte("NESTED-SECRET")},
		"raw.notes":    &fstest.MapFile{Data: []byte("NOTES")}, // unknown extension
	}
	srv := NewAssetServer(fsys, "/__p", []AssetSpec{
		{Name: "editor.html", Framed: true},
		{Name: "editor.js", Framed: true},
		{Name: "raw.notes", Framed: false}, // unknown extension
	})
	srv.AddBytes("/__p/host.js", "", false, []byte("HOST-JS"))
	srv.AddBytes("/__p/frame", "", true, []byte("FRAME-NOEXT")) // no extension at all
	rt := router.New()
	srv.Register(rt)
	RegisterBrokerRoute(rt)
	RegisterFrameClientRoute(rt)
	return rt
}

// Property: the bytes an asset route serves are fixed at REGISTRATION; the
// request URL can never steer which file is read, into or out of the prefix.
// The handler ignores r.URL entirely (it reads spec.Name), so the only way in
// is the exact registered pattern — everything else must 404 or redirect
// back inside the prefix. Surfaces: the FS spec route, both AddBytes
// flavours, the broker platform route, and the frame-client platform route.
func TestAssetRouteServesOnlyRegisteredBytes(t *testing.T) {
	rt := adversarialAssets(t)

	// Every steering shape, tried against the asset prefix (and a sibling
	// prefix for the escape direction).
	attacks := []string{
		"/__p/editor.html/../../secret.txt", // dot segments past the file
		"/__p/%2e%2e/secret.txt",            // encoded dot segments out of the prefix
		"/__p/deep/../editor.js",            // dot segments inside the FS
		"/__p/host.js?name=secret.txt",      // query confusion
		"/__p2/host.js",                     // sibling prefix
	}
	for _, attack := range attacks {
		req := httptest.NewRequest(http.MethodGet, attack, nil)
		req.Host = "probe.example"
		rec := httptest.NewRecorder()
		rt.ServeHTTP(rec, req)
		body := rec.Body.String()
		if strings.Contains(body, "SHOULD-NOT-SERVE") || strings.Contains(body, "NESTED-SECRET") {
			t.Errorf("steering path %q served unreferenced file bytes (status %d)", attack, rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "" && !strings.HasPrefix(loc, "/__p") {
			t.Errorf("steering path %q redirected OUT of the prefix to %q", attack, loc)
		}
	}

	// The exact registered routes still serve their own bytes.
	for path, want := range map[string]string{
		"/__p/editor.html":   "EDITOR-HTML",
		"/__p/editor.js":     "EDITOR-JS",
		"/__p/host.js":       "HOST-JS",
		BrokerScriptURL:      "pluginhost.js",  // prefix of the JS banner
		FrameClientScriptURL: "frameclient.js", // prefix of the JS banner
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "probe.example"
		rec := httptest.NewRecorder()
		rt.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: status %d, want 200", path, rec.Code)
			continue
		}
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("GET %s served %q, want %q", path, rec.Body.String(), want)
		}
	}

	// The prefix itself is not a directory listing.
	for _, p := range []string{"/__p", "/__p/", "/__p/nonexistent.js"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		req.Host = "probe.example"
		rec := httptest.NewRecorder()
		rt.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Errorf("GET %s: status 200, want 404/redirect", p)
		}
	}
}

// Property: a request-controlled scheme/host can never reach the framed CSP
// unvalidated, on ANY surface that emits one. TestAssetServerRejectsOriginInjection
// pins two shapes on ONE FS route; this loops the remaining framed surfaces
// (the AddBytes framed asset and the platform frame-client route) through the
// other injection classes: a forwarded-proto LIST, whitespace padding, the
// uppercase spelling, and control characters in the Host.
func TestFramedAssetsRefusePoisonedOrigin(t *testing.T) {
	rt := adversarialAssets(t)

	surfaces := []string{"/__p/editor.html", "/__p/frame", FrameClientScriptURL}
	poisoned := []struct {
		name string
		host string
		xfp  string
	}{
		{"forwarded proto list", "probe.example", "https,http"},
		{"forwarded proto padded", "probe.example", " https"},
		{"forwarded proto uppercase", "probe.example", "HTTPS"},
		{"tab in host", "probe\texample", ""},
		{"space in host", "probe example", ""},
	}
	for _, tc := range poisoned {
		for _, path := range surfaces {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Host = tc.host
			if tc.xfp != "" {
				req.Header.Set("X-Forwarded-Proto", tc.xfp)
			}
			rec := httptest.NewRecorder()
			rt.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s at %s: status %d, want 400", tc.name, path, rec.Code)
			}
			if csp := rec.Header().Get("Content-Security-Policy"); csp != "" {
				t.Errorf("%s at %s: refused request still emitted a CSP %q", tc.name, path, csp)
			}
		}
	}

	// The legitimate forwarded scheme serves on every framed surface.
	for _, path := range surfaces {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "probe.example"
		req.Header.Set("X-Forwarded-Proto", "https")
		rec := httptest.NewRecorder()
		rt.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("valid https X-Forwarded-Proto at %s: status %d, want 200", path, rec.Code)
			continue
		}
		if csp := rec.Header().Get("Content-Security-Policy"); !strings.HasPrefix(csp, "sandbox allow-scripts; default-src https://probe.example") {
			t.Errorf("valid https origin at %s keyed the CSP wrong: %q", path, csp)
		}
	}
}

// Property: no asset is ever served without a usable Content-Type, because
// nosniff sits on the next line and makes the empty header load-bearing
// (#303): a 200 with the right bytes and no type is a document no browser
// parses and no log explains. An extension the detector does not know floors
// to application/octet-stream rather than empty, on both the FS and the
// AddBytes surface.
func TestAssetContentTypeNeverEmptyOrSniffable(t *testing.T) {
	rt := adversarialAssets(t)

	expect := map[string]string{
		"/__p/raw.notes":     "application/octet-stream", // unknown FS extension floors
		"/__p/frame":         "application/octet-stream", // AddBytes route with no extension floors
		"/__p/host.js":       "text/javascript",          // AddBytes derives from route extension
		BrokerScriptURL:      "text/javascript",          // platform routes are explicit
		FrameClientScriptURL: "text/javascript",
		"/__p/editor.html":   "text/html",
	}
	for path, wantPrefix := range expect {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "probe.example"
		rec := httptest.NewRecorder()
		rt.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: status %d, want 200", path, rec.Code)
			continue
		}
		ct := rec.Header().Get("Content-Type")
		if ct == "" {
			t.Errorf("GET %s served with an EMPTY Content-Type under nosniff (#303)", path)
			continue
		}
		if !strings.HasPrefix(ct, wantPrefix) {
			t.Errorf("GET %s Content-Type=%q, want prefix %q", path, ct, wantPrefix)
		}
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("GET %s nosniff=%q, want nosniff", path, got)
		}
	}
}
