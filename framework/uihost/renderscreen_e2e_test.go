package uihost

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// TestRenderScreenE2E_BrandedRecoveryPaints drives the full guard
// wiring in a real browser: RouteMatchMiddleware + a guard calling
// RenderScreen(410). The branded page must paint with the runtime
// chrome attached (a page the runtime booted on), proving the recovery
// arm produces a real document, not a bare fragment.
func TestRenderScreenE2E_BrandedRecoveryPaints(t *testing.T) {
	application := app.NewApp("fieldassist")
	application.SetDefaultLayout(app.NewLayout("main"))
	application.RegisterScreen(
		app.NewScreen("/", &rawHTMLComp{html: `<h1 id="home">sessions</h1>`}).WithTitle("home"),
		nil,
	)
	application.RegisterScreen(
		app.NewScreen("/session/{sessionId}", &paramJSONComp{}).WithTitle("session"),
		nil,
	)

	host := New(application)
	rt := router.New()
	rt.Use(host.RouteMatchMiddleware())
	rt.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if m, ok := app.MatchFromContext(r.Context()); ok && m.Param("sessionId") == "dead" {
				host.RenderScreen(w, r, &rawHTMLComp{html: `
					<h1 id="gone">This session has ended</h1>
					<p>The operator closed it. Ask them to share a new link.</p>`},
					ScreenResponse{Status: http.StatusGone})
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	host.Mount(rt)

	srv := httptest.NewServer(rt)
	t.Cleanup(srv.Close)

	chCtx := newE2EChromeForUIHost(t)
	var heading, readyState string
	var shot []byte
	err := chromedp.Run(chCtx,
		chromedp.Navigate(srv.URL+"/session/dead"),
		chromedp.WaitVisible("#gone", chromedp.ByID),
		chromedp.Text("#gone", &heading, chromedp.ByID),
		chromedp.Evaluate(`document.readyState`, &readyState),
		chromedp.FullScreenshot(&shot, 640),
	)
	if err != nil {
		t.Fatalf("navigate to recovery page: %v", err)
	}
	if strings.TrimSpace(heading) != "This session has ended" {
		t.Errorf("heading = %q", heading)
	}
	// The runtime boot marker: the recovery page carries the same
	// chrome a normal page does, so __gofastr exists after hydration.
	var hasRuntime bool
	if err := chromedp.Run(chCtx, chromedp.Evaluate(
		`typeof window.__gofastr === 'object' && !!document.querySelector('script[src*="runtime.js"]')`,
		&hasRuntime)); err != nil || !hasRuntime {
		t.Errorf("recovery page lacks runtime chrome (err=%v)", err)
	}
	os.WriteFile("/tmp/gofastr-vis-recovery-screen.png", shot, 0o644)
}
