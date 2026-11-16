package headless

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"testing"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestSweepNavigationTagHrefPassesTheAnchorPolicy(t *testing.T) {
	refuse(t, "anchor policy", func() { Tag(TagProps{Label: "x", Href: "javascript:alert(1)"}, nil) })
	got := Tag(TagProps{Label: "env=prod", Href: "/apps?env=prod"}, nil)
	has(t, got, `<a href="/apps?env=prod">`, "a filter link tag is not an anchor")
}

func TestSweepNavigationPaginationDisabledEndLeavesTheTabOrder(t *testing.T) {
	got := string(Pagination(PaginationProps{Page: 1, Pages: 3, Path: "/apps", AriaLabel: "Pages",
		Island: fixtureIsland}, nil))
	prev := regexp.MustCompile(`<a [^>]*>Previous</a>`).FindString(got)
	if prev != `<a aria-disabled="true" tabindex="-1">Previous</a>` {
		t.Fatalf("the first page's Previous is not a disabled anchor out of the tab order: %s", prev)
	}
}

func TestSweepNavigationMenuAutoIDFoldsTheRows(t *testing.T) {
	a := renderMenu(MenuProps{Label: "Menu", Items: []MenuItem{{Label: "One"}}})
	b := renderMenu(MenuProps{Label: "Menu", Items: []MenuItem{{Label: "Two"}}})
	nested := renderMenu(MenuProps{Label: "Menu", Items: []MenuItem{{Label: "One", Children: []MenuItem{{Label: "In"}}}}})
	id := regexp.MustCompile(`data-hui-menu="(hui-menu-[0-9a-f]{8})"`)
	ia, ib, in := id.FindStringSubmatch(a), id.FindStringSubmatch(b), id.FindStringSubmatch(nested)
	if ia == nil || ib == nil || in == nil {
		t.Fatalf("a menu with no ID derived none:\n%s\n%s", a, b)
	}
	if ia[1] == ib[1] || ia[1] == in[1] {
		t.Fatalf("menus differing only in their rows share the derived id %s", ia[1])
	}
}

func TestSweepNavigationMenuDisabledButtonRowIsDisabled(t *testing.T) {
	h := renderMenu(MenuProps{Label: "M", Items: []MenuItem{{Label: "Purge", RPC: "/api/purge", Disabled: true}}})
	btn := regexp.MustCompile(`<button[^>]*>`).FindString(h)
	if !strings.Contains(btn, ` disabled=""`) || !strings.Contains(btn, `aria-disabled="true"`) {
		t.Fatalf("a disabled button row can still be activated: %s", btn)
	}
}

func TestSweepNavigationMenuFormRowConfirmsOnItsSubmit(t *testing.T) {
	h := renderMenu(MenuProps{Label: "M", Items: []MenuItem{{Label: "Stop", Confirm: "Really stop?",
		Action: &MenuAction{Path: "/stop", Fields: map[string]string{"csrf": "t"}}}}})
	btn := regexp.MustCompile(`<button[^>]*type="submit"[^>]*>`).FindString(h)
	if !strings.Contains(btn, `data-cui-confirm="Really stop?"`) {
		t.Fatalf("a destructive form row posts without asking: %s", btn)
	}
}

func TestSweepNavigationMenuUnsafeHrefDegrades(t *testing.T) {
	h := renderMenu(MenuProps{Label: "M", Items: []MenuItem{{Label: "Evil", Href: "javascript:alert(1)"}}})
	if strings.Contains(h, "javascript:") || !strings.Contains(h, `href="#"`) {
		t.Fatalf("an unsafe row href did not degrade to #:\n%s", h)
	}
}

func TestSweepNavigationMenuRPCIsSameOrigin(t *testing.T) {
	for _, rpc := range []string{"//evil.example/x", "/\\evil.example/x", "https://evil.example/x"} {
		refuse(t, "same-origin", func() {
			Menu(MenuProps{Label: "M", Items: []MenuItem{{Label: "Go", RPC: rpc}}}, nil)
		})
	}
}

func TestSweepNavigationMenuSubmenuRowReadsItsLabel(t *testing.T) {
	h := renderMenu(MenuProps{ID: "m", Label: "M", Items: []MenuItem{{Label: "Palette", Children: []MenuItem{{Label: "Light"}}}}})
	row := regexp.MustCompile(`<summary aria-controls="m-panel-sub-0-panel"[^>]*>(.*?)</summary>`).FindStringSubmatch(h)
	if row == nil {
		t.Fatalf("no submenu row:\n%s", h)
	}
	if text := regexp.MustCompile(`<[^>]*>`).ReplaceAllString(row[1], ""); text != "Palette" {
		t.Fatalf("the submenu row reads %q, not its label alone: type-ahead would miss it", text)
	}
}

func TestSweepNavigationBehaviorsDeclareMarkersAndRequirements(t *testing.T) {
	for _, tc := range []struct {
		name              string
		markers, requires []string
	}{
		{MenuBehaviorName, []string{"[data-hui-menu]", "[data-hui-menu-trigger]"}, []string{DisclosureBehaviorName, "widgets"}},
		{RailBehaviorName, []string{"[data-hui-rail]"}, nil},
		{TOCBehaviorName, []string{"[data-hui-toc]"}, []string{RailBehaviorName}},
		{SidebarBehaviorName, []string{"[data-hui-sidebar]", "[data-hui-sidebar-group-toggle]"}, []string{"widgets"}},
		{TabsBehaviorName, []string{"[data-hui-tabs]"}, nil},
		{PaneHostBehaviorName, []string{"[data-hui-panehost]"}, nil},
	} {
		e, ok := uiregistry.LookupBehavior(tc.name)
		if !ok {
			t.Fatalf("%s is not registered", tc.name)
		}
		if !slices.Equal(e.Markers, tc.markers) {
			t.Errorf("%s registers markers %v, want %v", tc.name, e.Markers, tc.markers)
		}
		if !slices.Equal(e.Requires, tc.requires) {
			t.Errorf("%s requires %v, want %v", tc.name, e.Requires, tc.requires)
		}
	}
}

func TestSweepNavigationStaticRailLoadsNoModule(t *testing.T) {
	static := renderRail(RailProps{Label: "L", TargetSelector: "section[id]", Items: []RailItem{{Anchor: "a", Text: "A"}}})
	if strings.Contains(static, "data-hui-rail") {
		t.Fatalf("a rail with no ObserveSelector carries a hook that loads the module:\n%s", static)
	}
	observed := renderRail(RailProps{Label: "L", ObserveSelector: "main", Items: []RailItem{{Anchor: "a", Text: "A"}}})
	if !strings.Contains(observed, `data-hui-rail=""`) {
		t.Fatalf("an observed rail lost its marker:\n%s", observed)
	}
}

func TestSweepNavigationTOCLevelZeroIsTwo(t *testing.T) {
	classes := Classes{PartTOCItem: "item", Part(string(PartTOCItem) + "--h2"): "item--h2"}
	h := string(TableOfContents(TableOfContentsProps{Items: []TOCItem{{ID: "a", Label: "A"}}}, classes))
	if !strings.Contains(h, `class="item item--h2"`) {
		t.Fatalf("a level-0 entry did not take the h2 variant:\n%s", h)
	}
}

func TestSweepNavigationDisabledTabNeitherSelectsNorLinks(t *testing.T) {
	h := renderTabs(TabsProps{Name: "cfg", Tabs: []Tab{
		{Label: "General", Panel: render.Text("g")},
		{Label: "Secrets", Panel: render.Text("s"), Href: "/cfg/secrets", Disabled: true},
	}})
	tab := regexp.MustCompile(`<a [^>]*id="cfg-tab-1"[^>]*>`).FindString(h)
	if !strings.Contains(tab, `aria-disabled="true"`) || strings.Contains(tab, "href=") || strings.Contains(tab, "data-cui-signal-set") {
		t.Fatalf("a disabled tab still selects or links: %s", tab)
	}
}

func TestSweepNavigationTabsStashCannotCloseItsScript(t *testing.T) {
	h := renderTabs(TabsProps{Name: "v", VacateHidden: true, Tabs: []Tab{
		{Label: "A", Panel: render.Text("a")},
		{Label: "B", Panel: render.HTML(`</script><script>alert(1)</script>`)},
	}})
	i := strings.Index(h, `type="application/json">`)
	if i < 0 {
		t.Fatalf("no stash:\n%s", h)
	}
	body := h[i+len(`type="application/json">`):]
	body = body[:strings.Index(body, "</script>")]
	if !strings.Contains(body, "alert(1)") {
		t.Fatalf("the vacated panel's markup closed the stash script early:\n%s", h)
	}
}

func TestSweepNavigationPaneHostNamesEveryOpenPane(t *testing.T) {
	h := renderPaneHost(PaneHostProps{Primary: render.Text("p"),
		Secondary: render.Text("s"), SecondaryOpen: true,
		Tertiary: render.Text("t"), TertiaryOpen: true})
	if !strings.Contains(h, `data-hui-pane-open="secondary tertiary"`) {
		t.Fatalf("the open list does not name both open panes in slot order:\n%s", h)
	}
	only := renderPaneHost(PaneHostProps{Primary: render.Text("p"), Tertiary: render.Text("t"), TertiaryOpen: true})
	if !strings.Contains(only, `data-hui-pane-open="tertiary"`) {
		t.Fatalf("an open tertiary pane alone is not named:\n%s", only)
	}
}

func TestSweepNavigationPaneHostDeepLinkIsAToken(t *testing.T) {
	refuse(t, "bare token", func() {
		PaneHost(PaneHostProps{Primary: render.Text("p"), DeepLinkParam: "pane id"}, nil)
	})
}

func TestSweepNavigationSidebarStorageHookNeedsAKey(t *testing.T) {
	h := renderSidebar(SidebarProps{NavLabel: "N", Variant: "collapsible", Collapse: "auto",
		Items: []SidebarItem{{Label: "One", Href: "/1"}}})
	if !strings.Contains(h, `data-hui-sidebar-collapse="auto"`) || strings.Contains(h, "data-hui-sidebar-storage") {
		t.Fatalf("an auto sidebar with no CollapseStorageKey carries a storage hook:\n%s", h)
	}
}

func TestSweepNavigationSidebarCollapseLabelsFallBackToNavLabel(t *testing.T) {
	h := renderSidebar(SidebarProps{NavLabel: "Primary", Variant: "collapsible",
		Items: []SidebarItem{{Label: "One", Href: "/1"}}})
	if !strings.Contains(h, `aria-label="Primary" data-hui-sidebar-collapse-label="Primary" data-hui-sidebar-expand-label="Primary"`) {
		t.Fatalf("the collapse toggle is not named by NavLabel without its own labels:\n%s", h)
	}
}

func TestSweepNavigationTabsExplicitIDReservesItsFamily(t *testing.T) {
	ctx := render.WithIDScope(context.Background())
	tabs := []Tab{{Label: "A", Panel: render.Text("a")}}
	first := renderTabs(TabsProps{Name: "a", ID: "cfg", Tabs: tabs, Ctx: ctx})
	second := renderTabs(TabsProps{Name: "cfg", Tabs: tabs, Ctx: ctx})
	if !strings.Contains(first, `id="cfg-tab-0"`) || !strings.Contains(second, `id="cfg-2-tab-0"`) {
		t.Fatalf("a derived strip id repeats an explicit one:\n%s\n%s", first, second)
	}
}

func TestSweepNavigationBreadcrumbRefusedHrefIsText(t *testing.T) {
	h := renderCrumbs(BreadcrumbsProps{Items: []Breadcrumb{
		{Text: "Home", Href: "/"}, {Text: "Evil", Href: "javascript:alert(1)"}, {Text: "End"},
	}})
	if !strings.Contains(h, `<span>Evil</span>`) {
		t.Fatalf("a refused breadcrumb href did not degrade to plain text:\n%s", h)
	}
}

func TestSweepNavigationSidebarToggleMarksItselfBesideCallerContent(t *testing.T) {
	h := renderSidebar(SidebarProps{NavLabel: "N", Variant: "collapsible",
		Items: []SidebarItem{{Label: "One", Href: "/1", Icon: render.Text("*")}}})
	toggle := regexp.MustCompile(`<button[^>]*data-hui-sidebar-toggle[^>]*>`).FindString(h)
	if !strings.Contains(toggle, `data-cui-internal=""`) {
		t.Fatalf("the collapse toggle beside a caller icon is not marked as the component's own: %s", toggle)
	}
}
