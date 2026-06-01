package interactive

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

func sampleMenu() SectionMenuConfig {
	return SectionMenuConfig{
		AriaLabel:    "Documentation sections",
		TriggerLabel: "Sections",
		DrawerName:   "docs-section-menu",
		Lead:         &SectionItem{Label: "Overview", Href: "/docs/"},
		Groups: []SectionGroup{
			{Eyebrow: "01", Label: "Modeling", Items: []SectionItem{
				{Label: "Entities", Href: "/docs/entities", Active: true},
				{Label: "Filter DSL", Href: "/docs/dsl"},
			}},
			{Eyebrow: "02", Label: "Serving", Collapsed: true, Items: []SectionItem{
				{Label: "HTTP", Href: "/docs/http"},
			}},
		},
	}
}

func TestSectionMenuRendersLandmarkRailAndTrigger(t *testing.T) {
	h := string(SectionMenu(sampleMenu()))
	for _, want := range []string{
		`data-fui-comp="fui-section-menu"`,
		`<nav`,
		`aria-label="Documentation sections"`,
		`class="fui-section-menu__trigger"`,
		`data-fui-open="docs-section-menu"`, // opens the mounted drawer widget
		`class="fui-section-menu__rail"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("SectionMenu missing %q\n%s", want, h)
		}
	}
}

func TestSectionMenuNoTriggerWithoutDrawerName(t *testing.T) {
	cfg := sampleMenu()
	cfg.DrawerName = ""
	h := string(SectionMenu(cfg))
	if strings.Contains(h, "fui-section-menu__trigger") {
		t.Errorf("no DrawerName should omit the mobile trigger:\n%s", h)
	}
}

func TestSectionMenuActiveItemMarked(t *testing.T) {
	h := string(SectionMenu(sampleMenu()))
	if !strings.Contains(h, `aria-current="page"`) {
		t.Errorf("active item should carry aria-current=page:\n%s", h)
	}
	if !strings.Contains(h, "is-active") {
		t.Errorf("active item should carry the is-active class:\n%s", h)
	}
}

func TestSectionMenuActiveGroupForcedOpen(t *testing.T) {
	h := string(SectionMenu(sampleMenu()))
	modeling := h[strings.Index(h, "Modeling")-200 : strings.Index(h, "Modeling")]
	if !strings.Contains(modeling, "open") {
		t.Errorf("group containing the active item must be open:\n%s", modeling)
	}
}

func TestSectionMenuCollapsedGroupClosed(t *testing.T) {
	h := string(SectionMenu(SectionMenuConfig{
		DrawerName: "x",
		Groups: []SectionGroup{
			{Label: "Closed", Collapsed: true, Items: []SectionItem{{Label: "x", Href: "/x"}}},
		},
	}))
	start := strings.Index(h, `class="fui-section-menu__group"`)
	if start == -1 {
		t.Fatalf("no group rendered:\n%s", h)
	}
	openTag := h[start:strings.Index(h[start:], ">")+start]
	if strings.Contains(openTag, "open") {
		t.Errorf("collapsed group without an active item should be closed: %q", openTag)
	}
}

func TestSectionMenuLeadAndEyebrow(t *testing.T) {
	h := string(SectionMenu(sampleMenu()))
	if !strings.Contains(h, "fui-section-menu__lead") || !strings.Contains(h, "Overview") {
		t.Errorf("lead item should render:\n%s", h)
	}
	if !strings.Contains(h, "fui-section-menu__eyebrow") || !strings.Contains(h, ">01<") {
		t.Errorf("group eyebrow should render:\n%s", h)
	}
}

// The mobile sheet is the framework's preset.Drawer — backdrop + dismiss
// behaviours come from the widget, not re-implemented here.
func TestSectionMenuDrawerIsADismissibleWidget(t *testing.T) {
	def := SectionMenuDrawer(sampleMenu()).Build()
	if def.Name != "docs-section-menu" {
		t.Errorf("drawer name = %q, want docs-section-menu", def.Name)
	}
	if !def.Backdrop {
		t.Error("drawer should render a backdrop")
	}
	if !def.CloseOnClickOutside {
		t.Error("drawer must close on outside click (the reported bug)")
	}
	if !def.Hidden {
		t.Error("drawer should start hidden")
	}
}

func TestSectionMenuDrawerHasVisibleCloseButton(t *testing.T) {
	h := string(sectionMenuDrawerSlot{cfg: sampleMenu()}.Render())
	if !strings.Contains(h, "fui-section-menu__close") {
		t.Errorf("drawer should render a close button:\n%s", h)
	}
	// Uses the framework's declarative widget-dismiss hook.
	if !strings.Contains(h, `data-fui-action="close"`) {
		t.Errorf("close button must use data-fui-action=close to dismiss the widget:\n%s", h)
	}
}

func TestSectionMenuDrawerRequiresName(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("SectionMenuDrawer without DrawerName should panic")
		}
	}()
	cfg := sampleMenu()
	cfg.DrawerName = ""
	SectionMenuDrawer(cfg)
}

func TestSectionMenuCSSScopedRailAndTrigger(t *testing.T) {
	css := sectionMenuCSS(style.Theme{})
	if !strings.Contains(css, `[data-fui-comp="fui-section-menu"]`) {
		t.Fatal("CSS must be scoped to the component marker")
	}
	for _, want := range []string{
		"@media (min-width: 900px)",          // desktop rail
		"__rail",                             // rail layout
		"__trigger",                          // mobile trigger button
		"prefers-reduced-motion",             // motion guard
	} {
		if !strings.Contains(css, want) {
			t.Errorf("CSS missing %q", want)
		}
	}
}
