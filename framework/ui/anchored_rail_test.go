package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/patterns/scrollspy"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

func TestAnchoredRailScrollspyWrapperPreservesStickyContract(t *testing.T) {
	h := string(AnchoredRail(AnchoredRailConfig{
		Label:           "On this page",
		Items:           []RailItem{{Anchor: "overview", Text: "Overview"}},
		ObserveSelector: "#docs-sections",
	}))

	if !strings.Contains(h, `class="scrollspy scrollspy--sticky"`) {
		t.Fatalf("scrollspy-wrapped AnchoredRail should mark its wrapper sticky:\n%s", h)
	}
	if !strings.Contains(h, `data-fui-scrollspy="#docs-sections"`) {
		t.Fatalf("scrollspy wrapper should keep its observe selector:\n%s", h)
	}
}

func TestAnchoredRailScrollspyWrapperUsesRailStickyCSS(t *testing.T) {
	css := scrollspy.Style.Entry().CSSFor(style.Theme{})
	for _, want := range []string{
		`.scrollspy.scrollspy--sticky`,
		`position: sticky`,
		`top: calc(var(--nav-h, 60px) + var(--spacing-lg, 16px))`,
		`align-self: start`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("scrollspy sticky wrapper CSS missing %q:\n%s", want, css)
		}
	}
}

func TestAnchoredRailExtraAttrsOnRoot(t *testing.T) {
	h := AnchoredRail(AnchoredRailConfig{
		Label:      "Sections",
		Items:      []RailItem{{Text: "A", Anchor: "a"}},
		ExtraAttrs: map[string]string{"data-test": "hook"},
	})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("aside root missing data-test:\n%s", root)
	}
}
