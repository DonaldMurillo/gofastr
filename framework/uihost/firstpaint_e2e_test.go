package uihost

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// TestInlinedHeadAppliesUnderCSP asks the browser the question the header
// test cannot: under the framework's strict policy, do the inlined sheets
// and the bootstrap actually apply? A hash that misses by one byte leaves
// the page unstyled with nothing but a console violation to show for it.
func TestInlinedHeadAppliesUnderCSP(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test")
	}
	registry.IsolateForTest(t)
	ds := newTestUIHostForMany(registerTestStyle(t, "fp-e2e-a"), registerTestStyle(t, "fp-e2e-b"))
	srv := httptest.NewServer(ds) // the standalone host installs the default policy
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t)
	var got struct {
		Inline     int      `json:"inline"`
		Applied    int      `json:"applied"`
		Scheme     string   `json:"scheme"`
		Violations []string `json:"violations"`
	}
	if err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(`window.__v=[];document.addEventListener('securitypolicyviolation',e=>window.__v.push(e.violatedDirective+' '+(e.sample||'')),true);`).Do(ctx)
			return err
		}),
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitReady(`body`, chromedp.ByQuery),
		chromedp.Evaluate(`(() => {
			const els = [...document.querySelectorAll('style[data-cui-bundle], style#cui-app-css')];
			return {
				inline: els.length,
				applied: els.filter(s => s.sheet && s.sheet.cssRules.length > 0).length,
				scheme: document.documentElement.dataset.colorScheme || '',
				violations: window.__v,
			};
		})()`, &got),
	); err != nil {
		t.Fatal(err)
	}
	if got.Inline != 2 {
		t.Fatalf("%d inline sheets on the page, want the bundle and app.css", got.Inline)
	}
	if got.Applied != got.Inline {
		t.Errorf("%d of %d inline sheets applied", got.Applied, got.Inline)
	}
	if got.Scheme == "" {
		t.Error("the color-scheme bootstrap did not run")
	}
	if len(got.Violations) > 0 {
		t.Errorf("policy violations: %q", got.Violations)
	}
}
