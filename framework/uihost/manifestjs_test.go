package uihost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// actScreen renders a component root and registers a click action, so
// AutoCompileActions compiles a registry under the route-derived id.
type actScreen struct{}

func (s *actScreen) Render() render.HTML {
	return render.HTML(`<div data-component="act"><button>go</button></div>`)
}

func (s *actScreen) Actions() {
	component.On("click", func(ctx *component.ComponentContext) { _ = ctx })
}

func actionsHost() *UIHost {
	application := app.NewApp("t")
	application.Register("/act", &actScreen{}, nil)
	application.Register("/plain", &testHomeComp{}, nil)
	ds := New(application)
	ds.AutoCompileActions()
	return ds
}

// manifest.js carries the externalized data blocks and follows the
// shared versioned-text policy.
func TestManifestJSContent(t *testing.T) {
	ds := actionsHost()

	req := httptest.NewRequest("GET", "/__gofastr/manifest.js", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		"window.__gofastr_catalog=",
		"window.__gofastr_runtime_modules=",
		"window.__gofastr_actions=",
		`"act":"`, // the compiled screen's action hash
	} {
		if !strings.Contains(body, want) {
			t.Errorf("manifest.js missing %q", want)
		}
	}
	if w.Header().Get("ETag") == "" {
		t.Error("manifest.js carries no ETag")
	}
}

// Live pages reference manifest.js and per-screen action scripts; the
// inline catalog/module blocks and the whole-app actions.js are gone.
func TestPageExternalizesDataBlocks(t *testing.T) {
	ds := actionsHost()

	req := httptest.NewRequest("GET", "/act", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	page := w.Body.String()

	if !strings.Contains(page, "/__gofastr/manifest.js?v=") {
		t.Error("page missing manifest.js reference")
	}
	if !strings.Contains(page, "/__gofastr/widget/act.js?v=") {
		t.Error("page missing its per-screen action script")
	}
	if strings.Contains(page, `id="gofastr-catalog"`) {
		t.Error("inline catalog block still emitted on a live page")
	}
	if strings.Contains(page, `id="gofastr-runtime-modules"`) {
		t.Error("inline module manifest still emitted on a live page")
	}
	if strings.Contains(page, `src="/__gofastr/actions.js"`) {
		t.Error("whole-app actions.js still referenced on a live page")
	}
	// manifest.js must precede runtime.js — its globals are read at boot.
	if strings.Index(page, "/__gofastr/manifest.js") > strings.Index(page, "/__gofastr/runtime.js") {
		t.Error("manifest.js must be injected before runtime.js")
	}

	// A page without the component must not ship its action script.
	req2 := httptest.NewRequest("GET", "/plain", nil)
	w2 := httptest.NewRecorder()
	ds.ServeHTTP(w2, req2)
	if strings.Contains(w2.Body.String(), "/__gofastr/widget/act.js") {
		t.Error("unrelated page ships another screen's action script")
	}
}

// The per-id endpoint is session-gated like the concat it replaces (it
// used to be LESS gated), and immutable under its content hash.
func TestWidgetJSGatedAndVersioned(t *testing.T) {
	ds := actionsHost()

	req := httptest.NewRequest("GET", "/__gofastr/widget/act.js", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
		t.Fatalf("ungated widget JS: status %d, want 401/403", w.Code)
	}

	sess := ds.CreateSession()
	ds.mu.RLock()
	hash := ds.actionHash["act"]
	ds.mu.RUnlock()
	req2 := httptest.NewRequest("GET", "/__gofastr/widget/act.js?v="+hash, nil)
	req2.AddCookie(&http.Cookie{Name: sessionCookieSecureName, Value: sess.Token})
	req2.AddCookie(&http.Cookie{Name: sessionCookieDevName, Value: sess.Token})
	w2 := httptest.NewRecorder()
	ds.ServeHTTP(w2, req2)
	if w2.Code != 200 {
		t.Fatalf("gated fetch with session: status %d", w2.Code)
	}
	cc := w2.Header().Get("Cache-Control")
	if strings.Contains(cc, "immutable") || !strings.Contains(cc, "private") || !strings.Contains(cc, "no-cache") {
		t.Errorf("Cache-Control = %q, want private no-cache — an immutable gated asset outlives its credential in the browser cache, so the gate would run only on the first miss", cc)
	}
	if w2.Header().Get("ETag") == "" {
		t.Error("gated asset needs an ETag so per-request revalidation is a body-less 304")
	}
}

// A prefetch presented with a dead session gets 204 and no mint — the
// client never caches it, so the real click performs the rollover. A
// prefetch that were served instead would let the click paint from the
// cached entry with the stale token still in place.
func TestPrefetchWithDeadSessionGets204(t *testing.T) {
	ds := actionsHost()

	req := httptest.NewRequest("GET", "/act", nil)
	req.Header.Set("X-Gofastr-Navigate", "1")
	req.Header.Set("X-Gofastr-From", "/plain")
	req.Header.Set("X-Gofastr-Prefetch", "1")
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("prefetch with no session: status %d, want 204", w.Code)
	}
	if len(w.Result().Cookies()) != 0 {
		t.Error("prefetch must not mint a session")
	}
	if w.Header().Get("X-Gofastr-Partial") != "" {
		t.Error("204 must not claim to be a partial")
	}

	// With a live session the prefetch serves the partial normally.
	sess := ds.CreateSession()
	req2 := httptest.NewRequest("GET", "/act", nil)
	req2.Header.Set("X-Gofastr-Navigate", "1")
	req2.Header.Set("X-Gofastr-From", "/plain")
	req2.Header.Set("X-Gofastr-Prefetch", "1")
	req2.AddCookie(&http.Cookie{Name: sessionCookieSecureName, Value: sess.Token})
	req2.AddCookie(&http.Cookie{Name: sessionCookieDevName, Value: sess.Token})
	w2 := httptest.NewRecorder()
	ds.ServeHTTP(w2, req2)
	if w2.Code != 200 || w2.Header().Get("X-Gofastr-Partial") != "true" {
		t.Fatalf("prefetch with live session: status %d partial=%q, want a normal partial", w2.Code, w2.Header().Get("X-Gofastr-Partial"))
	}
}
