package uihost

import (
	"context"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
)

// A dynamic screen whose access comes from a Requires resolver (the row
// for :id, ErrNotFound when the caller may not see it) gates its server
// actions the way it gates its page. An action that named no page ran
// the pattern-level check, which skipped the eager resolvers, so any
// caller with a self-minted session ran the handler.
func TestServerActionRunsRequiresResolvers(t *testing.T) {
	ran := 0
	project := app.NewKey[string]("project")
	g := app.NewScreenGroup("/projects", nil)
	g.Resolve(project.From(func(ctx context.Context) (string, error) {
		if m, ok := app.MatchFromContext(ctx); ok && m.Param("id") == "p1" {
			return "p1", nil
		}
		return "", app.ErrNotFound
	}))
	g.Requires(project)
	g.Screen(app.NewScreen("/projects/:id", &paramActionComp{actionTestComp{
		html:    "<p>project</p>",
		actions: func() { component.On("archive", func(*component.ComponentContext) { ran++ }) },
	}}).WithTitle("Project"), nil)

	a := app.NewApp("action-requires")
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	a.Router.ScreenGroup(g)
	ds := New(a)
	ds.AutoCompileActions()
	post := actionPoster(t, ds)

	for _, c := range []struct {
		body string
		want int
	}{
		{`{"action":"archive","params":{},"componentId":"projects-id","page":"/projects/p1"}`, http.StatusOK},
		{`{"action":"archive","params":{},"componentId":"projects-id","page":"/projects/p2"}`, http.StatusNotFound},
		{`{"action":"archive","params":{},"componentId":"projects-id"}`, http.StatusForbidden},
		{`{"action":"archive","params":{},"componentId":"projects-id","page":"/"}`, http.StatusForbidden},
	} {
		if code := post(context.Background(), c.body); code != c.want {
			t.Errorf("%s: status %d, want %d", c.body, code, c.want)
		}
	}
	if ran != 1 {
		t.Errorf("handler ran %d times, want 1 (the allowed page)", ran)
	}
}
