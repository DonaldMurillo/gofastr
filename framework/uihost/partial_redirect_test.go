package uihost

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ctxOnlyScreen is the smallest ContextOnly demonstration: no Render
// method on the type at all, RenderCtx provides the content, the
// embedded ContextOnly satisfies the Component interface via its
// stub Render that the framework never calls.
type ctxOnlyScreen struct {
	component.ContextOnly
}

func (s *ctxOnlyScreen) RenderCtx(ctx context.Context) render.HTML {
	return render.HTML(`<div id="ctx-only-content">ctx-only-content</div>`)
}

// TestPartialRedirect_Returns200WithLocationHeader pins the dispatch
// contract for policy-Redirect outcomes on a partial fetch
// (X-Gofastr-Navigate: 1). The runtime fetcher uses redirect:'follow',
// so a 303 would be chased silently and the X-Gofastr-Location signal
// would never reach client JS. We must return 200 + the header + empty
// body so the SPA router can read the destination and pushState.
func TestPartialRedirect_Returns200WithLocationHeader(t *testing.T) {
	pol := app.PolicyFunc(func(ctx context.Context) app.Decision {
		return decide.Redirect("/login")
	})
	application := app.NewApp("t")
	application.RegisterScreen(
		app.NewScreen("/dash", &testHomeComp{}).WithPolicy(pol),
		nil,
	)
	ds := New(application)

	req := httptest.NewRequest("GET", "/dash", nil)
	req.Header.Set("X-Gofastr-Navigate", "1")
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	if got := w.Code; got != 200 {
		t.Fatalf("partial redirect: status = %d, want 200 (303 would be auto-followed by the runtime fetch)", got)
	}
	if got := w.Header().Get("X-Gofastr-Location"); got != "/login" {
		t.Fatalf("partial redirect: X-Gofastr-Location = %q, want /login", got)
	}
	if got := w.Body.String(); got != "" {
		t.Fatalf("partial redirect: body should be empty, got %q", got)
	}
}

// TestPartialRedirect_RejectsUnsafeURLs pins the open-redirect
// defense: a policy that returns decide.Redirect with a
// protocol-relative ("//evil.com"), absolute ("https://evil.com"), or
// scheme-bearing ("javascript:...") URL MUST NOT be emitted via
// X-Gofastr-Location. That header is read by the runtime and fed
// directly into loadPage(), which would issue a cross-origin fetch
// with credentials.
//
// Behaviour: an absolute http(s) URL goes out as X-Gofastr-Document,
// which the runtime loads as a page: fetch would chase a 303 there,
// and a site that refuses CORS (an identity provider's login) failed
// it with a "could not load" toast. Every other unsafe URL falls
// through to a hard 303. Safe relative paths use the SPA-nav header
// path.
func TestPartialRedirect_RejectsUnsafeURLs(t *testing.T) {
	cases := []struct {
		name        string
		url         string
		wantHeader  bool   // expect X-Gofastr-Location set
		wantDoc     string // expect this X-Gofastr-Document
		wantStatus  int    // expect this HTTP status
		description string
	}{
		{"safe relative", "/login", true, "", 200, "normal SPA-nav path"},
		{"safe relative with query", "/login?next=/x", true, "", 200, "query is fine"},
		{"protocol-relative", "//evil.com/x", false, "", 303, "browser-safe hard redirect"},
		{"absolute https", "https://idp.example/x", false, "https://idp.example/x", 200, "the runtime loads it as a page"},
		{"absolute http", "http://idp.example/x", false, "http://idp.example/x", 200, "the runtime loads it as a page"},
		{"absolute without host", "https:/x", false, "", 303, "no host to load"},
		{"javascript scheme", "javascript:alert(1)", false, "", 303, "browser blocks the scheme"},
		{"data scheme", "data:text/html,<script>", false, "", 303, "browser blocks the scheme"},
		{"backslash bypass", "/\\evil.com", false, "", 303, "browsers normalise \\ to / → //evil"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pol := app.PolicyFunc(func(ctx context.Context) app.Decision {
				return decide.Redirect(tc.url)
			})
			application := app.NewApp("t")
			application.RegisterScreen(
				app.NewScreen("/dash", &testHomeComp{}).WithPolicy(pol),
				nil,
			)
			ds := New(application)
			req := httptest.NewRequest("GET", "/dash", nil)
			req.Header.Set("X-Gofastr-Navigate", "1")
			w := httptest.NewRecorder()
			ds.ServeHTTP(w, req)

			if got := w.Code; got != tc.wantStatus {
				t.Errorf("status = %d, want %d (%s)", got, tc.wantStatus, tc.description)
			}
			if got := w.Header().Get("X-Gofastr-Document"); got != tc.wantDoc {
				t.Errorf("X-Gofastr-Document = %q, want %q", got, tc.wantDoc)
			}
			header := w.Header().Get("X-Gofastr-Location")
			if tc.wantHeader {
				if header == "" {
					t.Errorf("X-Gofastr-Location empty for safe URL %q", tc.url)
				}
			} else {
				if header != "" {
					t.Errorf("X-Gofastr-Location should NOT be set for unsafe URL %q (would feed loadPage cross-origin fetch); got %q", tc.url, header)
				}
			}
		})
	}
}

// TestContextOnly_ScreenRendersViaUIHost pins the end-to-end render
// path for a screen that uses component.ContextOnly + RenderCtx: NO
// custom Render() method, NO Render() boilerplate stub. The uihost
// dispatch must detect RenderCtx via structural assertion and route
// to it, NEVER call ContextOnly's stub Render (which returns "").
func TestContextOnly_ScreenRendersViaUIHost(t *testing.T) {
	application := app.NewApp("t")
	application.RegisterScreen(
		app.NewScreen("/", &ctxOnlyScreen{}).WithTitle("Home"),
		nil,
	)
	ds := New(application)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "ctx-only-content") {
		t.Fatalf("RenderCtx output missing from response: %s", truncate(body, 600))
	}
	// ContextOnly.Render returns "". If the dispatch called it instead
	// of RenderCtx, we'd see only the layout chrome and an empty main.
	// A safeguard against future refactors that break the preference.
}

// TestFullPageRedirect_StaysAs303 confirms we did NOT regress the
// full-page redirect path. Browsers DO follow 303 on a top-level
// navigation, so http.Redirect remains correct there.
func TestFullPageRedirect_StaysAs303(t *testing.T) {
	pol := app.PolicyFunc(func(ctx context.Context) app.Decision {
		return decide.Redirect("/login")
	})
	application := app.NewApp("t")
	application.RegisterScreen(
		app.NewScreen("/dash", &testHomeComp{}).WithPolicy(pol),
		nil,
	)
	ds := New(application)

	req := httptest.NewRequest("GET", "/dash", nil)
	// No X-Gofastr-Navigate header, full-page request.
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	if got := w.Code; got != 303 {
		t.Fatalf("full-page redirect: status = %d, want 303", got)
	}
	if got := w.Header().Get("Location"); got != "/login" {
		t.Fatalf("full-page redirect: Location = %q, want /login", got)
	}
}

// The runtime's whole-page fetch (Sec-Fetch-Mode cors or same-origin)
// gets an external target as X-Gofastr-Document. A document load keeps
// the 303, and so does a relative target, which fetch follows on this
// origin.
func TestFullPageFetchExternalRedirect(t *testing.T) {
	for _, c := range []struct {
		name, url, fetch, wantDoc string
		wantStatus                int
	}{
		{"fetch, external", "https://idp.example/x", "same-origin", "https://idp.example/x", 200},
		{"cors fetch, external", "https://idp.example/x", "cors", "https://idp.example/x", 200},
		{"document load, external", "https://idp.example/x", "navigate", "", 303},
		{"no fetch metadata, external", "https://idp.example/x", "", "", 303},
		{"fetch, relative", "/login", "same-origin", "", 303},
		{"fetch, javascript", "javascript:alert(1)", "same-origin", "", 303},
	} {
		t.Run(c.name, func(t *testing.T) {
			pol := app.PolicyFunc(func(ctx context.Context) app.Decision {
				return decide.Redirect(c.url)
			})
			application := app.NewApp("t")
			application.RegisterScreen(app.NewScreen("/dash", &testHomeComp{}).WithPolicy(pol), nil)
			ds := New(application)
			req := httptest.NewRequest("GET", "/dash", nil)
			if c.fetch != "" {
				req.Header.Set("Sec-Fetch-Mode", c.fetch)
			}
			w := httptest.NewRecorder()
			ds.ServeHTTP(w, req)
			if w.Code != c.wantStatus || w.Header().Get("X-Gofastr-Document") != c.wantDoc {
				t.Errorf("status %d, X-Gofastr-Document %q; want %d, %q", w.Code, w.Header().Get("X-Gofastr-Document"), c.wantStatus, c.wantDoc)
			}
			// This arm returns before the page path's own no-store; a
			// shared cache must not replay one reader's redirect.
			if c.wantDoc != "" && w.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", w.Header().Get("Cache-Control"))
			}
		})
	}
}
