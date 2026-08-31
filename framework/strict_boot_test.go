package framework

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// bootScreen satisfies strict mode's per-screen checks.
type bootScreen struct{}

func (bootScreen) Render() render.HTML       { return render.HTML("<main>ok</main>") }
func (bootScreen) ScreenTitle() string       { return "Boot" }
func (bootScreen) ScreenDescription() string { return "A boot test screen." }

// backOfficeBattery registers its routes during Init, the admin-battery
// shape: routes exist only after App.Start has initialized batteries,
// never at the UI host's Mount.
type backOfficeBattery struct{}

func (backOfficeBattery) Name() string { return "back-office" }
func (backOfficeBattery) Init(a *App) error {
	a.Router().GetFunc("/admin", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "back office")
	})
	return nil
}

// strictBootSite builds a strict site whose sidebar links the battery's
// route: the exact shape a `gofastr generate` app ships (strict on, admin
// battery registered, "Back office" nav entry).
func strictBootSite() *coreapp.App {
	site := coreapp.NewApp("demo")
	site.Register("/", &bootScreen{}, nil)
	site.SetDefaultLayout(coreapp.NewLayout("app").WithSidebar(
		coreapp.NewStaticComponent(render.HTML(`<nav><a href="/admin">Back office</a></nav>`)),
	))
	return site
}

// A chrome link to a route the app registers AFTER the host's Mount (a
// battery's Init) must not fail strict boot: the route exists and serves
// by the time the app takes traffic, which is the moment the check runs.
func TestStrictLinksAcceptRoutesRegisteredAfterMount(t *testing.T) {
	host := uihost.New(strictBootSite(),
		uihost.WithStrict(),
		uihost.WithDescription("A demo app."),
		uihost.WithFavicon("/static/favicon.svg"),
		uihost.WithSitemap(uihost.SitemapConfig{BaseURL: "https://example.com"}),
		uihost.WithRobots(uihost.RobotsConfig{}),
	)
	fwApp := NewApp(WithoutDefaultMiddleware())
	fwApp.Mount(host)
	fwApp.RegisterBattery(backOfficeBattery{})

	ready := make(chan string, 1)
	fwApp.OnReady(func(addr string) { ready <- addr })
	done := make(chan error, 1)
	go func() { done <- fwApp.Start("127.0.0.1:0") }()

	var addr string
	select {
	case addr = <-ready:
	case err := <-done:
		t.Fatalf("strict boot failed on a link to a battery-registered route: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("boot never became ready")
	}

	// The link must be real, not exempted away: /admin serves.
	resp, err := http.Get("http://" + addr + "/admin")
	if err != nil {
		t.Fatalf("GET /admin: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "back office") {
		t.Fatalf("GET /admin = %d %q; the strict-passing link must actually serve", resp.StatusCode, body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = fwApp.Shutdown(ctx)
	<-done
}

// The other edge of the same coin: moving the link check later must not
// disable it. A chrome link to a path NOTHING serves (no screen, no
// route, no battery) must still refuse the boot, at the App.Start level
// where the check now lives. OnReady fires only if the check was
// skipped entirely (a regression would otherwise hang this test on a
// serving app), and shuts the app down so the failure is an assertion,
// not a timeout.
func TestStrictLinksRefuseBootOnGenuinelyMissingRoute(t *testing.T) {
	site := coreapp.NewApp("demo")
	site.Register("/", &bootScreen{}, nil)
	site.SetDefaultLayout(coreapp.NewLayout("marketing").WithSidebar(
		coreapp.NewStaticComponent(render.HTML(`<nav><a href="/gone">Gone</a></nav>`)),
	))
	host := uihost.New(site,
		uihost.WithStrict(),
		uihost.WithDescription("A demo app."),
		uihost.WithFavicon("/static/favicon.svg"),
		uihost.WithSitemap(uihost.SitemapConfig{BaseURL: "https://example.com"}),
		uihost.WithRobots(uihost.RobotsConfig{}),
	)
	fwApp := NewApp(WithoutDefaultMiddleware())
	fwApp.Mount(host)
	fwApp.OnReady(func(string) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = fwApp.Shutdown(ctx)
	})

	msg := func() (msg string) {
		defer func() {
			if r := recover(); r != nil {
				msg = fmt.Sprint(r)
			}
		}()
		_ = fwApp.Start("127.0.0.1:0") // panics before binding, or returns after the forced shutdown
		return ""
	}()
	if !strings.Contains(msg, `"/gone"`) {
		t.Fatalf("genuinely missing /gone did not refuse strict boot; panic message was:\n%s", msg)
	}
}
