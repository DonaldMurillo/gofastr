package uihost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
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

// TestSubsetCompletesInTheBrowser drives a page that inlines a subset of
// its bundle: the page paints styled from the subset, the fullbundle
// behaviour links the full bundle right after the subset and before
// app.css, a class the subset dropped is styled once it arrives, the
// per-component sheets are never fetched on top, a marker naming any
// other URL links nothing, the kernel's stylesheet wait covers the full
// bundle, and the strict policy refuses nothing.
func TestSubsetCompletesInTheBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test")
	}
	registry.IsolateForTest(t)
	registry.RegisterBehavior("fullbundle", fullBundleJS, registry.Markers("[data-cui-bundle-href]"))
	heavy := heavySubsetStyle(t, "fui-sub-used", "fui-sub-other")
	light := registerTestStyle(t, "fp-sub-light")
	application := app.NewApp("Test App")
	application.SetDefaultLayout(chromeTestLayout("main", &testHeaderComp{}, nil, &testFooterComp{}))
	application.RegisterScreen(app.NewScreen("/", &classPage{style: heavy, classes: []string{"fui-sub-used"}, also: []*registry.Style{light}}).WithTitle("Home"), nil)
	host := New(application)
	// The full bundle arrives late, so the page can see whether the
	// kernel's stylesheet wait covers it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__gofastr/comp-bundle.css" {
			time.Sleep(400 * time.Millisecond)
		}
		host.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t)
	var got struct {
		Waited     bool     `json:"waited"`
		NavWaited  bool     `json:"navWaited"`
		Used       string   `json:"used"`
		Other      string   `json:"other"`
		Order      bool     `json:"order"`
		Spent      bool     `json:"spent"`
		Foreign    []string `json:"foreign"`
		PerComp    []string `json:"perComp"`
		Violations []string `json:"violations"`
	}
	if err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			// A second marked block naming a foreign sheet, as injected
			// markup would: the module must spend its marker and link
			// nothing for it.
			_, err := page.AddScriptToEvaluateOnNewDocument(`window.__v=[];document.addEventListener('securitypolicyviolation',e=>window.__v.push(e.violatedDirective+' '+(e.sample||'')),true);
				new MutationObserver((_, o) => {
					const s = document.querySelector('style[data-cui-bundle-href]');
					if (!s) return;
					o.disconnect();
					for (const href of ['//evil.invalid/x.css', '/__gofastr/comp/../../evil.css?']) {
						const f = document.createElement('meta');
						f.setAttribute('data-cui-bundle-href', href);
						s.before(f);
					}
				}).observe(document, {childList: true, subtree: true});
				// When the full bundle's link goes in, ask the kernel's
				// stylesheet wait whether it settles only once the sheet has.
				new MutationObserver((_, o) => {
					const l = document.querySelector('link[rel="stylesheet"][href^="/__gofastr/comp-bundle.css"]');
					if (!l) return;
					o.disconnect();
					window.__gofastr._stylesReady.then(() => { window.__waited = !!l.sheet; });
					// A navigation bringing markup for a component the subset
					// covers waits on the same sheet before it swaps.
					const n = (l.getAttribute('data-cui-bundle') || '').split(',')[0];
					window.__gofastr._cssReady('<div data-cui-comp="' + n + '"></div>').then(() => { window.__navWaited = !!n && !!l.sheet; });
				}).observe(document, {childList: true, subtree: true});`).Do(ctx)
			return err
		}),
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitReady(`link[rel="stylesheet"][href^="/__gofastr/comp-bundle.css"]`, chromedp.ByQuery),
		chromedp.Evaluate(`(async () => {
			const link = document.querySelector('link[rel="stylesheet"][href^="/__gofastr/comp-bundle.css"]');
			await window.__gofastr._stylesReady;
			const waited = window.__waited === true;
			const navWaited = window.__navWaited === true;
			const style = document.querySelector('style[data-cui-bundle]');
			const app = document.getElementById('cui-app-css');
			const probe = document.createElement('p');
			probe.className = 'fui-sub-other';
			document.querySelector('.fui-sub-used').parentElement.appendChild(probe);
			return {
				waited,
				navWaited,
				used: getComputedStyle(document.querySelector('.fui-sub-used')).color,
				other: getComputedStyle(probe).color,
				order: style.nextElementSibling === link &&
					!!(link.compareDocumentPosition(app) & Node.DOCUMENT_POSITION_FOLLOWING),
				spent: !document.querySelector('[data-cui-bundle-href]'),
				foreign: [...document.querySelectorAll('link')].map(l => l.href).filter(h => h.includes('evil')),
				perComp: performance.getEntriesByType('resource').map(e => e.name).filter(n => n.includes('/__gofastr/comp/')),
				violations: window.__v,
			};
		})()`, &got, func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }),
	); err != nil {
		t.Fatal(err)
	}
	if !got.Waited {
		t.Error("the kernel's stylesheet wait settled before the full bundle loaded")
	}
	if !got.NavWaited {
		t.Error("a navigation's sheet wait for a subset component settled before the full bundle loaded")
	}
	if got.Used != "rgb(1, 2, 3)" {
		t.Errorf("the rendered class is %s, not styled by the subset", got.Used)
	}
	if got.Other != "rgb(1, 2, 3)" {
		t.Errorf("a class the subset dropped is %s after the full bundle loaded", got.Other)
	}
	if !got.Order {
		t.Error("the full bundle is not linked right after the subset, before app.css")
	}
	if !got.Spent {
		t.Error("a block still carries data-cui-bundle-href")
	}
	if len(got.Foreign) > 0 {
		t.Errorf("the module linked a foreign sheet: %q", got.Foreign)
	}
	if len(got.PerComp) > 0 {
		t.Errorf("per-component sheets fetched on top of the bundle: %q", got.PerComp)
	}
	if len(got.Violations) > 0 {
		t.Errorf("policy violations: %q", got.Violations)
	}
}
