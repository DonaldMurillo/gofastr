package ui_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	ui "github.com/DonaldMurillo/gofastr/framework/ui"
)

// Property: widgets that pre-generate per-index CSS or per-item markup
// bound their input instead of rendering unbounded output. Tabs emits a
// global stylesheet covering tabsMaxPanels indices, so a strip longer
// than that would silently hide panels; the contract is a loud panic.

// TestTabsPanelCapPanicsBeyondMax pins both sides: the ceiling is
// enforced with a panic (never a silent truncate), and a strip at the
// ceiling renders completely.
func TestTabsPanelCapPanicsBeyondMax(t *testing.T) {
	mustPanic(t, "more than tabsMaxPanels", func() {
		tabs := make([]ui.TabItem, 25)
		for i := range tabs {
			tabs[i] = ui.TabItem{Label: "T", Content: render.Text("c")}
		}
		_ = ui.Tabs(ui.TabsConfig{SignalName: "t", Tabs: tabs})
	})

	atCap := make([]ui.TabItem, 24)
	for i := range atCap {
		atCap[i] = ui.TabItem{Label: "T", Content: render.Text("c")}
	}
	h := string(ui.Tabs(ui.TabsConfig{SignalName: "t", Tabs: atCap}))
	if !strings.Contains(h, `data-fui-tab-index="23"`) {
		t.Errorf("a 24-tab strip must render all 24 panels:\n%.200s", h)
	}
}

// TestRepeaterNegativeMinItemsNoBlowup pins the negative-MinItems
// branch: a negative count must not drive a huge or negative loop; the
// template expansion loop simply renders zero items and the attribute
// carries the raw value for the runtime to treat as "no floor".
func TestRepeaterNegativeMinItemsNoBlowup(t *testing.T) {
	h := string(ui.Repeater(ui.RepeaterConfig{
		Name:     "rows",
		MinItems: -1000,
		Template: func(int) render.HTML { return render.Text("t") },
	}))
	// Count the item wrapper class exactly; the region class is
	// "ui-repeater-items", a superstring of "ui-repeater-item".
	if n := strings.Count(h, `class="ui-repeater-item"`); n != 0 {
		t.Errorf("negative MinItems rendered %d template items; expected none:\n%s", n, h)
	}
	if !strings.Contains(h, `data-min-items="-1000"`) {
		t.Errorf("MinItems must still be carried on the region attribute:\n%s", h)
	}
}

// TestCarouselVisiblePerViewClamped pins the visible-per-view clamp:
// the emitted modifier class only has CSS for cols-1..cols-8, so any
// out-of-range request (huge or negative) must clamp into that band,
// never render a class with no matching rule.
func TestCarouselVisiblePerViewClamped(t *testing.T) {
	for in, want := range map[int]string{99: "8", -3: "1", 0: "1"} {
		h := string(ui.Carousel(ui.CarouselConfig{
			Label:          "L",
			VisiblePerView: in,
			Slides:         []ui.CarouselSlide{{Content: render.Text("s")}},
		}))
		if !strings.Contains(h, "ui-carousel--cols-"+want+" ") && !strings.Contains(h, "ui-carousel--cols-"+want+`"`) {
			t.Errorf("VisiblePerView=%d must clamp to cols-%s:\n%s", in, want, h)
		}
	}
}
