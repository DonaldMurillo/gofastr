package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestTooltipRequiresText(t *testing.T) {
	defer func() { recover() }()
	Tooltip(TooltipConfig{}, render.Text("x"))
	t.Fatal("expected panic with empty Text")
}

func TestTooltipWrapsTriggerAndAddsAriaDescribedBy(t *testing.T) {
	trigger := render.HTML(`<button class="ui-button">Help</button>`)
	h := Tooltip(TooltipConfig{Text: "Need help?"}, trigger)
	for _, want := range []string{
		`data-fui-comp="ui-tooltip"`,
		`role="tooltip"`,
		"Need help?",
		`aria-describedby="tip-need-help"`,
	} {
		mustContain(t, h, want)
	}
}

func TestTooltipPlacementVariantClass(t *testing.T) {
	h := Tooltip(TooltipConfig{Text: "x", Placement: TooltipBottom},
		render.Text("trigger"))
	mustContain(t, h, "ui-tooltip--bottom")
}

func TestTooltipDefaultPlacementOmitsModifier(t *testing.T) {
	h := Tooltip(TooltipConfig{Text: "x"}, render.Text("trigger"))
	if strings.Contains(string(h), "ui-tooltip--top") {
		t.Fatalf("default top placement should not emit modifier:\n%s", h)
	}
}

func TestTooltipCustomIDOverridesSlug(t *testing.T) {
	h := Tooltip(TooltipConfig{Text: "Hello", ID: "my-id"},
		render.HTML(`<span>x</span>`))
	mustContain(t, h, `aria-describedby="my-id"`)
	mustContain(t, h, `id="my-id"`)
}
