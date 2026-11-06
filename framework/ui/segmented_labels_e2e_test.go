package ui_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/chromedp/chromedp"
)

func TestSegmentLabelsChooseOwnValue(t *testing.T) {
	body := `<form>` + string(ui.SegmentedControl(ui.SegmentedControlConfig{
		Name: "language", Label: "Language", Options: []ui.SegmentedOption{{Label: "C++", Value: "C++"}, {Label: "C#", Value: "C#"}},
	})) + `</form>`
	server := themeTestPageWithHead(t, "", body)
	ctx := moduleTestCtxURL(t, server.URL)
	var selected, label string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[role="radiogroup"]').getAttribute('aria-label') || ''`, &label),
		chromedp.Evaluate(`document.querySelectorAll('label')[1].click()`, nil),
		chromedp.Evaluate(`new FormData(document.querySelector('form')).get('language')`, &selected),
	); err != nil {
		t.Fatal(err)
	}
	if selected != "C#" {
		t.Fatalf("second label submitted %q, want C#", selected)
	}
	if label != "Language" {
		t.Fatalf("radiogroup label = %q, want Language", label)
	}
}
