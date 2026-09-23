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
	trigger := render.HTML(`<button class="fui-button">Help</button>`)
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
	mustContain(t, h, "fui-tooltip--bottom")
}

func TestTooltipDefaultPlacementOmitsModifier(t *testing.T) {
	h := Tooltip(TooltipConfig{Text: "x"}, render.Text("trigger"))
	if classTokenPresent(string(h), "fui-tooltip--top") {
		t.Fatalf("default top placement should not emit modifier:\n%s", h)
	}
}

func TestTooltipCustomIDOverridesSlug(t *testing.T) {
	h := Tooltip(TooltipConfig{Text: "Hello", ID: "my-id"},
		render.HTML(`<span>x</span>`))
	mustContain(t, h, `aria-describedby="my-id"`)
	mustContain(t, h, `id="my-id"`)
}

func TestTooltipExtraAttrsOnRoot(t *testing.T) {
	h := Tooltip(TooltipConfig{
		Text:       "tip",
		ExtraAttrs: map[string]string{"data-test": "hook"},
	}, render.Text("trigger"))
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("wrapper span missing data-test:\n%s", root)
	}
}

// The idempotence check is scoped to the trigger's FIRST OPEN TAG: a
// descendant carrying aria-describedby (an icon with its own
// description inside the trigger) must not read as the trigger root
// being wired, or the tooltip's own relationship never lands.
func TestTooltipWiresTheRootWhenADescendantCarriesDescribedBy(t *testing.T) {
	trigger := render.HTML(`<button class="fui-button"><svg aria-label="info" aria-describedby="icon-note"></svg>Help</button>`)
	h := Tooltip(TooltipConfig{Text: "Need help?"}, trigger)
	s := string(h)
	// The root button carries the tooltip's id.
	if !strings.Contains(s, `<button class="fui-button" aria-describedby="tip-need-help">`) {
		t.Errorf("the trigger root was left unwired by its descendant's attribute:\n%s", s)
	}
	// The descendant keeps its own description untouched.
	if !strings.Contains(s, `aria-describedby="icon-note"`) {
		t.Errorf("the descendant's own description was clobbered:\n%s", s)
	}
}

// The insertion point is the first `>` that closes the open tag,
// respecting attribute quotes: a `>` inside a quoted attribute value
// (title="a > b") must not terminate the tag early and splice the
// attribute into the value.
func TestTooltipSplicesPastAGtInsideAQuotedValue(t *testing.T) {
	trigger := render.HTML(`<button type="button" title="small > large">Sort</button>`)
	h := Tooltip(TooltipConfig{Text: "Sort order"}, trigger)
	// The value is intact and the attribute landed after it, inside
	// the real tag close.
	mustContain(t, h, `<button type="button" title="small > large" aria-describedby="tip-sort-order">`)
	// The corrupted spelling — the splice inside the quoted value —
	// is absent.
	if strings.Contains(string(h), `title="small aria-describedby=`) {
		t.Errorf("the splice landed inside the quoted attribute value:\n%s", h)
	}
}

// A trigger root that already carries the attribute is returned
// unchanged — idempotence survives the scoping fix.
func TestTooltipIdempotentWhenRootCarriesDescribedBy(t *testing.T) {
	trigger := render.HTML(`<button aria-describedby="tip-x">Help</button>`)
	h := Tooltip(TooltipConfig{Text: "Need help?", ID: "tip-x"}, trigger)
	if n := strings.Count(string(h), `aria-describedby="tip-x"`); n != 1 {
		t.Errorf("aria-describedby rendered %d times, want 1:\n%s", n, h)
	}
}
