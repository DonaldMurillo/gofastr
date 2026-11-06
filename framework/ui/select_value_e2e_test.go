package ui_test

import (
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/framework/testkit/axetest"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func TestSelectKeepsRetiredValue(t *testing.T) {
	for _, placeholder := range []string{"", "Choose a plan"} {
		t.Run(placeholder, func(t *testing.T) {
			selectHTML := ui.Select(ui.SelectConfig{
				Name: "plan", Label: "Plan", Selected: "enterprise", Placeholder: placeholder,
				Options: []ui.SelectOption{{Value: "pro", Text: "Pro", Selected: true}, {Value: "", Text: "None"}},
			})
			server := menuTriggerAxeServer(t, `<form id="edit">`+string(selectHTML)+`</form>`)
			browser := axetest.NewBrowser(t)
			ctx, cancel := axetest.NewTab(t, browser)
			defer cancel()
			var value string
			if err := chromedp.Run(ctx,
				chromedp.Navigate(server.URL+"/"),
				chromedp.Evaluate(`new FormData(document.getElementById('edit')).get('plan')`, &value),
			); err != nil {
				t.Fatal(err)
			}
			if value != "enterprise" {
				t.Fatalf("saved plan = %q, want enterprise", value)
			}
		})
	}
}
