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
// X-Gofastr-Location — that header is read by the runtime and fed
// directly into loadPage(), which would issue a cross-origin fetch
// with credentials.
//
// Behaviour: unsafe URLs fall through to a hard 303 redirect (which
// the browser handles safely — cross-origin redirects don't propagate
// cookies and the user sees the URL change). Safe relative paths use
// the SPA-nav header path.
func TestPartialRedirect_RejectsUnsafeURLs(t *testing.T) {
	cases := []struct {
		name        string
		url         string
		wantHeader  bool   // expect X-Gofastr-Location set
		wantStatus  int    // expect this HTTP status
		description string
	}{
		{"safe relative", "/login", true, 200, "normal SPA-nav path"},
		{"safe relative with query", "/login?next=/x", true, 200, "query is fine"},
		{"protocol-relative", "//evil.com/x", false, 303, "browser-safe hard redirect"},
		{"absolute https", "https://evil.com/x", false, 303, "browser-safe hard redirect"},
		{"absolute http", "http://evil.com/x", false, 303, "browser-safe hard redirect"},
		{"javascript scheme", "javascript:alert(1)", false, 303, "browser blocks the scheme"},
		{"data scheme", "data:text/html,<script>", false, 303, "browser blocks the scheme"},
		{"backslash bypass", "/\\evil.com", false, 303, "browsers normalise \\ to / → //evil"},
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
	// No X-Gofastr-Navigate header — full-page request.
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)

	if got := w.Code; got != 303 {
		t.Fatalf("full-page redirect: status = %d, want 303", got)
	}
	if got := w.Header().Get("Location"); got != "/login" {
		t.Fatalf("full-page redirect: Location = %q, want /login", got)
	}
}
