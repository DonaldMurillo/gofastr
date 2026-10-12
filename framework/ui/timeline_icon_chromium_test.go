package ui_test

import (
	"math"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/chromedp"
)

// An event with an Icon draws a bordered circle around the icon, the
// rail runs through the circle's centre, the variant tints the icon
// rather than filling the circle, and the title clears the marker.
func TestTimelineIconMarker(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	th := theme.Default()
	var css strings.Builder
	for _, e := range registry.All() {
		css.WriteString(e.CSSFor(th))
		css.WriteString("\n")
	}
	head := "<style>" + th.CSSCustomProperties() + "\n" + css.String() + "</style>"
	tl := ui.Timeline(ui.TimelineConfig{Events: []ui.TimelineEvent{
		{Title: "Ada created INV-1", Meta: "2h ago", Icon: "plus", Variant: ui.TimelineSuccess},
		{Title: "Ada deleted INV-2", Meta: "3h ago", Icon: "trash", Variant: ui.TimelineDanger},
		{Title: "Ada edited INV-3", Meta: "4h ago", Icon: "pencil"},
	}})
	body := `<div style="width: 480px; padding: 16px">` + string(tl) + `</div>`
	srv := themeTestPageWithHead(t, head, body)
	ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(800, 600))
	var got map[string]any
	probe := `(() => {
		const item = document.querySelector(".fui-timeline__item");
		const dot = item.querySelector(".fui-timeline__dot").getBoundingClientRect();
		const icon = item.querySelector(".fui-timeline__dot .fui-icon").getBoundingClientRect();
		const title = item.querySelector(".fui-timeline__title").getBoundingClientRect();
		const ir = item.getBoundingClientRect();
		const rail = getComputedStyle(item, "::before");
		const railMid = ir.left + parseFloat(rail.left) + parseFloat(rail.width) / 2;
		const ds = getComputedStyle(item.querySelector(".fui-timeline__dot"));
		const danger = getComputedStyle(document.querySelectorAll(".fui-timeline__dot")[1]);
		const plain = getComputedStyle(document.querySelectorAll(".fui-timeline__dot")[2]);
		const probe = document.createElement("span");
		probe.style.color = "var(--color-border)";
		document.body.append(probe);
		const borderToken = getComputedStyle(probe).color;
		return {
			dotW: dot.width, dotH: dot.height,
			iconMidX: icon.left + icon.width / 2, iconMidY: icon.top + icon.height / 2,
			dotMidX: dot.left + dot.width / 2, dotMidY: dot.top + dot.height / 2,
			railMid: railMid, titleLeft: title.left, dotRight: dot.right,
			border: parseFloat(ds.borderTopWidth), bg: ds.backgroundColor, fg: ds.color,
			dangerBg: danger.backgroundColor, dangerFg: danger.color, plainFg: plain.color,
			borderColor: ds.borderTopColor, borderToken: borderToken,
		};
	})()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(probe, &got)); err != nil {
		t.Fatalf("probe: %v", err)
	}
	f := func(k string) float64 { v, _ := got[k].(float64); return v }
	if f("dotW") < 24 || math.Abs(f("dotW")-f("dotH")) > 0.5 || f("border") < 1 {
		t.Errorf("the marker is not a bordered circle: %v", got)
	}
	if math.Abs(f("iconMidX")-f("dotMidX")) > 1 || math.Abs(f("iconMidY")-f("dotMidY")) > 1 {
		t.Errorf("the icon is off the marker's centre: %v", got)
	}
	if math.Abs(f("railMid")-f("dotMidX")) > 1 {
		t.Errorf("the rail misses the marker's centre: %v", got)
	}
	if f("titleLeft") <= f("dotRight") {
		t.Errorf("the title overlaps the marker: %v", got)
	}
	if got["borderColor"] != got["borderToken"] {
		t.Errorf("the circle's border is not the border token: %v", got)
	}
	if got["dangerBg"] != got["bg"] || got["dangerFg"] == got["plainFg"] {
		t.Errorf("the variant fills the circle instead of tinting the icon: %v", got)
	}
}
