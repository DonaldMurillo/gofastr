//go:build chromium

package ui

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

// Hard rule 9: primary and danger hover mixed the treatment's own
// background toward transparent, so under the outline treatment
// (transparent background) hover painted nothing and under soft it
// barely moved (caught in review). Every treatment answers the pointer:
// the pixel inside the button's padding changes on hover.
func TestButtonHoverShowsUnderTreatments(t *testing.T) {
	for _, tr := range []theme.ButtonTreatment{theme.Filled, theme.Outline, theme.Soft} {
		for _, v := range []ButtonVariant{ButtonPrimary, ButtonDanger} {
			t.Run(fmt.Sprintf("%v/%v", tr, v), func(t *testing.T) {
				th := theme.Default(theme.Overrides{Components: theme.ComponentOptions{Button: theme.ButtonOptions{Treatment: tr}}})
				page := Button(ButtonConfig{Label: "Save changes", Variant: v})
				before, after := hoverPixels(t, th.CSSCustomProperties()+buttonStyle.Entry().CSSFor(th), page)
				if before == after {
					t.Errorf("hover paints the same pixel %v; the button must answer the pointer", before)
				}
			})
		}
	}
}

// hoverPixels screenshots the first .fui-button at rest and under the
// pointer, and returns the colour inside its start padding each time.
func hoverPixels(t *testing.T, css string, page render.HTML) (before, after [4]uint32) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// The light scheme is pinned: headless Chrome follows the OS, and
		// a dark desktop would paint the dark palette on the white body.
		fmt.Fprintf(w, `<!doctype html><html data-color-scheme="light"><meta charset=utf-8>
<style>*,*::before,*::after{box-sizing:border-box}body{margin:40px;background:#fff}</style>
<style>%s</style>%s`, css, string(page))
	}))
	defer srv.Close()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.WSURLReadTimeout(90*time.Second),
			chromedp.NoSandbox, chromedp.WindowSize(800, 400))...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	defer cancelTimeout()

	sample := func(buf []byte) [4]uint32 {
		img, err := png.Decode(bytes.NewReader(buf))
		if err != nil {
			t.Fatal(err)
		}
		b := img.Bounds()
		r, g, bl, a := img.At(b.Min.X+6, b.Min.Y+b.Dy()/2).RGBA()
		return [4]uint32{r >> 8, g >> 8, bl >> 8, a >> 8}
	}
	var rest, hover []byte
	var box map[string]float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.Evaluate(`(() => { const b = document.querySelector('.fui-button').getBoundingClientRect(); return {x: b.x + b.width/2, y: b.y + b.height/2}; })()`, &box),
		// Disable transitions so the hovered frame is the settled one.
		chromedp.Evaluate(`document.head.insertAdjacentHTML('beforeend', '<style>*{transition:none!important}</style>')`, nil),
		chromedp.Screenshot(`.fui-button`, &rest, chromedp.ByQuery),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return input.DispatchMouseEvent(input.MouseMoved, box["x"], box["y"]).Do(ctx)
		}),
		chromedp.Screenshot(`.fui-button`, &hover, chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}
	return sample(rest), sample(hover)
}
