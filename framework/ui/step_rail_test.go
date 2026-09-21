package ui

import (
	"strings"
	"testing"
)

func TestStepRailRendersItemsAndMarksActive(t *testing.T) {
	h := string(StepRail(StepRailConfig{
		Title: "The path",
		Items: []StepRailItem{
			{Number: "01", Anchor: "s1", Label: "Install"},
			{Number: "02", Anchor: "s2", Label: "Scaffold"},
			{Number: "03", Anchor: "s3", Label: "First entity"},
		},
		ActiveIndex: 1,
		Meta:        "Stuck? Open the journal.",
	}))

	for _, want := range []string{
		`data-fui-comp="ui-step-rail"`,
		`role="complementary"`,
		`aria-label="The path"`,
		`class="fui-step-rail__title"`,
		`>The path<`,
		`href="#s1"`,
		`href="#s2"`,
		`href="#s3"`,
		`>Install<`,
		`>Scaffold<`,
		`>First entity<`,
		`Stuck? Open the journal.`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("StepRail missing %q\n%s", want, h)
		}
	}

	// Active item: exactly one step carries data-state=current with
	// aria-current, and it is the s2 entry (Scaffold).
	if n := strings.Count(h, `data-state="current"`); n != 1 {
		t.Errorf("exactly one current step expected, got %d:\n%s", n, h)
	}
	liAt := strings.Index(h, `data-state="current"`)
	liOpen := strings.LastIndex(h[:liAt], "<li")
	liChunk := h[liOpen:]
	if !strings.Contains(liChunk, `aria-current="step"`) {
		t.Errorf("the current step is not aria-current:\n%s", liChunk)
	}
	if !strings.Contains(liChunk[:strings.Index(liChunk, "</li>")], "Scaffold") {
		t.Errorf("the current step is not the s2 entry:\n%s", liChunk)
	}
}

// The caller's Number is text, never markup: the marker slot is typed
// render.HTML so ProgressSteps can pass an SVG, so the adapter must
// escape the string it receives.
func TestStepRailEscapesTheMarkerNumber(t *testing.T) {
	h := string(StepRail(StepRailConfig{Items: []StepRailItem{
		{Number: "<b>1</b>", Anchor: "s1", Label: "Install"},
	}}))
	if !strings.Contains(h, "&lt;b&gt;1&lt;/b&gt;") {
		t.Errorf("the marker number was not escaped:\n%s", h)
	}
	if strings.Contains(h, "<b>1</b>") {
		t.Errorf("raw markup reached the marker:\n%s", h)
	}
}

func TestStepRailMetaHrefRendersLink(t *testing.T) {
	h := string(StepRail(StepRailConfig{
		Items:       []StepRailItem{{Number: "01", Anchor: "s1", Label: "Install"}},
		ActiveIndex: 0,
		Meta:        "Ask in Discussions",
		MetaHref:    "https://example.com/discuss",
	}))
	if !strings.Contains(h, `href="https://example.com/discuss"`) {
		t.Fatalf("MetaHref should render an anchor; got %q", h)
	}
	if !strings.Contains(h, "Ask in Discussions") {
		t.Fatalf("Meta text missing; got %q", h)
	}

	// Without MetaHref, Meta stays plain text (no anchor around it).
	plain := string(StepRail(StepRailConfig{
		Items:       []StepRailItem{{Number: "01", Anchor: "s1", Label: "Install"}},
		ActiveIndex: 0,
		Meta:        "Plain note",
	}))
	if !strings.Contains(plain, `fui-step-rail__meta">Plain note</div>`) {
		t.Fatalf("Meta without MetaHref should be plain text in the meta div; got %q", plain)
	}
}

func TestStepRailDefaultsAriaLabelWhenTitleEmpty(t *testing.T) {
	h := string(StepRail(StepRailConfig{
		Items: []StepRailItem{{Number: "01", Anchor: "a", Label: "x"}},
	}))
	if !strings.Contains(h, `aria-label="Page steps"`) {
		t.Errorf("missing default aria-label:\n%s", h)
	}
	if strings.Contains(h, "fui-step-rail__title") {
		t.Errorf("empty Title should not render the title element:\n%s", h)
	}
}

func TestStepRailPanicsOnOutOfRangeActiveIndex(t *testing.T) {
	for _, idx := range []int{-2, 5, 999} {
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("ActiveIndex=%d should panic — silent no-active is a footgun", idx)
				}
			}()
			StepRail(StepRailConfig{
				Items: []StepRailItem{
					{Number: "01", Anchor: "a", Label: "x"},
					{Number: "02", Anchor: "b", Label: "y"},
				},
				ActiveIndex: idx,
			})
		}()
	}
	// -1 is the explicit "no active step" sentinel; must NOT panic.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ActiveIndex=-1 (sentinel for no-active) must not panic: %v", r)
		}
	}()
	_ = StepRail(StepRailConfig{
		Items:       []StepRailItem{{Number: "01", Anchor: "a", Label: "x"}},
		ActiveIndex: -1,
	})
}

func TestStepRailRequiresAtLeastOneItem(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("StepRail with no items should panic")
		}
	}()
	StepRail(StepRailConfig{Items: nil})
}

func TestStepRailExtraAttrsOnRoot(t *testing.T) {
	h := StepRail(StepRailConfig{
		Items:      []StepRailItem{{Number: "01", Anchor: "s1", Label: "One"}},
		ExtraAttrs: map[string]string{"data-test": "hook", "role": "banner"},
	})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("aside missing data-test:\n%s", root)
	}
	if !strings.Contains(root, `role="complementary"`) {
		t.Errorf("owned role must win over ExtraAttrs:\n%s", root)
	}
}
