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

	mint := httptest.NewRecorder()
	ds.ServeHTTP(mint, httptest.NewRequest(http.MethodPost, "/__gofastr/session", nil))
	cookies := mint.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("session mint set no cookie")
	}
	post := func(ctx context.Context) int {
		body := `{"action":"purge","params":{},"componentId":"secret"}`
		req := httptest.NewRequest(http.MethodPost, "/__gofastr/action", strings.NewReader(body)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		ds.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := post(context.Background()); code != http.StatusForbidden || ran != 0 {
		t.Fatalf("anonymous action on a policy-blocked screen: status %d, handler ran %d times; want 403, 0", code, ran)
	}
	if code := post(handler.SetUser(context.Background(), &actionCallerUser{id: "u-1"})); code != http.StatusOK || ran != 1 {
		t.Fatalf("allowed caller: status %d, handler ran %d times; want 200, 1", code, ran)
	}
}
