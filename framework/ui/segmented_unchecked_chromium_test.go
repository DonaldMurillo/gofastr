package ui_test

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// A segmented control whose Selected matches no segment checks none,
// and its pill must not sit under the first segment claiming a choice
// the group does not hold. A checked group still draws it.
func TestSegmentedUncheckedHidesPill(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	th := theme.Default()
	var css strings.Builder
	for _, e := range registry.All() {
		css.WriteString(e.CSSFor(th))
		css.WriteString("\n")
	}
	opts := []ui.SegmentedOption{{Label: "Day", Value: "day"}, {Label: "Week", Value: "week"}}
	head := "<style>" + th.CSSCustomProperties() + "\n" + css.String() + "</style>"
	body := `<div style="width:320px;padding:16px">` +
		string(ui.SegmentedControl(ui.SegmentedControlConfig{Name: "a", ID: "unlisted", Label: "A", Selected: "month", Options: opts})) +
		`<br>` +
		string(ui.SegmentedControl(ui.SegmentedControlConfig{Name: "b", ID: "listed", Label: "B", Selected: "week", Options: opts})) +
		`</div>`
	srv := themeTestPageWithHead(t, head, body)
	ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(400, 200))
	var got map[string]any
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const vis = id => getComputedStyle(document.querySelector("#" + id + " .fui-segmented__indicator")).visibility;
		return {unlisted: vis("unlisted"), listed: vis("listed")};
	})()`, &got)); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if got["unlisted"] != "hidden" {
		t.Errorf("the pill shows with no segment checked: %v", got)
	}
	if got["listed"] != "visible" {
		t.Errorf("the pill is hidden on a checked group: %v", got)
	}
}
