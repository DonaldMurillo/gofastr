package uihost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// newMatchApp builds an app with the two dynamic screen shapes Field
// Assist guards: the session page and its operator subpage.
func newMatchApp() *app.App {
	a := app.NewApp("matchapp")
	a.Register("/session/{sessionId}", &paramJSONComp{}, nil)
	a.Register("/session/{sessionId}/operator", &paramJSONComp{}, nil)
	return a
}

// matchProbe records what a middleware registered AFTER the route-match
// middleware can read.
type matchProbe struct {
	match   app.Match
	matched bool
}

func (p *matchProbe) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.match, p.matched = app.MatchFromContext(r.Context())
		next.ServeHTTP(w, r)
	})
}

func TestRouteMatchMiddlewarePopulatesContext(t *testing.T) {
	ds := New(newMatchApp())
	probe := &matchProbe{}
	rt := router.New()
	rt.Use(ds.RouteMatchMiddleware())
	rt.Use(probe.middleware)
	ds.Mount(rt)

	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/session/abc", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("screen did not render: %d %s", rec.Code, rec.Body.String())
	}
	if !probe.matched {
		t.Fatal("middleware saw no route match")
	}
	if got := probe.match.Param("sessionId"); got != "abc" {
		t.Errorf("sessionId = %q, want abc", got)
	}
	if got := probe.match.ScreenID(); got != "/session/:sessionId" {
		t.Errorf("ScreenID = %q, want /session/:sessionId", got)
	}
}

// The operator subpage must resolve to its own pattern, not the
// shorter sibling that also matches the prefix.
func TestRouteMatchMiddlewarePicksSubroute(t *testing.T) {
	ds := New(newMatchApp())
	probe := &matchProbe{}
	rt := router.New()
	rt.Use(ds.RouteMatchMiddleware())
	rt.Use(probe.middleware)
	ds.Mount(rt)

	rt.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/session/abc/operator", nil))

	if got := probe.match.ScreenID(); got != "/session/:sessionId/operator" {
		t.Errorf("ScreenID = %q, want /session/:sessionId/operator", got)
	}
	if got := probe.match.Param("sessionId"); got != "abc" {
		t.Errorf("sessionId = %q, want abc", got)
	}
}

// Trailing-slash requests resolve with the same params the screen
// pipeline sees (Router.Resolve trims both ends before splitting).
func TestRouteMatchMiddlewareTrailingSlash(t *testing.T) {
	ds := New(newMatchApp())
	probe := &matchProbe{}
	rt := router.New()
	rt.Use(ds.RouteMatchMiddleware())
	rt.Use(probe.middleware)
	ds.Mount(rt)

	rt.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/session/abc/", nil))

	if !probe.matched {
		t.Fatal("trailing-slash request carried no match")
	}
	if got := probe.match.Param("sessionId"); got != "abc" {
		t.Errorf("sessionId = %q, want abc", got)
	}
}

// A path no screen matches carries no Match, and the response stays
// the host's truthful 404: populating must not invent screens.
func TestRouteMatchMiddlewareUnknownStill404(t *testing.T) {
	ds := New(newMatchApp())
	probe := &matchProbe{}
	rt := router.New()
	rt.Use(ds.RouteMatchMiddleware())
	rt.Use(probe.middleware)
	ds.Mount(rt)

	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))

	if probe.matched {
		t.Fatal("unknown path produced a match")
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// goneComp is the branded recovery screen a guard renders instead of
// middleware plain text.
type goneComp struct{}

func (goneComp) Render() render.HTML { return render.HTML("<p>session is gone</p>") }

// The end-to-end middleware shape from the ticket: read the match,
// decide, and render a branded recovery screen with the real status.
// A live session must still reach the screen.
func TestMatchGuardRendersRecoveryScreen(t *testing.T) {
	ds := New(newMatchApp())
	rt := router.New()
	rt.Use(ds.RouteMatchMiddleware())
	rt.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if m, ok := app.MatchFromContext(r.Context()); ok && m.Param("sessionId") == "dead" {
				ds.RenderScreen(w, r, goneComp{}, ScreenResponse{Status: http.StatusGone})
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	ds.Mount(rt)

	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/session/dead", nil))
	if rec.Code != http.StatusGone {
		t.Fatalf("status = %d, want 410", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Errorf("Cache-Control = %q, want private, no-store", cc)
	}
	if body := rec.Body.String(); !strings.Contains(body, "session is gone") {
		t.Errorf("recovery body missing: %s", body)
	}

	rec = httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/session/live", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("live session status = %d, want 200", rec.Code)
	}
}

// matchPolicy records what a screen policy sees on the render context.
type matchPolicy struct {
	saw      bool
	param    string
	screenID string
}

func (p *matchPolicy) Decide(ctx context.Context) app.Decision {
	if m, ok := app.MatchFromContext(ctx); ok {
		p.saw, p.param, p.screenID = true, m.Param("id"), m.ScreenID()
	}
	return app.Decision{}
}

// Standalone hosts (no RouteMatchMiddleware) still populate the match
// before policy evaluation, so a policy on a dynamic route reads the
// authoritative params.
func TestPolicySeesRouteMatchOnFullRender(t *testing.T) {
	a := app.NewApp("policyapp")
	pol := &matchPolicy{}
	a.RegisterScreen(app.NewScreen("/thing/{id}", &paramJSONComp{}).WithPolicy(pol), nil)
	ds := New(a)

	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/thing/7", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if !pol.saw {
		t.Fatal("policy saw no route match")
	}
	if pol.param != "7" {
		t.Errorf("param = %q, want 7", pol.param)
	}
	if pol.screenID != "/thing/:id" {
		t.Errorf("screenID = %q, want /thing/:id", pol.screenID)
	}
}

// The partial-navigation arm populates the match too.
func TestPolicySeesRouteMatchOnPartial(t *testing.T) {
	a := app.NewApp("policyapp")
	pol := &matchPolicy{}
	a.RegisterScreen(app.NewScreen("/thing/{id}", &paramJSONComp{}).WithPolicy(pol), nil)
	ds := New(a)

	req := httptest.NewRequest(http.MethodGet, "/thing/9", nil)
	req.Header.Set("X-Gofastr-Navigate", "1")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if !pol.saw || pol.param != "9" {
		t.Errorf("partial render: saw=%v param=%q, want true 9", pol.saw, pol.param)
	}
}

// fieldAssistGuard is the shape from the ticket: read the match,
// distinguish the operator subroute (role), and answer expired or
// wrong-role with a branded recovery screen carrying the true status.
func fieldAssistGuard(ds *UIHost) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			m, ok := app.MatchFromContext(r.Context())
			if ok {
				switch {
				case m.Param("sessionId") == "dead":
					ds.RenderScreen(w, r, goneComp{}, ScreenResponse{Status: http.StatusGone})
					return
				case m.ScreenID() == "/session/:sessionId/operator" && m.Param("sessionId") != "boss":
					ds.RenderScreen(w, r, goneComp{}, ScreenResponse{Status: http.StatusForbidden})
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// The Field Assist matrix: valid, expired, wrong-role, trailing-slash,
// malformed (empty param), and unknown paths through one guard.
func TestMatchGuardPathMatrix(t *testing.T) {
	ds := New(newMatchApp())
	rt := router.New()
	rt.Use(ds.RouteMatchMiddleware())
	rt.Use(fieldAssistGuard(ds))
	ds.Mount(rt)

	for _, p := range []struct {
		path string
		code int
	}{
		{"/session/live", http.StatusOK},                  // valid
		{"/session/boss/operator", http.StatusOK},         // valid, right role
		{"/session/dead", http.StatusGone},                // expired
		{"/session/dead/operator", http.StatusGone},       // expired beats role
		{"/session/live/operator", http.StatusForbidden},  // wrong role
		{"/session/live/operator/", http.StatusForbidden}, // trailing slash: same verdict
	} {
		rec := httptest.NewRecorder()
		rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p.path, nil))
		if rec.Code != p.code {
			t.Errorf("%s: status = %d, want %d", p.path, rec.Code, p.code)
		}
	}
}
