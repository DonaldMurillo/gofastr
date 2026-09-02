package runtime_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/testkit/axetest"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/chromedp/chromedp"
)

// The axe suite lives in the EXTERNAL runtime_test package on purpose:
// it renders its fixtures live from framework/ui (the internal test
// package cannot import it — framework/ui links core-ui/runtime, an
// import cycle for an internal test), and importing it registers the
// real "ui-menu" component stylesheet with the registry, so the test
// server below serves the production CSS at /__gofastr/comp/ instead of
// a bare 404. The layering guard (framework/layering_test.go,
// TestCoreUIDoesNotImportFramework) runs `go list -deps` without
// -test, so a test-only import from an external _test package is not
// an inverted package edge; core-ui/style's tokenmap_test.go already
// imports framework/ui/theme the same way.

// menuTriggerAxeServer serves a fixture inside an axe-friendly page:
// lang, title, a <main> landmark around ALL content, the real runtime,
// and the real component stylesheet from the registry — the production
// pages this markup ships on always serve /__gofastr/comp/ui-menu.css.
// The page pins its own light colors (no theme token sheet exists
// here): axetest.Prepare forces color-scheme via <meta>, which flips
// the UA palette, and without an explicit body color the panel's
// inherited text would go white-on-white in the dark scheme — a
// fixture artifact, not a component fact.
func menuTriggerAxeServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	js, err := runtime.RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/__gofastr/runtime/"), ".js")
		src, ok := runtime.Module(name)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(src))
	})
	mux.HandleFunc("/__gofastr/comp/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/__gofastr/comp/"), ".css")
		for _, e := range registry.All() {
			if e.Name == name {
				w.Header().Set("Content-Type", "text/css")
				w.Write([]byte(e.CSSFor(style.Theme{})))
				return
			}
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/__gofastr/widgets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// The component catalog island: production pages ship it and the
		// kernel reads it at boot (kernel.js _readInlineJSON), which is
		// what makes scanAndLoadCSS fetch /__gofastr/comp/<name>.css.
		// Without it the runtime sees the marker but has no stylePath and
		// silently skips the stylesheet.
		cat := map[string]map[string]string{}
		for _, e := range registry.All() {
			cat[e.Name] = map[string]string{
				"stylePath": "/__gofastr/comp/" + e.Name + ".css",
				"version":   e.VersionFor(style.Theme{}),
				"loadMode":  "auto",
			}
		}
		catJSON, _ := json.Marshal(cat)
		fmt.Fprintf(w, `<!doctype html><html lang="en"><head><title>menu trigger</title>
<style>body { color: #18181B; background: #FFF; }</style>
<script type="application/json" id="gofastr-catalog">%s</script>
</head><body>
<main><h1>Menu trigger fixture</h1>
%s
<span id="ready">ready</span>
</main>
<script src="/__gofastr/runtime.js"></script>
</body></html>`, catJSON, body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// renderTriggerMenu renders the trigger-element fixture live from the
// component: the caller's own button.rounded-full (the pinned host
// shape from the metacollector port — addressable by role+name AND by
// a class-scoped tag selector), an href row, and a Palette submenu of
// theme radios.
func renderTriggerMenu() string {
	return string(ui.Menu(ui.MenuConfig{
		ID:             "um",
		TriggerElement: render.HTML(`<button type="button" class="rounded-full">Open user menu</button>`),
		Items: []ui.MenuItem{
			{Label: "Profile", Href: "/me"},
			{Label: "Palette", Children: []ui.MenuItem{
				{Label: "Light", Radio: "theme"},
				{Label: "Dark", Radio: "theme", Checked: true},
			}},
		},
	}))
}

// menuTriggerAxeAllowlist carries the one rule the trigger path's
// clean scan tolerates, with the reason inline (the discipline
// examples/site's axe gate uses). aria-allowed-role fires on
// <summary role="menuitem"> — the PRE-EXISTING submenu-parent dialect
// framework/ui renders for MenuItem.Children (writeSubMenu). This scan
// is the first axe scan in the repo of an OPEN menu, so the dialect
// has never been seen by a gate; it is identical on the summary path
// and out of scope for the trigger-element contract.
// nested-interactive is NOT allowlisted — it is the rule this API
// exists to satisfy.
var menuTriggerAxeAllowlist = map[string]string{
	"aria-allowed-role": "summary role=menuitem is the existing submenu-parent dialect, not the trigger-element surface",
}

// TestMenuTriggerAxeClean: the trigger-element path must carry no
// nested-interactive (an interactive control inside the summary
// control — the axe SERIOUS violation that made TriggerHTML unusable
// for host buttons), in BOTH the closed and the open state, under both
// color schemes, with the real component CSS applied.
func TestMenuTriggerAxeClean(t *testing.T) {
	srv := menuTriggerAxeServer(t, renderTriggerMenu())
	browser := axetest.NewBrowser(t)
	for _, scheme := range axetest.Schemes {
		ctx, cancel := axetest.NewTab(t, browser)
		if err := chromedp.Run(ctx,
			chromedp.Navigate(srv.URL+"/"),
			chromedp.WaitVisible(`#ready`, chromedp.ByID),
			chromedp.Sleep(700*time.Millisecond),
			axetest.Prepare(scheme),
		); err != nil {
			cancel()
			t.Fatalf("axe setup (%s): %v", scheme, err)
		}
		vs, err := axetest.Scan(ctx, scheme, nil)
		cancel()
		if err != nil {
			t.Fatalf("axe scan closed (%s): %v", scheme, err)
		}
		for _, v := range vs {
			if reason, ok := menuTriggerAxeAllowlist[v.ID]; ok {
				t.Logf("(%s, closed) allowlisted %s: %s", scheme, v.ID, reason)
				continue
			}
			t.Errorf("closed menu (%s scheme): [%s] %s", scheme, v.ID, v.Help)
		}

		// Same page with the menu OPEN: panel rows are in the tree now.
		ctx2, cancel2 := axetest.NewTab(t, browser)
		if err := chromedp.Run(ctx2,
			chromedp.Navigate(srv.URL+"/"),
			chromedp.WaitVisible(`#ready`, chromedp.ByID),
			chromedp.Sleep(700*time.Millisecond),
			axetest.Prepare(scheme),
			chromedp.Click(`button.rounded-full`, chromedp.ByQuery),
			chromedp.Sleep(250*time.Millisecond),
		); err != nil {
			cancel2()
			t.Fatalf("axe open setup (%s): %v", scheme, err)
		}
		vs2, err := axetest.Scan(ctx2, scheme, nil)
		cancel2()
		if err != nil {
			t.Fatalf("axe scan open (%s): %v", scheme, err)
		}
		for _, v := range vs2 {
			if reason, ok := menuTriggerAxeAllowlist[v.ID]; ok {
				t.Logf("(%s, open) allowlisted %s: %s", scheme, v.ID, reason)
				continue
			}
			t.Errorf("open menu (%s scheme): [%s] %s", scheme, v.ID, v.Help)
		}
	}
}

// TestMenuTriggerAxeNestingControl proves the axe gate can see the
// violation class at all: the OLD shape — the same host button routed
// through TriggerHTML, landing INSIDE the framework <summary> — fires
// nested-interactive even while the menu is CLOSED (the summary and
// its button are always visible). Without this control, a scanner
// silently skipping the rule would make the clean scan above vacuous.
func TestMenuTriggerAxeNestingControl(t *testing.T) {
	body := string(ui.Menu(ui.MenuConfig{
		ID:          "sm",
		TriggerHTML: render.HTML(`<button type="button" class="rounded-full">Open user menu</button>`),
		Items:       []ui.MenuItem{{Label: "Profile", Href: "/me"}},
	}))
	srv := menuTriggerAxeServer(t, body)
	browser := axetest.NewBrowser(t)
	ctx, cancel := axetest.NewTab(t, browser)
	defer cancel()
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
		axetest.Prepare(axetest.Schemes[0]),
	); err != nil {
		t.Fatal(err)
	}
	vs, err := axetest.Scan(ctx, axetest.Schemes[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		if v.ID == "nested-interactive" {
			return // the scanner sees the class; the clean scan is meaningful
		}
	}
	t.Errorf("summary-path TriggerHTML nesting produced %d violations, none nested-interactive — the axe gate is blind to the rule this API exists to satisfy", len(vs))
}
