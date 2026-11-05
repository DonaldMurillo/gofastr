//go:build chromium

package interactive

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestOptimisticButtonDoesNotSubmit(t *testing.T) {
	body := render.Tag("form", nil, OptimisticUpdate(Post("/save"), render.Text("Save"), render.Text("Saved")))
	url := menuPage(t, body.String(), false)
	ctx := chromedptest.Context(t)
	var submits int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.Evaluate(`window.submits = 0; document.querySelector('form').addEventListener('submit', e => { e.preventDefault(); window.submits++; });`, nil),
		chromedp.Click("button", chromedp.ByQuery),
		chromedp.Evaluate(`window.submits`, &submits),
	); err != nil {
		t.Fatal(err)
	}
	if submits != 0 {
		t.Fatalf("unhydrated optimistic action submitted its enclosing form %d times", submits)
	}
}
