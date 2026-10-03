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
	"github.com/chromedp/chromedp"
)

// Hard rule 9: whether two fields share the same surface is a computed
// style, not a stylesheet string. The select carried no background of
// its own, so the UA painted its base colour — in dark schemes a
// lighter grey than the text inputs beside it. This renders a Select
// and a TextField in one page and compares the computed
// background-color (and border colour) in both schemes.
func TestSelectSharesTheFieldSurface(t *testing.T) {
	page := Select(SelectConfig{
		Name: "theme", Label: "Theme",
		Options: []SelectOption{{Value: "system", Text: "Match this device", Selected: true}},
	}) + TextField(TextFieldConfig{Name: "name", Label: "Workspace name", Value: "Acme"})
	css := selectStyle.Entry().CSSFor(theme.Default()) + formFieldStyle.Entry().CSSFor(theme.Default())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset=utf-8>
<style>*,*::before,*::after{box-sizing:border-box}body{margin:0;padding:24px}%s
%s</style>%s`,
			theme.Default().CSSCustomProperties(), css, string(page))
	}))
	defer srv.Close()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.WSURLReadTimeout(90*time.Second),
			chromedp.NoSandbox)...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	defer cancelTimeout()

	for _, scheme := range []string{"light", "dark"} {
		var m map[string]string
		if err := chromedp.Run(ctx,
			chromedp.Navigate(srv.URL),
			chromedp.WaitVisible(".fui-select", chromedp.ByQuery),
			chromedp.Evaluate(
				`document.documentElement.setAttribute('data-color-scheme', '`+scheme+`')`, nil),
			chromedp.Evaluate(`(() => {
				const sel = getComputedStyle(document.querySelector('.fui-select'));
				const input = getComputedStyle(document.querySelector('.fui-input'));
				return {selectBg: sel.backgroundColor, inputBg: input.backgroundColor,
					selectBorder: sel.borderTopColor, inputBorder: input.borderTopColor};
			})()`, &m),
		); err != nil {
			t.Fatalf("%s: %v", scheme, err)
		}
		if m["selectBg"] == "rgba(0, 0, 0, 0)" {
			t.Fatalf("%s: the select still paints no background of its own", scheme)
		}
		if m["selectBg"] != m["inputBg"] {
			t.Errorf("%s: select background %s != text input %s — the fields must share the surface",
				scheme, m["selectBg"], m["inputBg"])
		}
		if m["selectBorder"] != m["inputBorder"] {
			t.Errorf("%s: select border %s != text input %s", scheme, m["selectBorder"], m["inputBorder"])
		}
	}
}
