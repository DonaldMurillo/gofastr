package app_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type homeComp struct{}

func (homeComp) Render() render.HTML { return render.HTML("<p>home</p>") }

type altComp struct{}

func (altComp) Render() render.HTML { return render.HTML("<p>anon-landing</p>") }

type authedKey struct{}

func authedPolicy() app.Policy {
	return app.PolicyFunc(func(ctx context.Context) app.Decision {
		if v, _ := ctx.Value(authedKey{}).(bool); v {
			return decide.Allow()
		}
		return decide.Redirect("/login")
	})
}

func altPolicy() app.Policy {
	return app.PolicyFunc(func(ctx context.Context) app.Decision {
		if v, _ := ctx.Value(authedKey{}).(bool); v {
			return decide.Allow()
		}
		return decide.RenderAlt(func() component.Component { return &altComp{} })
	})
}

func blockPolicy() app.Policy {
	return app.PolicyFunc(func(ctx context.Context) app.Decision {
		return decide.Block(403, "no")
	})
}

func TestRenderPageResult_AllowsByDefault(t *testing.T) {
	a := app.NewApp("t")
	a.Register("/", &homeComp{}, nil)

	res, err := a.RenderPageResult(context.Background(), "/")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Kind != app.DecisionAllow {
		t.Fatalf("want Allow, got %v", res.Kind)
	}
	if !strings.Contains(string(res.HTML), "<p>home</p>") {
		t.Fatalf("page missing home body: %s", res.HTML)
	}
}

func TestRenderPageResult_RedirectsOnPolicyFail(t *testing.T) {
	a := app.NewApp("t")
	a.RegisterScreen(app.NewScreen("/dash", &homeComp{}).WithPolicy(authedPolicy()), nil)

	res, err := a.RenderPageResult(context.Background(), "/dash")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Kind != app.DecisionRedirect || res.URL != "/login" {
		t.Fatalf("want Redirect /login, got %v %q", res.Kind, res.URL)
	}
}

func TestRenderPageResult_RendersAltForAnon(t *testing.T) {
	a := app.NewApp("t")
	a.RegisterScreen(app.NewScreen("/", &homeComp{}).WithPolicy(altPolicy()), nil)

	res, err := a.RenderPageResult(context.Background(), "/")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Kind != app.DecisionRenderAlt {
		t.Fatalf("want RenderAlt, got %v", res.Kind)
	}
	if !strings.Contains(string(res.HTML), "anon-landing") {
		t.Fatalf("alt body missing: %s", res.HTML)
	}
	if strings.Contains(string(res.HTML), "<p>home</p>") {
		t.Fatalf("home body should not render when alt wins: %s", res.HTML)
	}

	ctx := context.WithValue(context.Background(), authedKey{}, true)
	res, err = a.RenderPageResult(ctx, "/")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Kind != app.DecisionAllow {
		t.Fatalf("authed should allow, got %v", res.Kind)
	}
	if !strings.Contains(string(res.HTML), "<p>home</p>") {
		t.Fatalf("authed should render home: %s", res.HTML)
	}
}

func TestRenderPageResult_BlocksWithStatus(t *testing.T) {
	a := app.NewApp("t")
	a.RegisterScreen(app.NewScreen("/admin", &homeComp{}).WithPolicy(blockPolicy()), nil)

	res, err := a.RenderPageResult(context.Background(), "/admin")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Kind != app.DecisionBlock || res.Status != 403 {
		t.Fatalf("want Block 403, got %v %d", res.Kind, res.Status)
	}
}

func TestPolicy_GroupInheritsThroughScreen(t *testing.T) {
	a := app.NewApp("t")
	g := app.NewScreenGroup("/dash", nil, authedPolicy())
	g.Screen(app.NewScreen("home", &homeComp{}), nil)
	a.Router.ScreenGroup(g)

	res, err := a.RenderPageResult(context.Background(), "/dash/home")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Kind != app.DecisionRedirect {
		t.Fatalf("want Redirect from group policy, got %v", res.Kind)
	}

	ctx := context.WithValue(context.Background(), authedKey{}, true)
	res, err = a.RenderPageResult(ctx, "/dash/home")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Kind != app.DecisionAllow {
		t.Fatalf("authed should allow group screen, got %v", res.Kind)
	}
}

func TestPolicy_SubGroupAddsToParentChain(t *testing.T) {
	a := app.NewApp("t")
	g := app.NewScreenGroup("/dash", nil, authedPolicy())
	admin := g.SubGroup("admin", nil, blockPolicy())
	admin.Screen(app.NewScreen("panel", &homeComp{}), nil)
	a.Router.ScreenGroup(g)

	// Anon: parent policy (Redirect) wins first
	res, err := a.RenderPageResult(context.Background(), "/dash/admin/panel")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Kind != app.DecisionRedirect {
		t.Fatalf("want Redirect from parent, got %v", res.Kind)
	}

	// Authed: parent passes, child blocks
	ctx := context.WithValue(context.Background(), authedKey{}, true)
	res, err = a.RenderPageResult(ctx, "/dash/admin/panel")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.Kind != app.DecisionBlock || res.Status != 403 {
		t.Fatalf("want Block 403 from child, got %v %d", res.Kind, res.Status)
	}
}

// TestRenderAlt_FactoryPerRequest pins the no-singleton-mutation
// guarantee: two parallel renders of the SAME policy that returns
// RenderAlt must produce two DISTINCT alt instances. Otherwise a
// shared alt sees concurrent SetParams / Inject / Load and leaks
// data between users (the bug the original singleton design had).
func TestRenderAlt_FactoryPerRequest(t *testing.T) {
	var (
		altsMu sync.Mutex
		alts   []*recordingComp
		next   int
	)
	factory := func() component.Component {
		altsMu.Lock()
		next++
		c := &recordingComp{id: next}
		alts = append(alts, c)
		altsMu.Unlock()
		return c
	}
	pol := app.PolicyFunc(func(ctx context.Context) app.Decision {
		return decide.RenderAlt(factory)
	})

	a := app.NewApp("t")
	a.RegisterScreen(app.NewScreen("/", &homeComp{}).WithPolicy(pol), nil)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := a.RenderPageResult(context.Background(), "/")
			if err != nil {
				t.Errorf("err: %v", err)
				return
			}
			if res.Kind != app.DecisionRenderAlt {
				t.Errorf("want RenderAlt, got %v", res.Kind)
			}
		}()
	}
	wg.Wait()

	altsMu.Lock()
	defer altsMu.Unlock()
	if len(alts) != 8 {
		t.Fatalf("expected 8 alt instances (one per request), got %d", len(alts))
	}
	seen := map[*recordingComp]bool{}
	for _, c := range alts {
		if seen[c] {
			t.Fatalf("alt instance %p reused — RenderAlt must produce fresh component per request", c)
		}
		seen[c] = true
	}
}

// recordingComp carries a non-zero-size field so that &recordingComp{}
// produces a unique heap address per call (zero-sized structs can share
// addresses under Go's runtime).
type recordingComp struct{ id int }

func (recordingComp) Render() render.HTML { return render.HTML("<p>alt</p>") }

// TestRenderAlt_FactoryConcurrentAcrossScreens is the harder
// concurrency test: N distinct gated screens share a single alt
// factory, so parallel goroutines hit DIFFERENT screens (each with
// its own mu) and the factory contract is exercised under genuine
// parallelism — not the per-screen serialization that
// TestRenderAlt_FactoryPerRequest had.
func TestRenderAlt_FactoryConcurrentAcrossScreens(t *testing.T) {
	const numScreens = 8
	const requestsPerScreen = 4

	var (
		altsMu sync.Mutex
		alts   []*recordingComp
		next   int
	)
	factory := func() component.Component {
		altsMu.Lock()
		next++
		c := &recordingComp{id: next}
		alts = append(alts, c)
		altsMu.Unlock()
		return c
	}
	pol := app.PolicyFunc(func(ctx context.Context) app.Decision {
		return decide.RenderAlt(factory)
	})

	a := app.NewApp("t")
	for i := 0; i < numScreens; i++ {
		path := "/s" + string(rune('0'+i))
		a.RegisterScreen(app.NewScreen(path, &homeComp{}).WithPolicy(pol), nil)
	}

	var wg sync.WaitGroup
	for i := 0; i < numScreens; i++ {
		path := "/s" + string(rune('0'+i))
		for j := 0; j < requestsPerScreen; j++ {
			wg.Add(1)
			go func(p string) {
				defer wg.Done()
				res, err := a.RenderPageResult(context.Background(), p)
				if err != nil {
					t.Errorf("err %s: %v", p, err)
					return
				}
				if res.Kind != app.DecisionRenderAlt {
					t.Errorf("path %s: want RenderAlt, got %v", p, res.Kind)
				}
			}(path)
		}
	}
	wg.Wait()

	altsMu.Lock()
	defer altsMu.Unlock()
	want := numScreens * requestsPerScreen
	if len(alts) != want {
		t.Fatalf("expected %d distinct alt instances (one per request, across %d screens), got %d", want, numScreens, len(alts))
	}
	seen := make(map[*recordingComp]bool, want)
	for _, c := range alts {
		if seen[c] {
			t.Fatalf("alt instance %p reused — factory must produce a fresh component per request, even across screens", c)
		}
		seen[c] = true
	}
}

func TestRenderPage_LegacyShape_ErrorsOnRedirect(t *testing.T) {
	a := app.NewApp("t")
	a.RegisterScreen(app.NewScreen("/dash", &homeComp{}).WithPolicy(authedPolicy()), nil)

	_, err := a.RenderPage(context.Background(), "/dash")
	if err == nil {
		t.Fatalf("legacy RenderPage should error on redirect decision")
	}
}
