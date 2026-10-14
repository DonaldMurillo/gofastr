package interactive

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
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
		`data-cui-comp="cui-section-menu"`,
		`<nav`,
		`aria-label="Documentation sections"`,
		`class="cui-sheet-pill"`,
		`data-cui-open="docs-section-menu"`, // opens the mounted drawer widget
		`class="cui-section-menu__rail"`,
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
	if strings.Contains(h, "cui-sheet-pill") {
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
	cfg := sampleMenu()
	for i := range cfg.Groups {
		cfg.Groups[i].Collapsed = true
	}
	h := string(sectionMenuDrawerSlot{cfg: cfg}.Render())
	at := strings.Index(h, `cui-section-menu__group-label">Modeling`)
	modeling := h[at-200 : at]
	if !strings.Contains(modeling, "open") {
		t.Errorf("group containing the active item must be open:\n%s", modeling)
	}
}

// Collapsed is a drawer setting: the drawer starts the group closed,
// while the desktop rail shows every group as a labelled region, not a
// disclosure a reader could shut.
func TestSectionMenuCollapsedGroupClosed(t *testing.T) {
	cfg := SectionMenuConfig{
		DrawerName: "x",
		Groups: []SectionGroup{
			{Label: "Closed", Collapsed: true, Items: []SectionItem{{Label: "x", Href: "/x"}}},
		},
	}
	groupTag := func(h string) string {
		start := strings.Index(h, `class="cui-section-menu__group"`)
		if start == -1 {
			t.Fatalf("no group rendered:\n%s", h)
		}
		return h[start : strings.Index(h[start:], ">")+start]
	}
	if tag := groupTag(string(sectionMenuDrawerSlot{cfg: cfg}.Render())); strings.Contains(tag, "open") {
		t.Errorf("drawer: a collapsed group without an active item should be closed: %q", tag)
	}
	rail := string(SectionMenu(cfg))
	if !strings.Contains(rail, `<div aria-label="Closed" class="cui-section-menu__group" role="group">`) {
		t.Errorf("rail: a group should render as a labelled region:\n%s", rail)
	}
	if strings.Contains(rail, "<summary") || strings.Contains(rail, "<details") {
		t.Errorf("rail: no group should be a disclosure (a pinned <summary> overlaps the links under it):\n%s", rail)
	}
}

func TestSectionMenuLeadAndEyebrow(t *testing.T) {
	h := string(SectionMenu(sampleMenu()))
	if !strings.Contains(h, "cui-section-menu__lead") || !strings.Contains(h, "Overview") {
		t.Errorf("lead item should render:\n%s", h)
	}
	if !strings.Contains(h, "cui-section-menu__eyebrow") || !strings.Contains(h, ">01<") {
		t.Errorf("group eyebrow should render:\n%s", h)
	}
}

// The mobile sheet is the framework's preset.BottomSheet, backdrop +
// dismiss behaviours come from the widget, not re-implemented here.
func TestSectionMenuDrawerIsADismissibleWidget(t *testing.T) {
	def := SectionMenuDrawer(sampleMenu()).Build()
	if def.Position != widget.Bottom || !def.DragDismiss {
		t.Errorf("drawer mounts at %v (drag dismiss %v); want the bottom sheet", def.Position, def.DragDismiss)
	}
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
	// The sheet's dialog is named by its visible title.
	slot := string(sectionMenuDrawerSlot{cfg: sampleMenu()}.Render())
	if def.LabelledBy != "docs-section-menu-title" || !strings.Contains(slot, `id="docs-section-menu-title"`) {
		t.Errorf("dialog labelled by %q; want docs-section-menu-title, the id of the sheet's title", def.LabelledBy)
	}
}

func TestSectionMenuDrawerHasVisibleCloseButton(t *testing.T) {
	h := string(sectionMenuDrawerSlot{cfg: sampleMenu()}.Render())
	if !strings.Contains(h, "cui-sheet-head__close") {
		t.Errorf("drawer should render a close button:\n%s", h)
	}
	// Uses the framework's declarative widget-dismiss hook.
	if !strings.Contains(h, `data-cui-action="close"`) {
		t.Errorf("close button must use data-cui-action=close to dismiss the widget:\n%s", h)
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
	if !strings.Contains(css, `[data-cui-comp="cui-section-menu"]`) {
		t.Fatal("CSS must be scoped to the component marker")
	}
	for _, want := range []string{
		"@media (min-width: 900px)", // desktop rail
		"__rail",                    // rail layout
		"prefers-reduced-motion",    // motion guard
	} {
		if !strings.Contains(css, want) {
			t.Errorf("CSS missing %q", want)
		}
	}
}

// FillColumn marks the root so the desktop rail fills the host's pinned
// column; without it the root carries no fill marker and the rail keeps
// its own sticky offset and viewport cap.
func TestSectionMenuFillColumnMarksRoot(t *testing.T) {
	if h := string(SectionMenu(sampleMenu())); strings.Contains(h, "cui-section-menu--fill") {
		t.Errorf("a default menu must not carry the fill marker:\n%s", h)
	}
	cfg := sampleMenu()
	cfg.FillColumn = true
	if h := string(SectionMenu(cfg)); !strings.Contains(h, `class="cui-section-menu cui-section-menu--fill"`) {
		t.Errorf("FillColumn should mark the root:\n%s", h)
	}
}

func TestSectionMenuUnknownSwapAtPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("SwapAt \"xl\" rendered; want a panic naming the choices")
		}
	}()
	cfg := sampleMenu()
	cfg.SwapAt = "xl"
	SectionMenu(cfg)
}

// The pill names the page the reader is on and its group, so it reads
// as a location rather than a button labelled "Sections"; its accessible
// name still starts with the trigger label.
func TestSectionMenuTriggerNamesCurrentPage(t *testing.T) {
	h := string(SectionMenu(sampleMenu()))
	for _, want := range []string{
		`<span class="cui-sheet-pill__sr">Sections: </span>`,
		`<span class="cui-sheet-pill__label">Entities</span>`,
		`<span class="cui-sheet-pill__detail">Modeling</span>`,
		`aria-haspopup="dialog"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("trigger missing %q\n%s", want, h)
		}
	}
	cfg := sampleMenu()
	cfg.Groups[0].Items[0].Active = false
	h = string(SectionMenu(cfg))
	if !strings.Contains(h, `<span class="cui-sheet-pill__label">Sections</span><span class="cui-sheet-pill__detail"></span>`) {
		t.Errorf("with no current page the pill should show the trigger label alone:\n%s", h)
	}
}

func TestSectionMenuGroupShowsCount(t *testing.T) {
	h := string(SectionMenu(sampleMenu()))
	if !strings.Contains(h, `cui-section-menu__group-label">Modeling</span><span class="cui-section-menu__count">2</span>`) {
		t.Errorf("group header should carry its item count:\n%s", h)
	}
}

// Filter puts the box in the rail and the sheet, with a hidden empty
// line the behaviour reveals; a menu without it renders neither.
func TestSectionMenuFilterRendersBox(t *testing.T) {
	cfg := sampleMenu()
	if h := string(SectionMenu(cfg)); strings.Contains(h, "cui-section-menu__filter") || strings.Contains(h, "cui-section-menu__empty") {
		t.Errorf("a menu without Filter should render no filter:\n%s", h)
	}
	cfg.Filter = true
	cfg.FilterPlaceholder = "Filter 3 docs"
	for name, h := range map[string]string{
		"rail":  string(SectionMenu(cfg)),
		"sheet": string(sectionMenuDrawerSlot{cfg: cfg}.Render()),
	} {
		for _, want := range []string{
			`type="search"`,
			`placeholder="Filter 3 docs"`,
			`aria-label="Filter 3 docs"`,
			`<p class="cui-section-menu__empty" hidden="" role="status">No matches</p>`,
		} {
			if !strings.Contains(h, want) {
				t.Errorf("%s: missing %q\n%s", name, want, h)
			}
		}
	}
	if h := string(sectionMenuDrawerSlot{cfg: cfg}.Render()); !strings.Contains(h, "cui-section-menu--filter") {
		t.Errorf("sheet with a filter should mark its root so the headers pin under the taller top bar:\n%s", h)
	}
}

func TestSectionMenuInlineTriggerMarksPill(t *testing.T) {
	if h := string(SectionMenu(sampleMenu())); strings.Contains(h, "cui-sheet-pill--inline") {
		t.Errorf("the default pill floats; no inline marker expected:\n%s", h)
	}
	cfg := sampleMenu()
	cfg.InlineTrigger = true
	if h := string(SectionMenu(cfg)); !strings.Contains(h, "cui-sheet-pill--inline") {
		t.Errorf("InlineTrigger should mark the pill:\n%s", h)
	}
}
