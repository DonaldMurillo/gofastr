package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// Right to left the toolbar puts Prev on the right and the arrow keys
// follow it, so Prev's chevron has to point right and Next's left. The
// glyphs kept their left-to-right direction. A left-to-right lightbox
// leaves them as drawn.
func TestLightboxChevronsFlipInRTL(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	th := theme.Default()
	css := th.CSSCustomProperties() + lightboxStyle.Entry().CSSFor(th)
	slot := string((&lightboxSlot{name: "lb", label: "Viewer", navArrows: true}).Render())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html><meta charset=utf-8><style>%s</style><body>
<div id="ltr" dir="ltr">%s</div><div id="rtl" dir="rtl">%s</div></body>`, css, slot, slot)
	}))
	t.Cleanup(srv.Close)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var got map[string]string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.Evaluate(`(() => {
			const tf = sel => { const el = document.querySelector(sel); return el ? getComputedStyle(el).transform : "missing"; };
			return {
				ltrPrev: tf('#ltr .fui-lightbox__nav--prev svg'), ltrNext: tf('#ltr .fui-lightbox__nav--next svg'),
				rtlPrev: tf('#rtl .fui-lightbox__nav--prev svg'), rtlNext: tf('#rtl .fui-lightbox__nav--next svg'),
			};
		})()`, &got),
	); err != nil {
		t.Fatal(err)
	}
	const mirrored = "matrix(-1, 0, 0, 1, 0, 0)"
	for _, k := range []string{"ltrPrev", "ltrNext"} {
		if got[k] != "none" {
			t.Errorf("%s chevron transform = %q, want none", k, got[k])
		}
	}
	for _, k := range []string{"rtlPrev", "rtlNext"} {
		if got[k] != mirrored {
			t.Errorf("%s chevron transform = %q, want %s", k, got[k], mirrored)
		}
	}
}
