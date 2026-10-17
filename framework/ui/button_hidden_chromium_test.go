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
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/chromedp"
)

// A button rendered hidden (one script reveals once it can drive it,
// like the passkey buttons) must not show on a page without that
// script: .fui-button's display beat the UA's [hidden] rule.
func TestHiddenButtonStaysHidden(t *testing.T) {
	th := theme.Default(theme.Overrides{})
	css := th.CSSCustomProperties() + buttonStyle.Entry().CSSFor(th)
	page := Button(ButtonConfig{Label: "Sign in with a passkey", ID: "b", ExtraAttrs: html.Attrs{"hidden": ""}})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html><meta charset=utf-8><style>%s</style>%s`, css, string(page))
	}))
	defer srv.Close()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:], chromedp.WSURLReadTimeout(90*time.Second), chromedp.NoSandbox)...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	defer cancelTimeout()

	var hiddenAttr bool
	var width float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.Evaluate(`document.getElementById('b').hasAttribute('hidden')`, &hiddenAttr),
		chromedp.Evaluate(`document.getElementById('b').getBoundingClientRect().width`, &width),
	); err != nil {
		t.Fatal(err)
	}
	if !hiddenAttr {
		t.Fatal("setup: the hidden attribute did not reach the button")
	}
	if width != 0 {
		t.Errorf("hidden button is %vpx wide; it must take no space", width)
	}
}
