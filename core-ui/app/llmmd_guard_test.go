package app

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// acctGuardApp mounts every page under a layout whose outlet guard sends
// a visitor away from /acct pages, the way an account area sends an
// anonymous one to /login. The screen chains allow everyone.
func acctGuardApp(t *testing.T) (*App, map[string]*Screen) {
	t.Helper()
	guard := PolicyFunc(func(ctx context.Context) Decision {
		if m, ok := MatchFromContext(ctx); ok && strings.HasPrefix(m.Path(), "/acct") {
			return Decision{Kind: DecisionRedirect, URL: "/login"}
		}
		return Decision{Kind: DecisionAllow}
	})
	aside := NewOutlet("aside", OutletOptions{Policy: guard})
	shell := NewLayout("shell", LayoutSpec{Outlets: []*Outlet{aside}},
		func(ctx context.Context, l *LayoutTree) render.HTML {
			return render.Join(l.Primary(), l.Place(aside))
		})
	a := NewApp("t")
	a.SetDefaultLayout(shell)
	screens := map[string]*Screen{}
	for path, title := range map[string]string{
		"/acct": "Account LEDGER", "/acct/:id": "Invoice LEDGER", "/pub": "Public NEWS",
	} {
		s := NewScreen(path, &basicComp{})
		s.Title, s.Description = title, title+" desc"
		a.RegisterScreen(s, nil)
		screens[path] = s
	}
	if res, err := a.RenderPageResult(context.Background(), "/acct"); err != nil || res.Kind != DecisionRedirect {
		t.Fatalf("premise: /acct renders %+v %v, want the guard's redirect", res, err)
	}
	return a, screens
}

// The page index runs the region guards a page render runs: a page the
// guard moves away keeps its path but not its title or description. A
// dynamic page's guards need params the index has none of, so it is
// withheld too; a page the guard lets through stays listed.
func TestPageIndexRunsRegionGuards(t *testing.T) {
	a, _ := acctGuardApp(t)
	md := AppLLMMDCtx(context.Background(), a)
	if strings.Contains(md, "LEDGER") {
		t.Errorf("index leaked a guarded page's metadata:\n%s", md)
	}
	if !strings.Contains(md, "/acct") || !strings.Contains(md, "Public NEWS") {
		t.Errorf("index lost a path or the public page's title:\n%s", md)
	}
}

// The single-screen handler withholds a page a region guard moves away.
func TestScreenDocRunsRegionGuards(t *testing.T) {
	a, screens := acctGuardApp(t)
	for path, want := range map[string]bool{"/acct": false, "/acct/:id": false, "/pub": true} {
		rec := httptest.NewRecorder()
		ScreenLLMMDHandler(a, screens[path]).ServeHTTP(rec, httptest.NewRequest("GET", path+"/llm.md", nil))
		body := rec.Body.String()
		if full := strings.Contains(body, " desc"); full != want {
			t.Errorf("%s: full doc = %v, want %v:\n%s", path, full, want, body)
		}
	}
}
