package uihost

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
)

// TestRenderAlt_AnonSeesAltAtSameURL drives the canonical
// landing/dashboard at-same-URL pattern: "/" is the dashboard for
// authed users, marketing landing for anon. We never log in here, so
// we exercise the anon branch; the assertion is "alt content
// rendered, dashboard content did not leak". Screenshot saved to
// /tmp for the human-eyes verification step.
func TestRenderAlt_AnonSeesAltAtSameURL(t *testing.T) {
	application := app.NewApp("vis")
	application.RegisterScreen(
		app.NewScreen("/", &textComp{tag: "dashboard-real"}).
			WithTitle("Dashboard").
			WithPolicy(app.PolicyFunc(func(ctx context.Context) app.Decision {
				return decide.RenderAlt(func() component.Component {
					return &textComp{tag: "anon-landing"}
				})
			})),
		nil,
	)
	ds := New(application)
	srv := httptest.NewServer(ds)
	t.Cleanup(srv.Close)

	chCtx := newE2EChromeForUIHost(t)
	var body string
	var shot []byte
	err := chromedp.Run(chCtx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`[data-text="anon-landing"]`, chromedp.ByQuery),
		chromedp.Evaluate(`document.body.innerText`, &body),
		chromedp.FullScreenshot(&shot, 90),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !strings.Contains(body, "anon-landing") {
		t.Errorf("alt body missing; got %q", body)
	}
	if strings.Contains(body, "dashboard-real") {
		t.Errorf("dashboard body leaked through despite RenderAlt; got %q", body)
	}
	_ = os.WriteFile("/tmp/gofastr-vis-render-alt-anon.png", shot, 0o644)
}
