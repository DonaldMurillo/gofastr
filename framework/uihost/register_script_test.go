package uihost

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// RegisterExternalScript exists for plugins/batteries wiring in Init, which
// runs after Mount froze the construction-time options. This file pins the
// rail's contract: validation, dedupe/order, full-shell-only emission, and
// the refuse-once-serving guard.

func TestRegisterExternalScriptRejectsUnsafe(t *testing.T) {
	ds := newTestUIHost()
	for _, src := range []string{
		"https://evil.com/x.js", // cross-origin scheme
		"//evil.com/x.js",       // protocol-relative
		"javascript:x",          // dangerous scheme, no path
		"..%2F",                 // traversal, no leading /
		"/a/../b.js",            // traversal segment
		"/a/./b.js",             // dot segment
		"/%2e%2e/b.js",          // encoded traversal segment
		`/a\b.js`,               // backslash
		"/a%5Cb.js",             // encoded backslash
		"",                      // empty
		"?v=1",                  // query only, no path
		"#f",                    // fragment only
		"/x.js#f",               // fragment on a path
		"/x\x00.js",             // NUL
		"/x\n.js",               // control chars (attribute injection)
		"/x\r.js",
		"/x\t.js",
		"x.js", // relative, not an absolute path
	} {
		if err := ds.RegisterExternalScript(src); err == nil {
			t.Errorf("RegisterExternalScript(%q) = nil, want error", src)
		}
	}
}

func TestRegisterExternalScriptAcceptsValid(t *testing.T) {
	ds := newTestUIHost()
	for _, src := range []string{"/x.js", "/x.js?v=abc", "/a/b/c.js", "/x.js?"} {
		if err := ds.RegisterExternalScript(src); err != nil {
			t.Errorf("RegisterExternalScript(%q) = %v, want nil", src, err)
		}
	}
}

func TestRegisterExternalScriptDedupesInOrder(t *testing.T) {
	ds := newTestUIHost()
	for _, src := range []string{"/b.js", "/a.js", "/b.js"} {
		if err := ds.RegisterExternalScript(src); err != nil {
			t.Fatalf("RegisterExternalScript(%q) = %v, want nil (duplicate must be idempotent)", src, err)
		}
	}
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	page := w.Body.String()
	bTag := `<script src="/b.js"></script>`
	aTag := `<script src="/a.js"></script>`
	if n := strings.Count(page, bTag); n != 1 {
		t.Errorf("duplicate /b.js emitted %d times, want exactly 1", n)
	}
	if n := strings.Count(page, aTag); n != 1 {
		t.Errorf("/a.js emitted %d times, want exactly 1", n)
	}
	if strings.Index(page, bTag) > strings.Index(page, aTag) {
		t.Error("first-registered /b.js must precede /a.js in the rail")
	}
}

func TestRegisterScriptRefusedAfterServing(t *testing.T) {
	ds := newTestUIHost()
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != 200 {
		t.Fatalf("first page render: status %d", w.Code)
	}
	err := ds.RegisterExternalScript("/x.js")
	if err == nil {
		t.Fatal("RegisterExternalScript after a page was served = nil, want error")
	}
	if !strings.Contains(err.Error(), "serving") {
		t.Errorf("error should name serving, got %q", err.Error())
	}
}

func TestExtraScriptEmittedFullPageOnly(t *testing.T) {
	ds := newTestUIHost()
	if err := ds.RegisterExternalScript("/plug.js?v=abc"); err != nil {
		t.Fatalf("RegisterExternalScript: %v", err)
	}
	tag := `<script src="/plug.js?v=abc"></script>`

	// Full shell render: exactly once, after runtime.js.
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	page := w.Body.String()
	if n := strings.Count(page, tag); n != 1 {
		t.Errorf("full page emits the tag %d times, want 1", n)
	}
	rt, tagAt := strings.Index(page, "/__gofastr/runtime.js"), strings.Index(page, tag)
	if rt == -1 || tagAt == -1 || tagAt < rt {
		t.Errorf("extra script must load after runtime.js (runtime@%d, tag@%d)", rt, tagAt)
	}

	// SPA partial navigation response: content cell only, no script rail.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Gofastr-Navigate", "1")
	pw := httptest.NewRecorder()
	ds.ServeHTTP(pw, req)
	if strings.Contains(pw.Body.String(), tag) {
		t.Error("partial response must not carry the extra script rail")
	}
}

// A registered same-origin script needs no CSP widening: default-src 'self'
// already covers it, and the header must stay byte-identical (the exact
// framework default) across registration.
func TestRegisterScriptKeepsDefaultCSP(t *testing.T) {
	const defaultCSP = "default-src 'self'; img-src 'self' data:; object-src 'none'; " +
		"form-action 'self'; frame-ancestors 'none'; base-uri 'self'"

	get := func(ds *UIHost) string {
		t.Helper()
		w := httptest.NewRecorder()
		ds.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		csp := w.Header().Get("Content-Security-Policy")
		if csp == "" {
			t.Fatal("page response carries no Content-Security-Policy")
		}
		return csp
	}

	before := get(newTestUIHost())

	withScript := newTestUIHost()
	if err := withScript.RegisterExternalScript("/plug.js?v=abc"); err != nil {
		t.Fatalf("RegisterExternalScript: %v", err)
	}
	after := get(withScript)

	if before != defaultCSP || after != defaultCSP || before != after {
		t.Errorf("CSP changed across script registration:\nbefore: %q\nafter:  %q", before, after)
	}
}

// setParamsComp is testHomeComp with the SetParams a dynamic route
// requires, so "/session/:id" can register.
type setParamsComp struct{ testHomeComp }

func (setParamsComp) SetParams(map[string]string) {}

// A document-lifetime script ships only on pages its scope accepts,
// and the tag carries data-fui-doc so the runtime can read the live
// document's capability set from the DOM.
func TestDocumentScriptScopedEmission(t *testing.T) {
	ds := newTestUIHost()
	ds.App.RegisterScreen(app.NewScreen("/session/:id", &setParamsComp{}).WithTitle("Session"), nil)
	if err := ds.RegisterDocumentScript("/cap.js", func(path string) bool {
		return strings.HasPrefix(path, "/session/")
	}); err != nil {
		t.Fatalf("RegisterDocumentScript: %v", err)
	}

	page := func(path string) string {
		t.Helper()
		w := httptest.NewRecorder()
		ds.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != 200 {
			t.Fatalf("GET %s: %d", path, w.Code)
		}
		return w.Body.String()
	}

	// In scope, pattern AND concrete path: tagged, exactly once.
	tag := `<script src="/cap.js" data-fui-doc></script>`
	for _, p := range []string{"/session/42", "/session/7"} {
		if n := strings.Count(page(p), tag); n != 1 {
			t.Errorf("%s emits the document script %d times, want 1:\n%s", p, n, truncate(page(p), 400))
		}
	}
	// Out of scope: no tag at all, not even untagged. (The inline route
	// manifest still NAMES /cap.js for the in-scope route — that is the
	// boundary declaration, not an emission.)
	if home := page("/"); strings.Contains(home, `src="/cap.js"`) {
		t.Errorf("out-of-scope page emits the document script:\n%s", truncate(home, 400))
	}
}

// The route manifest carries each route's document-script set, keyed by
// the route PATTERN (prefix-style scopes answer the pattern and its
// concrete paths identically). Out-of-scope routes omit the field.
func TestDocumentScriptInRouteManifest(t *testing.T) {
	ds := newTestUIHost()
	ds.App.RegisterScreen(app.NewScreen("/session/:id", &setParamsComp{}).WithTitle("Session"), nil)
	if err := ds.RegisterDocumentScript("/cap.js", func(path string) bool {
		return strings.HasPrefix(path, "/session/")
	}); err != nil {
		t.Fatalf("RegisterDocumentScript: %v", err)
	}

	var session, home map[string]any
	for _, e := range manifestEntries(t, ds) {
		switch e["path"] {
		case "/session/:id":
			session = e
		case "/":
			home = e
		}
	}
	if session == nil || home == nil {
		t.Fatalf("manifest missing routes, got %v", manifestEntries(t, ds))
	}
	if got := session["docScripts"]; !reflect.DeepEqual(got, []any{"/cap.js"}) {
		t.Errorf("/session/:id docScripts = %v, want [/cap.js]", got)
	}
	if _, ok := home["docScripts"]; ok {
		t.Errorf("out-of-scope route / carries docScripts: %v", home["docScripts"])
	}
}

// Multiple document scripts sort into a deterministic manifest array;
// unscoped (every-page) rail entries never appear in docScripts.
func TestDocumentScriptManifestSorted(t *testing.T) {
	ds := newTestUIHost()
	for _, src := range []string{"/b.js", "/a.js"} {
		if err := ds.RegisterDocumentScript(src, func(string) bool { return true }); err != nil {
			t.Fatalf("RegisterDocumentScript(%q): %v", src, err)
		}
	}
	if err := ds.RegisterExternalScript("/plain.js"); err != nil {
		t.Fatalf("RegisterExternalScript: %v", err)
	}
	for _, e := range manifestEntries(t, ds) {
		if e["path"] != "/" {
			continue
		}
		if got := e["docScripts"]; !reflect.DeepEqual(got, []any{"/a.js", "/b.js"}) {
			t.Errorf("docScripts = %v, want [/a.js /b.js]", got)
		}
		return
	}
	t.Fatal("manifest has no / entry")
}

// One src, one lifetime, one scope: nil scope, a src already on the
// rail as every-page, a src already document-scoped, and registration
// after serving began are all refused.
func TestDocumentScriptRegistrationRefusals(t *testing.T) {
	if err := newTestUIHost().RegisterDocumentScript("/cap.js", nil); err == nil {
		t.Error("nil scope = nil error, want refusal")
	}

	ds := newTestUIHost()
	if err := ds.RegisterExternalScript("/cap.js"); err != nil {
		t.Fatal(err)
	}
	if err := ds.RegisterDocumentScript("/cap.js", func(string) bool { return true }); err == nil {
		t.Error("document registration of an every-page src = nil error, want refusal")
	}

	ds2 := newTestUIHost()
	if err := ds2.RegisterDocumentScript("/cap.js", func(string) bool { return true }); err != nil {
		t.Fatal(err)
	}
	if err := ds2.RegisterDocumentScript("/cap.js", func(string) bool { return false }); err == nil {
		t.Error("second scope on one src = nil error, want refusal")
	}

	ds3 := newTestUIHost()
	w := httptest.NewRecorder()
	ds3.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if err := ds3.RegisterDocumentScript("/cap.js", func(string) bool { return true }); err == nil {
		t.Error("registration after serving began = nil error, want refusal")
	}
}
