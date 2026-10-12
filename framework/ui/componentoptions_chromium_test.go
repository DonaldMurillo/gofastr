//go:build chromium

package ui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/chromedp"
)

// Hard rule 9 for the scope half of the option cascade: a primary
// button inside a palette-only scope (no Components of its own, the
// shape of examples/meridian's ink band) must paint the SCOPE's
// primary, in light and in dark. With the option variables declared
// only at :root, --fui-button-primary-bg resolved to the root's
// indigo before inheritance and the scoped button drew indigo with
// white ink. This renders the root theme, the scope block and the
// button sheet in Chrome and reads the computed fill of a button in
// and out of the scope.
func TestPaletteOnlyScopePaintsPrimary(t *testing.T) {
	button := func(name string) string {
		return string(Button(ButtonConfig{
			Label:      "Compare plans",
			Variant:    ButtonPrimary,
			ExtraAttrs: html.Attrs{"data-probe": name},
		}))
	}
	page := render.HTML(button("root") +
		`<div class="cui-theme-h">` + button("scoped") + `</div>`)
	css := style.DefaultTheme().CSSCustomProperties() + "\n" +
		style.ThemeOverrideCSS("h", paletteOnlyTheme()) + "\n" +
		buttonStyle.Entry().CSSFor(theme.Default())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html data-color-scheme="%s"><meta charset=utf-8>
<style>%s</style>%s`, r.URL.Query().Get("scheme"), css, string(page))
	}))
	defer srv.Close()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.WSURLReadTimeout(90*time.Second),
			chromedp.NoSandbox, chromedp.WindowSize(800, 600))...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	defer cancelTimeout()

	const ink, inkText = "rgb(139, 128, 242)", "rgb(21, 20, 27)"
	for _, scheme := range []string{"light", "dark"} {
		var got map[string]string
		if err := chromedp.Run(ctx,
			chromedp.Navigate(srv.URL+"/?scheme="+scheme),
			chromedp.Evaluate(`(() => {
				const cs = name => getComputedStyle(document.querySelector('[data-probe="' + name + '"]'));
				return {rootBg: cs('root').backgroundColor,
					scopedBg: cs('scoped').backgroundColor, scopedFg: cs('scoped').color};
			})()`, &got),
		); err != nil {
			t.Fatal(err)
		}
		// Anti-vacuity: the root button must paint some other fill, or
		// the scoped probe cannot tell a rebound variable from an
		// inherited one.
		if got["rootBg"] == ink || got["rootBg"] == "rgba(0, 0, 0, 0)" {
			t.Fatalf("%s: root button fill %s cannot discriminate the scope (want the root primary)", scheme, got["rootBg"])
		}
		if got["scopedBg"] != ink {
			t.Errorf("%s: scoped primary button fill = %s, want the scope's primary %s (root drew %s)", scheme, got["scopedBg"], ink, got["rootBg"])
		}
		if got["scopedFg"] != inkText {
			t.Errorf("%s: scoped primary button ink = %s, want the scope's primary-fg %s", scheme, got["scopedFg"], inkText)
		}
	}
}
