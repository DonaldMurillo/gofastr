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
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/chromedp"
)

// A blank source line renders as an empty line span. With no text it
// had no line box, so its height was 0 and its gutter number painted
// over the next line's (the docs site's home sample showed "4" on top
// of "5"). Both per-line paths are covered: Lines, and Code with a
// per-line feature on.
func TestCodeBlockBlankLineKeepsItsRow(t *testing.T) {
	page := render.HTML(string(CodeBlock(CodeBlockConfig{
		ExtraAttrs:  html.Attrs{"data-cb": "lines"},
		Filename:    "main.go",
		LineNumbers: true,
		Lines:       []render.HTML{render.Text("a := 1"), "", render.Text("b := 2")},
	})) + string(CodeBlock(CodeBlockConfig{
		ExtraAttrs:     html.Attrs{"data-cb": "code"},
		Filename:       "main.go",
		LineNumbers:    true,
		HighlightLines: []LineRange{{From: 3}},
		Code:           "a := 1\n\nb := 2\n",
	})))
	css := codeBlockStyle.Entry().CSSFor(theme.Default())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset=utf-8>
<style>body{margin:0}</style>
<style>%s</style>%s`, css, string(page))
	}))
	defer srv.Close()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.WSURLReadTimeout(90*time.Second),
			chromedp.NoSandbox, chromedp.WindowSize(1024, 800))...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	defer cancelTimeout()
	var got map[string][]float64
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.Evaluate(`(() => {
			const out = {};
			for (const name of ['lines', 'code']) {
				const rows = document.querySelectorAll('[data-cb="' + name + '"] .fui-code-block__line');
				out[name] = [...rows].map(r => r.getBoundingClientRect().height);
			}
			return out;
		})()`, &got),
	); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"lines", "code"} {
		h := got[name]
		if len(h) != 3 {
			t.Fatalf("%s: want 3 line rows, got %d", name, len(h))
		}
		if h[0] < 8 {
			t.Fatalf("%s: first row is %.1fpx tall; the page did not lay out", name, h[0])
		}
		if h[1]+0.5 < h[0] {
			t.Errorf("%s: blank row is %.1fpx tall, a text row %.1fpx; its number overlaps the next", name, h[1], h[0])
		}
	}
}
