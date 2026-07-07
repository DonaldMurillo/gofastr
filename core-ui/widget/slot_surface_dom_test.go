package widget

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/chromedp/chromedp"
)

// bareStub is a minimal Component whose rendered HTML is the slot's
// root element — used to plant .fui-slot-bare / data-fui-comp markers
// on the body root for the panel opt-out rendered-DOM tests.
type bareStub struct{ html string }

func (s bareStub) Render() render.HTML { return render.HTML(s.html) }

// A distinctive surface color so a computed rgb() match is unambiguous:
// the test page defines --color-surface as this, and the panel rule
// sets background: var(--color-surface), so a painted panel resolves to
// exactly this rgb() and an opted-out panel does not.
const bareProbeSurface = "rgb(123, 45, 6)"

func newBareBrowserCtx(t *testing.T) context.Context {
	t.Helper()
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.WindowSize(1024, 768),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(allocCancel)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	t.Cleanup(browserCancel)
	ctx, cancel := context.WithTimeout(browserCtx, 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// mountBarePage builds a centered Modal whose body root is bodyHTML,
// generates its resolved stylesheet via widgetCSS, renders the chrome,
// and serves a page that assigns --color-surface a sentinel color and
// embeds the chrome. The .fui-panel computed background reports whether
// the :not(:has(...)) opt-out selector matched.
func mountBarePage(t *testing.T, bodyHTML string) string {
	t.Helper()
	def := New("bare-dom-probe").
		Mount(Center).
		Slot("body", bareStub{html: bodyHTML}).
		Build()
	css := widgetCSS(def)
	chrome := RenderChrome(&def)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html><head><style>
:root{--color-surface:%s;--color-border:#000}
%s
</style></head><body>%s<span id="r">r</span></body></html>`,
			bareProbeSurface, css, chrome)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func barePanelBackgroundRGB(t *testing.T, ctx context.Context, url string) string {
	t.Helper()
	var bg string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(`#r`, chromedp.ByID),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.fui-panel')).backgroundColor`, &bg),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	return bg
}

// TestCenterPanelBareOptOutRendered — the .fui-slot-bare opt-out is
// only string-matched in slot_surface_test.go; this renders real DOM
// and reads the panel's computed background to prove the :has()
// selector ACTUALLY matches (not just that the selector text exists).
func TestCenterPanelBareOptOutRendered(t *testing.T) {
	ctx := newBareBrowserCtx(t)

	// Sanity: a plain body DOES paint the surface — proves the CSS
	// loaded and the panel rule applies, so the opt-out assertions
	// below are meaningful (not vacuously passing on missing CSS).
	if bg := barePanelBackgroundRGB(t, ctx, mountBarePage(t, `<div>plain</div>`)); bg != bareProbeSurface {
		t.Fatalf("plain body panel background = %q, want %q — CSS not applied; opt-out assertions below would be vacuous", bg, bareProbeSurface)
	}

	// (a) .fui-slot-bare on the body's ROOT element suppresses the
	// panel surface — bare means the body owns every pixel.
	if bg := barePanelBackgroundRGB(t, ctx, mountBarePage(t, `<div class="fui-slot-bare">bare</div>`)); bg == bareProbeSurface {
		t.Errorf("bare-root panel painted the surface %q — .fui-slot-bare must opt the panel out of its background", bg)
	}

	// The bare marker must be the DIRECT child of .fui-slot. A wrapper
	// <div> around it defeats the :has(> .fui-slot > .fui-slot-bare)
	// selector, so the panel re-paints — pins the single-root rule
	// the docs state.
	if bg := barePanelBackgroundRGB(t, ctx, mountBarePage(t, `<div><div class="fui-slot-bare">bare</div></div>`)); bg != bareProbeSurface {
		t.Errorf("wrapped .fui-slot-bare panel did NOT paint %q — a wrapper div must defeat the opt-out (marker not a direct child of .fui-slot); got %q", bareProbeSurface, bg)
	}
}

// TestCenterPanelCmdPaletteExclusionRendered — the legacy
// [data-fui-comp="ui-cmd-palette"] branch of the opt-out selector must
// actually match a rendered palette root (the string test only checks
// the selector text; a stray quote or typo would slip past it).
func TestCenterPanelCmdPaletteExclusionRendered(t *testing.T) {
	ctx := newBareBrowserCtx(t)

	if bg := barePanelBackgroundRGB(t, ctx, mountBarePage(t, `<div>plain</div>`)); bg != bareProbeSurface {
		t.Fatalf("plain body panel background = %q, want %q — CSS not applied", bg, bareProbeSurface)
	}
	// (b) data-fui-comp="ui-cmd-palette" on the body root opts out.
	if bg := barePanelBackgroundRGB(t, ctx, mountBarePage(t, `<div data-fui-comp="ui-cmd-palette">palette</div>`)); bg == bareProbeSurface {
		t.Errorf("cmd-palette panel painted the surface %q — the [data-fui-comp=\"ui-cmd-palette\"] exclusion must suppress the panel background", bg)
	}
}
