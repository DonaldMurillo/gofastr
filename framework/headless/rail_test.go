package headless

import (
	"strings"
	"testing"
)

func renderRail(p RailProps) string { return string(Rail(p, nil)) }

func TestRailRendersFragmentLinksAndLandmark(t *testing.T) {
	h := renderRail(RailProps{Label: "By intent", Items: []RailItem{
		{Anchor: "modeling", Text: "Modeling", Eyebrow: "01", Count: "9"},
		{Anchor: "serving", Text: "Serving"},
	}})
	for _, want := range []string{
		`<aside aria-label="By intent">`,
		`<a href="#modeling">`,
		`>01<`, `>9<`,
		`<a href="#serving">`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("rail missing %q:\n%s", want, h)
		}
	}
	// The label is a plain label, not a heading: the landmark is
	// already named by the aria-label, and a heading here would sit
	// out of order in the page outline.
	if strings.Contains(h, "<h") {
		t.Errorf("rail rendered a heading inside a named landmark:\n%s", h)
	}
}

func TestRailArmsTheObserverOnlyWhenAsked(t *testing.T) {
	observed := renderRail(RailProps{Label: "L", ObserveSelector: "#region", TargetSelector: "section[id]",
		Items: []RailItem{{Anchor: "a", Text: "A"}}})
	for _, want := range []string{"data-hui-rail-observe=\"#region\"", "data-hui-rail-target=\"section[id]\""} {
		if !strings.Contains(observed, want) {
			t.Errorf("observed rail missing %q:\n%s", want, observed)
		}
	}
	static := renderRail(RailProps{Label: "L", Items: []RailItem{{Anchor: "a", Text: "A"}}})
	if strings.Contains(static, "data-hui-rail-observe") || strings.Contains(static, "data-hui-rail-target") {
		t.Errorf("static rail carries observation hooks it cannot use:\n%s", static)
	}
	// The target selector rides along only with an observe selector:
	// a target with nothing to watch it from is markup for no one.
	stray := renderRail(RailProps{Label: "L", TargetSelector: "section[id]",
		Items: []RailItem{{Anchor: "a", Text: "A"}}})
	if strings.Contains(stray, "data-hui-rail-target") {
		t.Errorf("a rail with no ObserveSelector rendered a target:\n%s", stray)
	}
}

func TestRailScrubsCarriedText(t *testing.T) {
	h := renderRail(RailProps{Label: "L", Items: []RailItem{
		{Anchor: "a", Text: "Mod\r\neling", Eyebrow: "0\r1", Count: "9\n"},
	}})
	for _, want := range []string{">Modeling<", ">01<", ">9<"} {
		if !strings.Contains(h, want) {
			t.Errorf("carried text was not scrubbed into %q:\n%s", want, h)
		}
	}
	if strings.ContainsAny(h, "\r\n") {
		// The only newlines a render carries are none: this package
		// renders single-line markup.
		t.Errorf("control bytes reached the rail markup")
	}
}

func TestRailRefusesBrokenConfiguration(t *testing.T) {
	cases := []struct {
		name string
		p    RailProps
	}{
		{"no label", RailProps{Items: []RailItem{{Anchor: "a", Text: "A"}}}},
		{"whitespace-only label", RailProps{Label: "  ", Items: []RailItem{{Anchor: "a", Text: "A"}}}},
		{"no items", RailProps{Label: "L"}},
		{"no anchor", RailProps{Label: "L", Items: []RailItem{{Text: "A"}}}},
		{"selector-shaped anchor", RailProps{Label: "L", Items: []RailItem{{Anchor: "#a", Text: "A"}}}},
		{"no text", RailProps{Label: "L", Items: []RailItem{{Anchor: "a"}}}},
		{"duplicate anchors", RailProps{Label: "L", Items: []RailItem{
			{Anchor: "a", Text: "A"}, {Anchor: "a", Text: "B"},
		}}},
		{"control bytes in selector", RailProps{Label: "L", ObserveSelector: "#a\r\n", Items: []RailItem{{Anchor: "a", Text: "A"}}}},
		{"markup in selector", RailProps{Label: "L", ObserveSelector: "<script>", Items: []RailItem{{Anchor: "a", Text: "A"}}}},
	}
	for _, tc := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: rendering should have been refused", tc.name)
				}
			}()
			Rail(tc.p, nil)
		}()
	}
}
