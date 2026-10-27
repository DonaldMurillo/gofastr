package uihost

import (
	"context"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// A region guard's Redirect moves the whole page, as a screen policy's
// does: a visitor an outlet guard sends to /login never reaches the page,
// so the page's server actions refuse them too. The action check ran the
// screen chain alone.
func TestServerActionRunsRegionGuards(t *testing.T) {
	ran := 0
	aside := app.NewOutlet("aside", app.OutletOptions{Policy: app.PolicyFunc(func(ctx context.Context) app.Decision {
		if u, ok := handler.GetUser(ctx); ok && u != nil {
			return app.Decision{Kind: app.DecisionAllow}
		}
		return app.Decision{Kind: app.DecisionRedirect, URL: "/login"}
	})})
	shell := app.NewLayout("shell", app.LayoutSpec{Outlets: []*app.Outlet{aside}},
		func(ctx context.Context, l *app.LayoutTree) render.HTML {
			return render.Join(l.Primary(), l.Place(aside))
		})
	a := app.NewApp("action-guard")
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	a.RegisterScreen(app.NewScreen("/secret", &actionTestComp{
		html:    "<p>secret</p>",
		actions: func() { component.On("purge", func(*component.ComponentContext) { ran++ }) },
	}).WithTitle("Secret"), nil)
	a.RegisterScreen(app.NewScreen("/projects/:id", &paramActionComp{actionTestComp{
		html:    "<p>project</p>",
		actions: func() { component.On("archive", func(*component.ComponentContext) { ran++ }) },
	}}).WithTitle("Project"), nil)
	ds := New(a)
	ds.AutoCompileActions()
	post := actionPoster(t, ds)
	user := handler.SetUser(context.Background(), &actionCallerUser{id: "u-1"})

	for _, c := range []struct {
		ctx  context.Context
		body string
		want int
	}{
		{context.Background(), `{"action":"purge","params":{},"componentId":"secret","page":"/secret"}`, http.StatusForbidden},
		{context.Background(), `{"action":"purge","params":{},"componentId":"secret"}`, http.StatusForbidden},
		{context.Background(), `{"action":"archive","params":{},"componentId":"projects-id","page":"/projects/p1"}`, http.StatusForbidden},
		{user, `{"action":"purge","params":{},"componentId":"secret","page":"/secret"}`, http.StatusOK},
		{user, `{"action":"archive","params":{},"componentId":"projects-id","page":"/projects/p1"}`, http.StatusOK},
	} {
		if code := post(c.ctx, c.body); code != c.want {
			t.Errorf("%s: status %d, want %d", c.body, code, c.want)
		}
	}
	if ran != 2 {
		t.Errorf("handlers ran %d times, want 2 (the signed-in calls)", ran)
	}
}
