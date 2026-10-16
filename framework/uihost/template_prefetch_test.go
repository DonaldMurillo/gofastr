package uihost

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// templateHeldPage renders live on the page and held only inside a
// <template>, the way a confirm dialog or toast item ships.
type templateHeldPage struct{ live, held *registry.Style }

func (p *templateHeldPage) Render() render.HTML {
	return p.live.Render(&innerSpan{body: "live"}) +
		`<template data-probe>` + p.held.Render(&innerSpan{body: "held"}) + `</template>`
}

func newTemplateHeldHost(live, held *registry.Style) *UIHost {
	application := app.NewApp("Test App")
	application.SetDefaultLayout(chromeTestLayout("main", &testHeaderComp{}, nil, &testFooterComp{}))
	application.RegisterScreen(app.NewScreen("/", &templateHeldPage{live: live, held: held}).WithTitle("Home"), nil)
	return New(application)
}

// A sheet only template content needs stays off the render-blocking
// links and arrives as a prefetch, at the URL the runtime will ask for
// when it clones the template: stylePath plus ?v=version from the
// catalog. Any other spelling is a second download.
func TestTemplateSheetIsPrefetched(t *testing.T) {
	registry.IsolateForTest(t)
	live := registerTestStyle(t, "tpl-live")
	held := registerTestStyle(t, "tpl-held")
	ds := newTemplateHeldHost(live, held)
	body := linkedPageBody(t, ds, "/")

	var cat map[string]struct {
		StylePath string `json:"stylePath"`
		Version   string `json:"version"`
	}
	if err := json.Unmarshal(catalogJSON(ds, ds.activeTheme(), ""), &cat); err != nil {
		t.Fatal(err)
	}
	e := cat[held.Name()]
	runtimeURL := e.StylePath + "?v=" + e.Version
	if want := `<link rel="prefetch" href="` + runtimeURL + `">`; !strings.Contains(body, want) {
		t.Errorf("missing %s in:\n%s", want, truncate(body, 1500))
	}
	if strings.Contains(body, `rel="stylesheet" href="/__gofastr/comp/`+held.Name()) {
		t.Errorf("template-held %s got a render-blocking link", held.Name())
	}
	if !strings.Contains(body, `rel="stylesheet" href="/__gofastr/comp/`+live.Name()) {
		t.Errorf("rendered %s lost its stylesheet link", live.Name())
	}
	if strings.Contains(body, `rel="prefetch" href="/__gofastr/comp/`+live.Name()) {
		t.Errorf("rendered %s was prefetched as well", live.Name())
	}
}

// The prefetch only pays off when Chrome reuses it: the runtime's
// stylesheet link for the cloned template must not reach the server a
// second time.
func TestTemplateSheetPrefetchIsReused(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test")
	}
	registry.IsolateForTest(t)
	live := registerTestStyle(t, "tpl-live")
	held := registerTestStyle(t, "tpl-held")
	ds := newTemplateHeldHost(live, held)
	var hits atomic.Int32
	heldPath := "/__gofastr/comp/" + held.Name() + ".css"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == heldPath {
			hits.Add(1)
		}
		ds.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t)
	var loaded bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.Poll(`!!window.__gofastr && !!window.__gofastr_catalog`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for hits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if hits.Load() != 1 {
		t.Fatalf("prefetch requests = %d, want 1", hits.Load())
	}
	clone := `(() => {
		const el = document.querySelector('template[data-probe]').content.firstElementChild.cloneNode(true);
		document.body.appendChild(el);
		window.__gofastr.scanAndLoadCSS(document.body);
	})()`
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(clone, nil),
		chromedp.Poll(`(() => { const l = document.querySelector('link[data-cui-style="`+held.Name()+`"]'); return !!(l && l.sheet); })()`,
			&loaded, chromedp.WithPollingTimeout(5*time.Second)),
	); err != nil {
		t.Fatalf("the runtime never loaded the held sheet: %v", err)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("held sheet fetched %d times, want 1 (the runtime missed the prefetch)", n)
	}
}

// An eager sheet is already render-blocking on every page; a template
// that also carries it adds no prefetch for the same bytes.
func TestEagerTemplateSheetNotPrefetched(t *testing.T) {
	registry.IsolateForTest(t)
	live := registerTestStyle(t, "tpl-live")
	held := registerTestStyle(t, "tpl-eager", registry.WithLoad(registry.LoadAlways))
	body := linkedPageBody(t, newTemplateHeldHost(live, held), "/")
	if strings.Contains(body, `rel="prefetch" href="/__gofastr/comp/`+held.Name()) {
		t.Errorf("eager %s was prefetched as well as linked", held.Name())
	}
	if !regexp.MustCompile(`<link rel="stylesheet"[^>]*[=,]` + regexp.QuoteMeta(held.Name()) + `[&,".]`).MatchString(body) {
		t.Errorf("eager %s lost its stylesheet link:\n%s", held.Name(), truncate(body, 1200))
	}
}
