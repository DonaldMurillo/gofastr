package uihost

import (
	"context"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// A page whose policy phase fails answers its error, never itself, so its
// actions refuse with the same status: a guard that reads a missing row
// makes /gone a 404, and the action does not run.
func TestServerActionRefusesFailedPage(t *testing.T) {
	ran := 0
	row := app.NewKey[string]("row")
	aside := app.NewOutlet("aside", app.OutletOptions{Policy: app.PolicyFunc(func(ctx context.Context) app.Decision {
		_, _ = row.Get(ctx)
		return app.Decision{Kind: app.DecisionAllow}
	})})
	shell := app.NewLayout("shell", app.LayoutSpec{Outlets: []*app.Outlet{aside}},
		func(ctx context.Context, l *app.LayoutTree) render.HTML {
			return render.Join(l.Primary(), l.Place(aside))
		})
	a := app.NewApp("action-failed")
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	a.RegisterScreen(app.NewScreen("/gone", &actionTestComp{
		html:    "<p>gone</p>",
		actions: func() { component.On("purge", func(*component.ComponentContext) { ran++ }) },
	}).WithTitle("Gone").
		Resolve(row.From(func(context.Context) (string, error) { return "", app.ErrNotFound })), nil)
	ds := New(a)
	ds.AutoCompileActions()
	post := actionPoster(t, ds)

	if code := post(context.Background(), `{"action":"purge","params":{},"componentId":"gone","page":"/gone"}`); code != http.StatusNotFound {
		t.Errorf("named page: status %d, want 404", code)
	}
	if code := post(context.Background(), `{"action":"purge","params":{},"componentId":"gone"}`); code != http.StatusForbidden {
		t.Errorf("no page: status %d, want 403", code)
	}
	if ran != 0 {
		t.Errorf("handler ran %d times, want 0", ran)
	}
}
