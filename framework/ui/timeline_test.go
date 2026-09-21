package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestTimelineRequiresAtLeastOneEvent(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Timeline without Events should panic")
		}
	}()
	Timeline(TimelineConfig{})
}

func TestTimelineEventRequiresTitle(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Timeline event without Title should panic")
		}
	}()
	Timeline(TimelineConfig{Events: []TimelineEvent{{Title: ""}}})
}

func TestTimelineRendersAsOrderedList(t *testing.T) {
	h := string(Timeline(TimelineConfig{
		Events: []TimelineEvent{
			{Title: "First"}, {Title: "Second"},
		},
	}))
	if !strings.Contains(h, "<ol") {
		t.Errorf("Timeline should render as <ol>:\n%s", h)
	}
	if strings.Count(h, "fui-timeline__item") != 2 {
		t.Errorf("expected 2 items in DOM:\n%s", h)
	}
}

func TestTimelineVariantsEmitClass(t *testing.T) {
	h := string(Timeline(TimelineConfig{
		Events: []TimelineEvent{
			{Title: "ok", Variant: TimelineSuccess},
			{Title: "broken", Variant: TimelineDanger},
		},
	}))
	if !strings.Contains(h, "fui-timeline__dot--success") {
		t.Errorf("success variant should add modifier class:\n%s", h)
	}
	if !strings.Contains(h, "fui-timeline__dot--danger") {
		t.Errorf("danger variant should add modifier class:\n%s", h)
	}
}

func TestTimelineRejectsUnknownVariant(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Timeline event with unknown Variant should panic")
		}
	}()
	Timeline(TimelineConfig{
		Events: []TimelineEvent{{Title: "x", Variant: TimelineEventVariant("bogus")}},
	})
}

// A tinted dot is still a dot: the primitive joins the tone variant
// to the mark class, and the map carries only the modifier. A dot
// that loses the base class stretches into a bar (the captures
// caught it).
func TestTimelineVariantDotsKeepTheBaseDotClass(t *testing.T) {
	h := string(Timeline(TimelineConfig{Events: []TimelineEvent{
		{Title: "ok", Variant: TimelineSuccess},
	}}))
	if !strings.Contains(h, `class="fui-timeline__dot fui-timeline__dot--success"`) {
		t.Errorf("the tinted dot lost the base dot class:\n%s", h)
	}
}

// An event without Meta still styles its title as the title: the
// muted supporting prose is keyed on the primitive's detail part, and
// no rule in the sheet targets a bare p that could outrank it.
func TestTimelineTitleKeepsItsOwnRuleWithoutMeta(t *testing.T) {
	h := string(Timeline(TimelineConfig{Events: []TimelineEvent{
		{Title: "Deployed"},
	}}))
	if !strings.Contains(h, `class="fui-timeline__title"`) {
		t.Errorf("the title does not carry its own class:\n%s", h)
	}
	css := timelineCSS(style.Theme{})
	if strings.Contains(css, "> p") {
		t.Errorf("the sheet targets a bare p instead of a part:\n%s", css)
	}
	if !strings.Contains(css, ".fui-timeline__detail") {
		t.Errorf("the sheet does not style the detail part:\n%s", css)
	}
}

// The caller's Body rides in the body wrapper the old markup drew,
// so the sheet's body rule reaches it and nothing else.
func TestTimelineBodyRidesInTheBodyWrapper(t *testing.T) {
	h := string(Timeline(TimelineConfig{Events: []TimelineEvent{
		{Title: "Deployed", Body: render.HTML("<p>Exit 0.</p>")},
	}}))
	if !strings.Contains(h, `<div class="fui-timeline__body"><p>Exit 0.</p></div>`) {
		t.Errorf("the body markup did not ride in the body wrapper:\n%s", h)
	}
	if !strings.Contains(timelineCSS(style.Theme{}), ".fui-timeline__body") {
		t.Errorf("the sheet has no rule for the body wrapper:\n%s", h)
	}
}

// ExtraAttrs land on the <ol> root but never override what the
// component owns (#262): class and data-fui-* variants are dropped
// (there are no other owned attributes on the root).
func TestTimelineExtraAttrsCannotOverrideOwned(t *testing.T) {
	h := string(Timeline(TimelineConfig{
		Class:  "mine",
		Events: []TimelineEvent{{Title: "First"}},
		ExtraAttrs: map[string]string{
			"data-test": "hook", "Class": "evil", "data-fui-comp": "spoof",
		},
	}))
	root := h[:strings.Index(h, ">")+1]
	for _, banned := range []string{"evil", "spoof"} {
		if strings.Contains(root, banned) {
			t.Errorf("owned attr overridden by ExtraAttrs (%q):\n%s", banned, root)
		}
	}
	for _, want := range []string{
		`data-test="hook"`, `class="fui-timeline mine"`,
	} {
		if !strings.Contains(root, want) {
			t.Errorf("root missing %q:\n%s", want, root)
		}
	}
}

// The variant dot rules are scoped under the sheet's marker so they
// outrank the base dot rule; unscoped, a tinted dot rendered grey
// (the captures caught it).
func TestTimelineVariantDotRulesAreScoped(t *testing.T) {
	css := timelineCSS(style.Theme{})
	for _, v := range []string{"success", "warn", "danger", "info"} {
		if !strings.Contains(css, `[data-fui-comp="ui-timeline"] .fui-timeline__dot--`+v) {
			t.Errorf("the %s dot rule is not scoped under the marker:\n%s", v, css)
		}
	}
}
