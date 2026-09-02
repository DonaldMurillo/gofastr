package webmcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// requireRole answers 401 without a session cookie and 403 when the
// session's role (the cookie value) is not the wanted one — the shape
// a support-console gate takes in real apps.
func requireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie("session")
			if err != nil || c.Value == "" {
				http.Error(w, "sign in", http.StatusUnauthorized)
				return
			}
			if c.Value != role {
				http.Error(w, "wrong role", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func get(t *testing.T, rt *router.Router, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, req)
	return rec
}

func mountedHost(t *testing.T, opts ...MountOption) (*router.Router, string) {
	t.Helper()
	h := New()
	if err := h.Register(validTool()); err != nil {
		t.Fatal(err)
	}
	rt := router.New()
	url, err := h.Mount(rt, nil, opts...)
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	return rt, url
}

// The zero-config public mount keeps the policies it has always had:
// immutable hash-versioned script, no-cache manifest. A scoped mount
// must not quietly change the default.
func TestDefaultMountKeepsPublicPolicies(t *testing.T) {
	rt, scriptURL := mountedHost(t)

	rec := get(t, rt, scriptURL)
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Fatalf("script cache-control: %q", cc)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/javascript") {
		t.Fatalf("script content type: %q", ct)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("script has no ETag")
	}
	req := httptest.NewRequest(http.MethodGet, scriptURL, nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("script revalidation: %d", rec.Code)
	}

	rec = get(t, rt, ManifestRoute)
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("manifest cache-control: %q", cc)
	}
	if rec.Header().Get("ETag") == "" {
		t.Fatal("manifest has no ETag")
	}
	if !strings.Contains(rec.Body.String(), `"name":"echo"`) {
		t.Fatal("manifest does not carry the tool set")
	}
}

func TestAssetAuthorizationGuardsAssets(t *testing.T) {
	rt, scriptURL := mountedHost(t, WithAssetAuthorization(requireRole("support")))
	support := []*http.Cookie{{Name: "session", Value: "support"}}
	operator := []*http.Cookie{{Name: "session", Value: "operator"}}

	// Anonymous and wrong-role requests fail on BOTH assets: the
	// manifest names every tool, so discovery is an authority surface
	// even when the endpoints authorize execution.
	for _, path := range []string{scriptURL, ManifestRoute} {
		if rec := get(t, rt, path); rec.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous %s: %d", path, rec.Code)
		}
		if rec := get(t, rt, path, operator...); rec.Code != http.StatusForbidden {
			t.Fatalf("wrong role %s: %d", path, rec.Code)
		}
		if rec := get(t, rt, path, support...); rec.Code != http.StatusOK {
			t.Fatalf("support %s: %d", path, rec.Code)
		}
	}
}

// The shared-cache guard: an authenticated fetch of a credential-gated
// asset must never be replayable by a shared cache to anonymous
// traffic — private, no-store, and no public/immutable anywhere, even
// though the app never passed WithPrivateAssets.
func TestAuthenticatedAssetsNotSharedCacheable(t *testing.T) {
	for name, opt := range map[string]MountOption{
		"asset-authorization": WithAssetAuthorization(requireRole("support")),
		"page-scope": WithPageScope(func(r *http.Request) bool {
			c, err := r.Cookie("session")
			return err == nil && c.Value == "support"
		}),
	} {
		rt, scriptURL := mountedHost(t, opt)
		for _, path := range []string{scriptURL, ManifestRoute} {
			rec := get(t, rt, path, &http.Cookie{Name: "session", Value: "support"})
			if rec.Code != http.StatusOK {
				t.Fatalf("%s %s: %d", name, path, rec.Code)
			}
			cc := rec.Header().Get("Cache-Control")
			if cc != "private, no-store" {
				t.Fatalf("%s %s cache-control: %q, want %q", name, path, cc, "private, no-store")
			}
			low := strings.ToLower(cc)
			if strings.Contains(low, "public") || strings.Contains(low, "immutable") {
				t.Fatalf("%s %s is shared-cacheable: %q", name, path, cc)
			}
		}
	}
}

func TestPrivateAssetsOptionSetsNoStore(t *testing.T) {
	rt, scriptURL := mountedHost(t, WithPrivateAssets())
	for _, path := range []string{scriptURL, ManifestRoute} {
		rec := get(t, rt, path)
		if cc := rec.Header().Get("Cache-Control"); cc != "private, no-store" {
			t.Fatalf("%s cache-control: %q", path, cc)
		}
		// ETag revalidation still works for the browser cache.
		etag := rec.Header().Get("ETag")
		if etag == "" {
			t.Fatalf("%s has no ETag", path)
		}
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("If-None-Match", etag)
		rec = httptest.NewRecorder()
		rt.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotModified {
			t.Fatalf("%s revalidation: %d", path, rec.Code)
		}
	}
}

func TestPageScopeServesNothingOutOfScope(t *testing.T) {
	inScope := func(r *http.Request) bool {
		c, err := r.Cookie("session")
		return err == nil && c.Value == "support"
	}
	rt, scriptURL := mountedHost(t, WithPageScope(inScope))
	anon := get(t, rt, scriptURL)
	if anon.Code != http.StatusOK {
		t.Fatalf("out-of-scope script: %d", anon.Code)
	}
	if anon.Body.Len() != 0 {
		t.Fatalf("out-of-scope script served %d bytes of bridge", anon.Body.Len())
	}
	if cc := anon.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Fatalf("out-of-scope script cache-control: %q", cc)
	}
	rec := get(t, rt, ManifestRoute)
	if rec.Body.String() != `{"tools":[]}` {
		t.Fatalf("out-of-scope manifest must be an empty tool set a client can parse, got %q", rec.Body.String())
	}

	// In scope, the full bridge and manifest come back.
	full := get(t, rt, scriptURL, &http.Cookie{Name: "session", Value: "support"})
	if !strings.Contains(full.Body.String(), `\"name\":\"echo\"`) {
		t.Fatal("in-scope script does not carry the tool manifest")
	}
	rec = get(t, rt, ManifestRoute, &http.Cookie{Name: "session", Value: "support"})
	if !strings.Contains(rec.Body.String(), `"name":"echo"`) {
		t.Fatal("in-scope manifest does not carry the tool set")
	}
}

// Authorization runs before the scope predicate: an anonymous request
// to a mount that has both fails with the middleware's status, never
// with the scope gate's empty 200. Page inclusion is not authorization.
func TestAuthRunsBeforePageScope(t *testing.T) {
	inScope := func(r *http.Request) bool { return false }
	rt, scriptURL := mountedHost(t, WithAssetAuthorization(requireRole("support")), WithPageScope(inScope))
	if rec := get(t, rt, scriptURL); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous out-of-scope script: %d, want 401 from the auth middleware", rec.Code)
	}
	if rec := get(t, rt, ManifestRoute); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous out-of-scope manifest: %d, want 401", rec.Code)
	}
}

// plainRegistrar implements only the every-page seam; a host that
// cannot carry a document script must fail the mount loudly.
type plainRegistrar struct{}

func (plainRegistrar) RegisterExternalScript(string) error { return nil }

// homeScreen is the minimal screen the uihost integration below needs.
type homeScreen struct{}

func (homeScreen) Render() render.HTML         { return html.Div(html.DivConfig{}, render.Text("hi")) }
func (homeScreen) SetParams(map[string]string) {}

func TestWithDocumentScopeNeedsDocumentRail(t *testing.T) {
	newHost := func() *Host {
		h := New()
		if err := h.Register(validTool()); err != nil {
			t.Fatal(err)
		}
		return h
	}
	scope := func(string) bool { return true }

	if _, err := newHost().Mount(router.New(), nil, WithDocumentScope(scope)); err == nil {
		t.Error("nil registrar with WithDocumentScope = nil error, want refusal")
	}
	if _, err := newHost().Mount(router.New(), plainRegistrar{}, WithDocumentScope(scope)); err == nil {
		t.Error("registrar without RegisterDocumentScript = nil error, want refusal")
	}

	// A failed mount leaves the Host re-mountable: the registrar-less
	// refusal above registered nothing.
	h := newHost()
	if _, err := h.Mount(router.New(), nil, WithDocumentScope(scope)); err == nil {
		t.Fatal("expected refusal")
	}
	if _, err := h.Mount(router.New(), nil); err != nil {
		t.Fatalf("re-mount after refusal: %v", err)
	}
}

// WithDocumentScope on a real uihost: the bridge tag rides the
// document rail — data-fui-doc, in scope only — and the route manifest
// carries docScripts for the scoped route so the client sees the
// boundary.
func TestWithDocumentScopeRidesHostRail(t *testing.T) {
	application := app.NewApp("docscope")
	application.RegisterScreen(app.NewScreen("/", &homeScreen{}), nil)
	application.RegisterScreen(app.NewScreen("/support", &homeScreen{}), nil)
	ds := uihost.New(application)
	rt := router.New()
	ds.Mount(rt)

	h := New()
	if err := h.Register(validTool()); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Mount(rt, ds, WithDocumentScope(func(path string) bool {
		return strings.HasPrefix(path, "/support")
	})); err != nil {
		t.Fatalf("mount: %v", err)
	}

	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/support", nil))
	page := rec.Body.String()
	if n := strings.Count(page, `data-fui-doc`); n != 1 {
		t.Errorf("/support carries %d data-fui-doc tags, want 1:\n%s", n, page)
	}
	if !strings.Contains(page, `"docScripts":["/__gofastr/webmcp.js`) {
		t.Errorf("route manifest does not declare the bridge for /support:\n%s", page)
	}

	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	home := rec.Body.String()
	if strings.Contains(home, `data-fui-doc`) || strings.Contains(home, `src="/__gofastr/webmcp.js`) {
		t.Errorf("out-of-scope / ships the bridge:\n%s", home)
	}
}
