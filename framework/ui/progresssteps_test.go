package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"strings"
	"testing"
)

func TestProgressStepsRequiresAtLeastOneStep(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("ProgressSteps without Steps should panic")
		}
	}()
	ProgressSteps(ProgressStepsConfig{})
}

func TestProgressStepsRendersOLWithNav(t *testing.T) {
	h := string(ProgressSteps(ProgressStepsConfig{
		Steps: []ProgressStep{{Label: "A"}, {Label: "B"}},
	}))
	if !strings.Contains(h, "<nav") {
		t.Errorf("ProgressSteps should wrap in <nav>:\n%s", h)
	}
	if !strings.Contains(h, "<ol") {
		t.Errorf("ProgressSteps inner list should be <ol>:\n%s", h)
	}
	if !strings.Contains(h, `aria-label="Progress"`) {
		t.Errorf("nav should default aria-label=Progress:\n%s", h)
	}
}

func TestProgressStepsCurrentEmitsAriaCurrent(t *testing.T) {
	h := string(ProgressSteps(ProgressStepsConfig{
		Steps: []ProgressStep{
			{Label: "Done", Status: ProgressStepComplete},
			{Label: "Now", Status: ProgressStepCurrent},
			{Label: "Later"},
		},
	}))
	if !strings.Contains(h, `aria-current="step"`) {
		t.Errorf("current step should have aria-current=step:\n%s", h)
	}
	if !strings.Contains(h, `data-state="current"`) {
		t.Errorf("current step should have modifier class:\n%s", h)
	}
	if !strings.Contains(h, `data-state="done"`) {
		t.Errorf("complete step should have modifier class:\n%s", h)
	}
}

func TestProgressStepsCompleteWithHrefIsLink(t *testing.T) {
	h := string(ProgressSteps(ProgressStepsConfig{
		Steps: []ProgressStep{
			{Label: "Done", Status: ProgressStepComplete, Href: "/back"},
			{Label: "Now", Status: ProgressStepCurrent},
		},
	}))
	if !strings.Contains(h, `href="/back"`) {
		t.Errorf("complete + Href should render an <a>:\n%s", h)
	}
}

// Upcoming steps ignore Href — the field's own contract: only a
// completed step is a way back.
func TestProgressStepsUpcomingStepIgnoresHref(t *testing.T) {
	h := string(ProgressSteps(ProgressStepsConfig{
		Steps: []ProgressStep{
			{Label: "Done", Status: ProgressStepComplete, Href: "/back"},
			{Label: "Now", Status: ProgressStepCurrent},
			{Label: "Later", Href: "/ahead"},
		},
	}))
	if !strings.Contains(h, `href="/back"`) {
		t.Errorf("complete + Href should render an <a>:\n%s", h)
	}
	if strings.Contains(h, `href="/ahead"`) {
		t.Errorf("an upcoming step's Href became an anchor:\n%s", h)
	}
	if n := strings.Count(h, "<a "); n != 1 {
		t.Errorf("expected exactly one anchor, got %d:\n%s", n, h)
	}
}

func TestProgressStepsVerticalOrientation(t *testing.T) {
	h := string(ProgressSteps(ProgressStepsConfig{
		Orientation: ProgressStepsVertical,
		Steps:       []ProgressStep{{Label: "A"}},
	}))
	if !strings.Contains(h, "fui-progress-steps--vertical") {
		t.Errorf("Vertical orientation should add modifier class:\n%s", h)
	}
}

func TestProgressStepsRejectsUnknownStatus(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("ProgressSteps with unknown Status should panic")
		}
	}()
	ProgressSteps(ProgressStepsConfig{
		Steps: []ProgressStep{{Label: "x", Status: ProgressStepStatus("bogus")}},
	})
}

// ExtraAttrs land on the <nav> root but never override what the
// component owns (#262): aria-label keeps its framework value; class
// and data-fui-* case-variants are dropped.
func TestProgressStepsExtraAttrsCannotOverrideOwned(t *testing.T) {
	h := string(ProgressSteps(ProgressStepsConfig{
		Label: "Checkout", Class: "mine",
		Steps: []ProgressStep{{Label: "A"}},
		ExtraAttrs: map[string]string{
			"data-test": "hook", "aria-label": "evil", "Class": "evil", "data-fui-comp": "spoof",
		},
	}))
	root := h[:strings.Index(h, ">")+1]
	for _, banned := range []string{"evil", "spoof"} {
		if strings.Contains(root, banned) {
			t.Errorf("owned attr overridden by ExtraAttrs (%q):\n%s", banned, root)
		}
	}
	for _, want := range []string{
		`data-test="hook"`, `aria-label="Checkout"`, `class="fui-progress-steps mine"`,
	} {
		if !strings.Contains(root, want) {
			t.Errorf("nav missing %q:\n%s", want, root)
		}
	}
}

// The vertical connector is a short bar above each item, the exact
// geometry the base sheet drew; the modifier class alone proves
// nothing about it.
func TestProgressStepsVerticalConnectorGeometry(t *testing.T) {
	css := progressStepsCSS(style.Theme{})
	i := strings.Index(css, ".fui-progress-steps--vertical .fui-progress-steps__item + .fui-progress-steps__item::before")
	if i < 0 {
		t.Fatalf("the vertical connector rule is gone:\n%s", css)
	}
	rule := css[i:]
	rule = rule[:strings.Index(rule, "}")]
	for _, want := range []string{"left: 13px", "right: auto", "top: -12px", "bottom: auto", "width: 2px", "height: 12px"} {
		if !strings.Contains(rule, want) {
			t.Errorf("the vertical connector lost %q:\n%s", want, rule)
		}
	}
}
