//go:build chromium

package ui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

// A dark-scheme block for the variants used to sit after the hover rule
// and pin the secondary border to --color-border, so in dark mode a
// hovered secondary button kept its resting border instead of the
// strong one. Hovered under the dark palette, the border is
// --color-border-strong's dark value.
func TestBackToTopDarkHoverBorder(t *testing.T) {
	th := theme.Default()
	css := th.CSSCustomProperties() + backToTopStyle.Entry().CSSFor(th)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html data-color-scheme="dark"><meta charset=utf-8>
<style>%s</style><style>*{transition:none!important}</style><body>%s</body>`,
			css, string(BackToTop(BackToTopConfig{Variant: BackToTopSecondary})))
	}))
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var box map[string]float64
	var rest, hover string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		// The navigation runtime writes the visible state; there is no
		// runtime here, so set it.
		chromedp.Evaluate(`document.querySelector('[data-cui-comp="ui-back-to-top"]').setAttribute('data-hui-back-to-top-visible', '')`, nil),
		chromedp.Evaluate(`(() => { const b = document.querySelector('.fui-back-to-top').getBoundingClientRect(); return {x: b.x + b.width/2, y: b.y + b.height/2}; })()`, &box),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.fui-back-to-top')).borderTopColor`, &rest),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return input.DispatchMouseEvent(input.MouseMoved, box["x"], box["y"]).Do(ctx)
		}),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.fui-back-to-top')).borderTopColor`, &hover),
	); err != nil {
		t.Fatal(err)
	}
	if rest != "rgb(39, 39, 42)" {
		t.Fatalf("resting border = %s, want the dark --color-border rgb(39, 39, 42); the dark palette did not apply", rest)
	}
	if hover != "rgb(63, 63, 70)" {
		t.Errorf("hovered border = %s, want the dark --color-border-strong rgb(63, 63, 70)", hover)
	}
}
