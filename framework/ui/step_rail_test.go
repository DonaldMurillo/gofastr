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
		`class="ui-step-rail__title"`,
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

	// Active link should have the active modifier class only on item 1.
	activeChunk := strings.Index(h, `href="#s2"`)
	if activeChunk == -1 {
		t.Fatal("missing s2 link")
	}
	preceding := h[:activeChunk]
	lastOpenAnchor := strings.LastIndex(preceding, "<a")
	if lastOpenAnchor == -1 {
		t.Fatal("no <a tag preceding s2")
	}
	activeTag := h[lastOpenAnchor:activeChunk]
	if !strings.Contains(activeTag, "ui-step-rail__link--active") {
		t.Errorf("active item (s2) should carry --active class:\n%s", activeTag)
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
	if !strings.Contains(plain, `ui-step-rail__meta">Plain note</div>`) {
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
	if strings.Contains(h, "ui-step-rail__title") {
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
