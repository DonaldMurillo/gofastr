package ui

import (
	"strings"
	"testing"
)

func TestAnchoredRailRendersThroughHeadlessRail(t *testing.T) {
	h := string(AnchoredRail(AnchoredRailConfig{
		Label:           "On this page",
		Items:           []RailItem{{Anchor: "overview", Text: "Overview", Eyebrow: "01", Count: 9}},
		ObserveSelector: "#docs-sections",
	}))
	for _, want := range []string{
		`<aside aria-label="On this page" class="fui-anchored-rail" data-hui-rail="" data-hui-rail-observe="#docs-sections" data-hui-rail-target=".fui-section[id]" data-fui-comp="ui-anchored-rail">`,
		`<a class="fui-anchored-rail__link" href="#overview"><span class="fui-anchored-rail__eyebrow">01</span>Overview<span class="fui-anchored-rail__count">9</span></a>`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("rail missing %q:\n%s", want, h)
		}
	}
	// The retired scrollspy wrapper is gone: the aside itself is the
	// layout item the caller composes.
	if strings.Contains(h, "data-fui-scrollspy") || strings.Contains(h, `class="scrollspy`) {
		t.Errorf("the retired scrollspy wrapper is still rendered:\n%s", h)
	}
}

func TestAnchoredRailStaticWithoutObserver(t *testing.T) {
	h := string(AnchoredRail(AnchoredRailConfig{
		Label: "Sections",
		Items: []RailItem{{Text: "A", Anchor: "a"}},
	}))
	if strings.Contains(h, "data-hui-rail-observe") {
		t.Errorf("a rail with no ObserveSelector carries observer wiring:\n%s", h)
	}
	if !strings.Contains(h, `href="#a"`) {
		t.Errorf("static rail lost its fragment link:\n%s", h)
	}
}

func TestAnchoredRailExtraAttrsOnRoot(t *testing.T) {
	h := string(AnchoredRail(AnchoredRailConfig{
		Label:      "Sections",
		Items:      []RailItem{{Text: "A", Anchor: "a"}},
		Class:      "site-rail",
		ExtraAttrs: map[string]string{"data-test": "hook", "data-hui-rail": "forged"},
	}))
	for _, want := range []string{`data-test="hook"`, "site-rail"} {
		if !strings.Contains(h, want) {
			t.Errorf("rail lost %q:\n%s", want, h)
		}
	}
	if strings.Contains(h, `data-hui-rail="forged"`) {
		t.Errorf("a caller forged the rail hook:\n%s", h)
	}
}
