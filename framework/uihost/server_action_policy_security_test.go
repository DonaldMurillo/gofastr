package uihost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/handler"
)

// actionPoster mints a uihost session and returns a func that POSTs body to
// /__gofastr/action with it, answering the status.
func actionPoster(t *testing.T, ds *UIHost) func(ctx context.Context, body string) int {
	t.Helper()
	mint := httptest.NewRecorder()
	ds.ServeHTTP(mint, httptest.NewRequest(http.MethodPost, "/__gofastr/session", nil))
	cookies := mint.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("session mint set no cookie")
	}
	return func(ctx context.Context, body string) int {
		req := httptest.NewRequest(http.MethodPost, "/__gofastr/action", strings.NewReader(body)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		ds.ServeHTTP(rec, req)
		return rec.Code
	}
}

// A screen's Policy chain gates its page render, and the screen's server
// actions are compiled from the same component. POST /__gofastr/action
// needed only a self-minted session, so a visitor the policy blocks from
// the page could still run every action on it.
func TestServerActionRunsScreenPolicy(t *testing.T) {
	ran := 0
	comp := &actionTestComp{
		html: "<p>secret</p>",
		actions: func() {
			component.On("purge", func(*component.ComponentContext) { ran++ })
		},
	}
	signedIn := app.PolicyFunc(func(ctx context.Context) app.Decision {
		if u, ok := handler.GetUser(ctx); ok && u != nil {
			return app.Decision{Kind: app.DecisionAllow}
		}
		return app.Decision{Kind: app.DecisionBlock, Status: http.StatusForbidden}
	})

	a := app.NewApp("action-policy")
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	a.RegisterScreen(app.NewScreen("/secret", comp).WithTitle("Secret").WithPolicy(signedIn), nil)
	ds := New(a)
	ds.AutoCompileActions()
	post := actionPoster(t, ds)
	const body = `{"action":"purge","params":{},"componentId":"secret"}`

	if code := post(context.Background(), body); code != http.StatusForbidden || ran != 0 {
		t.Fatalf("anonymous action on a policy-blocked screen: status %d, handler ran %d times; want 403, 0", code, ran)
	}
	if code := post(handler.SetUser(context.Background(), &actionCallerUser{id: "u-1"}), body); code != http.StatusOK || ran != 1 {
		t.Fatalf("allowed caller: status %d, handler ran %d times; want 200, 1", code, ran)
	}
}

// paramActionComp is actionTestComp on a dynamic route.
type paramActionComp struct{ actionTestComp }

func (*paramActionComp) SetParams(map[string]string) {}

// A policy on a dynamic route reads its params. The runtime sends the page
// path with each action; the policy sees that page's params when the path
// resolves to the action's own screen, and none otherwise.
func TestServerActionPolicySeesRouteParams(t *testing.T) {
	ran := 0
	comp := &paramActionComp{actionTestComp{
		html: "<p>project</p>",
		actions: func() {
			component.On("archive", func(*component.ComponentContext) { ran++ })
		},
	}}
	memberOfP1 := app.PolicyFunc(func(ctx context.Context) app.Decision {
		if m, ok := app.MatchFromContext(ctx); ok && m.Param("id") == "p1" {
			return app.Decision{Kind: app.DecisionAllow}
		}
		return app.Decision{Kind: app.DecisionBlock, Status: http.StatusForbidden}
	})

	a := app.NewApp("action-params")
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	a.RegisterScreen(app.NewScreen("/other/:id", plainComp{}).WithTitle("Other"), nil)
	a.RegisterScreen(app.NewScreen("/projects/:id", comp).WithTitle("Project").WithPolicy(memberOfP1), nil)
	ds := New(a)
	ds.AutoCompileActions()
	post := actionPoster(t, ds)
	send := func(page string) int {
		return post(context.Background(), `{"action":"archive","params":{},"componentId":"projects-id","page":"`+page+`"}`)
	}

	if code := send("/projects/p1"); code != http.StatusOK || ran != 1 {
		t.Fatalf("action from an allowed page: status %d, ran %d; want 200, 1", code, ran)
	}
	for _, page := range []string{"/projects/p2", "", "/other/p1", "/nowhere/p1"} {
		if code := send(page); code != http.StatusForbidden {
			t.Errorf("action with page %q: status %d, want 403", page, code)
		}
	}
	if ran != 1 {
		t.Fatalf("handler ran %d times, want 1", ran)
	}
}
