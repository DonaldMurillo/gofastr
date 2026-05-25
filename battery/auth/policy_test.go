package auth_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type stubComp struct{ tag string }

func (s *stubComp) Render() render.HTML { return render.HTML("<p>" + s.tag + "</p>") }

func TestSessionFrom_ReturnsLoadedUser(t *testing.T) {
	u := &auth.BasicUser{ID: "u1", Email: "a@b", Roles: []string{"user"}}
	ctx := handler.SetUser(context.Background(), u)
	got, ok := auth.SessionFrom(ctx)
	if !ok || got == nil || got.GetID() != "u1" {
		t.Fatalf("want u1, got %v ok=%v", got, ok)
	}
}

func TestSessionFrom_AnonymousReturnsFalse(t *testing.T) {
	_, ok := auth.SessionFrom(context.Background())
	if ok {
		t.Fatalf("anon ctx should return ok=false")
	}
}

func TestSessionPolicy_RedirectsAnonWithNextPath(t *testing.T) {
	a := app.NewApp("t")
	a.RegisterScreen(
		app.NewScreen("/dash", &stubComp{tag: "dash"}).WithPolicy(auth.SessionPolicy()),
		nil,
	)
	r := httptest.NewRequest("GET", "/dash?x=1", nil)
	ctx := app.WithRequest(r.Context(), r)

	res, err := a.RenderPageResult(ctx, "/dash")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Kind != app.DecisionRedirect {
		t.Fatalf("want Redirect, got %v", res.Kind)
	}
	if !strings.HasPrefix(res.URL, "/login?next=") {
		t.Fatalf("want next-path appended, got %q", res.URL)
	}
	if !strings.Contains(res.URL, "%2Fdash%3Fx%3D1") {
		t.Fatalf("next= should encode full path+query, got %q", res.URL)
	}
}

func TestSessionPolicy_AllowsAuthed(t *testing.T) {
	a := app.NewApp("t")
	a.RegisterScreen(
		app.NewScreen("/dash", &stubComp{tag: "dash"}).WithPolicy(auth.SessionPolicy()),
		nil,
	)
	r := httptest.NewRequest("GET", "/dash", nil)
	ctx := handler.SetUser(r.Context(), &auth.BasicUser{ID: "u1"})
	ctx = app.WithRequest(ctx, r)

	res, err := a.RenderPageResult(ctx, "/dash")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Kind != app.DecisionAllow {
		t.Fatalf("authed want Allow, got %v", res.Kind)
	}
	if !strings.Contains(string(res.HTML), "<p>dash</p>") {
		t.Fatalf("page body missing: %s", res.HTML)
	}
}

func TestSessionPolicy_WithRenderAltForAnon(t *testing.T) {
	a := app.NewApp("t")
	a.RegisterScreen(
		app.NewScreen("/", &stubComp{tag: "dash"}).
			WithPolicy(auth.SessionPolicy(auth.WithRenderAlt(
				func() component.Component { return &stubComp{tag: "landing"} },
			))),
		nil,
	)
	r := httptest.NewRequest("GET", "/", nil)
	ctx := app.WithRequest(r.Context(), r)

	res, err := a.RenderPageResult(ctx, "/")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Kind != app.DecisionRenderAlt {
		t.Fatalf("anon want RenderAlt, got %v", res.Kind)
	}
	if !strings.Contains(string(res.HTML), "<p>landing</p>") {
		t.Fatalf("alt body missing: %s", res.HTML)
	}
}

func TestRolePolicy_BlocksMissingRole(t *testing.T) {
	a := app.NewApp("t")
	a.RegisterScreen(
		app.NewScreen("/admin", &stubComp{tag: "x"}).
			WithPolicy(auth.RolePolicy([]string{"admin"})),
		nil,
	)
	r := httptest.NewRequest("GET", "/admin", nil)
	ctx := handler.SetUser(r.Context(), &auth.BasicUser{ID: "u1", Roles: []string{"user"}})
	ctx = app.WithRequest(ctx, r)

	res, err := a.RenderPageResult(ctx, "/admin")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Kind != app.DecisionBlock || res.Status != 403 {
		t.Fatalf("want Block 403, got %v %d", res.Kind, res.Status)
	}
}

func TestRolePolicy_AllowsMatchingRole(t *testing.T) {
	a := app.NewApp("t")
	a.RegisterScreen(
		app.NewScreen("/admin", &stubComp{tag: "x"}).
			WithPolicy(auth.RolePolicy([]string{"admin", "owner"})),
		nil,
	)
	r := httptest.NewRequest("GET", "/admin", nil)
	ctx := handler.SetUser(r.Context(), &auth.BasicUser{ID: "u1", Roles: []string{"admin"}})
	ctx = app.WithRequest(ctx, r)

	res, err := a.RenderPageResult(ctx, "/admin")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Kind != app.DecisionAllow {
		t.Fatalf("admin want Allow, got %v", res.Kind)
	}
}

var _ component.Component = (*stubComp)(nil)

// TestSessionPolicy_NextPathEdgeCases pins the encoding + skip rules
// for the ?next= append on the default Redirect outcome. Specifically:
//   - root path "/" is intentionally NOT appended (login bouncing back
//     to "/" is the default behavior; ?next=/ is noise)
//   - paths with query strings get the whole path+query encoded
//   - special characters are url-escaped
//   - when no app.WithRequest ctx is attached, no ?next= is appended
//   - a redirect URL that already has a ?param keeps it and uses & for next=
func TestSessionPolicy_NextPathEdgeCases(t *testing.T) {
	cases := []struct {
		name         string
		requestPath  string // includes ?query if any
		withRequest  bool   // whether to attach app.WithRequest
		policyURL    string // url passed to WithRedirect
		wantPrefix   string
		wantContains string // empty = no specific substring check
		wantNoNext   bool
	}{
		{
			name:        "root path skips next",
			requestPath: "/",
			withRequest: true,
			policyURL:   "/login",
			wantPrefix:  "/login",
			wantNoNext:  true,
		},
		{
			name:        "no request context skips next",
			requestPath: "/dash",
			withRequest: false,
			policyURL:   "/login",
			wantPrefix:  "/login",
			wantNoNext:  true,
		},
		{
			name:         "plain path appended",
			requestPath:  "/dash",
			withRequest:  true,
			policyURL:    "/login",
			wantPrefix:   "/login?next=",
			wantContains: "%2Fdash",
		},
		{
			name:         "path with query encoded whole",
			requestPath:  "/dash?sort=asc",
			withRequest:  true,
			policyURL:    "/login",
			wantPrefix:   "/login?next=",
			wantContains: "%2Fdash%3Fsort%3Dasc",
		},
		{
			name:         "policy URL with existing query uses & separator",
			requestPath:  "/dash",
			withRequest:  true,
			policyURL:    "/login?x=1",
			wantPrefix:   "/login?x=1&next=",
			wantContains: "%2Fdash",
		},
		{
			// httptest.NewRequest decodes %2F → /, so the next= value
			// contains the canonical decoded form re-encoded once.
			name:         "encoded slash in path normalises through",
			requestPath:  "/foo%2Fbar",
			withRequest:  true,
			policyURL:    "/login",
			wantPrefix:   "/login?next=",
			wantContains: "%2Ffoo%2Fbar",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := app.NewApp("t")
			a.RegisterScreen(
				app.NewScreen(tc.requestPath, &stubComp{tag: "x"}).
					WithPolicy(auth.SessionPolicy(auth.WithRedirect(tc.policyURL))),
				nil,
			)
			ctx := context.Background()
			if tc.withRequest {
				r := httptest.NewRequest("GET", tc.requestPath, nil)
				ctx = app.WithRequest(r.Context(), r)
			}
			res, err := a.RenderPageResult(ctx, tc.requestPath)
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if res.Kind != app.DecisionRedirect {
				t.Fatalf("want Redirect, got %v", res.Kind)
			}
			if !strings.HasPrefix(res.URL, tc.wantPrefix) {
				t.Fatalf("URL = %q, want prefix %q", res.URL, tc.wantPrefix)
			}
			if tc.wantNoNext && strings.Contains(res.URL, "next=") {
				t.Fatalf("URL = %q should NOT contain next=", res.URL)
			}
			if tc.wantContains != "" && !strings.Contains(res.URL, tc.wantContains) {
				t.Fatalf("URL = %q, want to contain %q", res.URL, tc.wantContains)
			}
			// Safety: no smuggling vectors slip through QueryEscape.
			if strings.ContainsAny(res.URL, "\r\n\t\x00") {
				t.Fatalf("URL contains control char: %q", res.URL)
			}
		})
	}
}

// TestRolePolicy_AnonHitsBlockDefault pins the documented default:
// RolePolicy on an anonymous request blocks with 403 (not redirect),
// because the user can't satisfy a role check by visiting /login.
// Sessionless requests fail the SessionFrom precondition and fall into
// the configured failure outcome — default Block(403).
func TestRolePolicy_AnonHitsBlockDefault(t *testing.T) {
	a := app.NewApp("t")
	a.RegisterScreen(
		app.NewScreen("/admin", &stubComp{tag: "x"}).
			WithPolicy(auth.RolePolicy(auth.Roles("admin"))),
		nil,
	)
	res, err := a.RenderPageResult(context.Background(), "/admin")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Kind != app.DecisionBlock || res.Status != 403 {
		t.Fatalf("anon should default to Block(403), got %v %d", res.Kind, res.Status)
	}
}

// TestRolePolicy_AnonRedirectWhenOptedIn pins that the operator can
// override the default block by passing WithRedirect — anon hits a
// role-gated route, gets redirected to login (with next= chain).
func TestRolePolicy_AnonRedirectWhenOptedIn(t *testing.T) {
	a := app.NewApp("t")
	a.RegisterScreen(
		app.NewScreen("/admin", &stubComp{tag: "x"}).
			WithPolicy(auth.RolePolicy(auth.Roles("admin"), auth.WithRedirect("/login"))),
		nil,
	)
	r := httptest.NewRequest("GET", "/admin", nil)
	ctx := app.WithRequest(r.Context(), r)
	res, err := a.RenderPageResult(ctx, "/admin")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Kind != app.DecisionRedirect {
		t.Fatalf("want Redirect, got %v", res.Kind)
	}
	if !strings.HasPrefix(res.URL, "/login?next=") {
		t.Fatalf("want /login?next=... got %q", res.URL)
	}
}

// TestSessionPolicy_NoNextSuppressesAppend pins the NoNext() option.
func TestSessionPolicy_NoNextSuppressesAppend(t *testing.T) {
	a := app.NewApp("t")
	a.RegisterScreen(
		app.NewScreen("/dash", &stubComp{tag: "x"}).
			WithPolicy(auth.SessionPolicy(auth.WithRedirect("/marketing", auth.NoNext()))),
		nil,
	)
	r := httptest.NewRequest("GET", "/dash", nil)
	ctx := app.WithRequest(r.Context(), r)
	res, err := a.RenderPageResult(ctx, "/dash")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.URL != "/marketing" {
		t.Fatalf("URL = %q, want exactly /marketing (no ?next=)", res.URL)
	}
}
