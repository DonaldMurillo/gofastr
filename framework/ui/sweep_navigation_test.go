package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestSweepNavigationSidebarMatchPathSegmentBoundary(t *testing.T) {
	draw := func(current string) string {
		return string(Sidebar(SidebarConfig{CurrentPath: current, Items: []SidebarItem{
			{Label: "Customers", Href: "/customers", MatchPath: "/customers"},
		}}).Render())
	}
	if out := draw("/customers-archive"); strings.Contains(out, `aria-current="page"`) {
		t.Errorf("MatchPath /customers lit /customers-archive:\n%s", out)
	}
	if out := draw("/customers/42"); !strings.Contains(out, `aria-current="page"`) {
		t.Errorf("MatchPath /customers left /customers/42 dark:\n%s", out)
	}
}

func TestSweepNavigationSidebarActivePrecedence(t *testing.T) {
	lit := func(current string, it SidebarItem) bool {
		out := string(Sidebar(SidebarConfig{CurrentPath: current, Items: []SidebarItem{it}}).Render())
		return strings.Contains(out, `aria-current="page"`)
	}
	if !lit("/elsewhere", SidebarItem{Label: "A", Href: "/a", Active: true}) {
		t.Error("Active did not force the item current")
	}
	if lit("/a", SidebarItem{Label: "A", Href: "/a", MatchPath: "/b"}) {
		t.Error("an exact Href match lit an item whose MatchPath names another section")
	}
	if !lit("/a", SidebarItem{Label: "A", Href: "/a"}) {
		t.Error("an exact Href match left the item dark")
	}
	group := string(Sidebar(SidebarConfig{CurrentPath: "/s/deep/x", Items: []SidebarItem{
		{Label: "S", Children: []SidebarItem{{Label: "Deep", Children: []SidebarItem{{Label: "X", Href: "/s/deep/x"}}}}},
	}}).Render())
	if strings.Count(group, ` open=""`) != 2 {
		t.Errorf("both ancestors of the current link must render open:\n%s", group)
	}
}

func TestSweepNavigationSidebarBadBreakpointPanics(t *testing.T) {
	items := []SidebarItem{{Label: "Home", Href: "/"}}
	wantPanic(t, "Sidebar unknown StackBreakpoint", func() {
		_ = Sidebar(SidebarConfig{Items: items, DrawerBreakpoint: "lge"}).Render()
	})
	wantPanic(t, "SidebarDrawerTrigger unknown StackBreakpoint", func() {
		_ = SidebarDrawerTrigger(SidebarConfig{DrawerBreakpoint: "lge"})
	})
}

func TestSweepNavigationSidebarCollapseOnlyOnCollapsible(t *testing.T) {
	items := []SidebarItem{{Label: "Home", Href: "/"}}
	for _, v := range []SidebarVariant{SidebarPersistent, SidebarOffCanvas, SidebarAutoHide} {
		out := string(Sidebar(SidebarConfig{Variant: v, Items: items, Collapse: SidebarCollapseCollapsed}).Render())
		if strings.Contains(out, "data-collapsed") || strings.Contains(out, "data-hui-sidebar-collapse=") {
			t.Errorf("variant %q carries collapse state:\n%s", v, out)
		}
	}
	out := string(Sidebar(SidebarConfig{Variant: SidebarCollapsible, Items: items, Collapse: SidebarCollapseCollapsed}).Render())
	if !strings.Contains(out, `data-collapsed="true"`) || strings.Contains(out, "gofastr.sidebar.") {
		t.Errorf("a server-collapsed sidebar must ship the rail state and no storage key:\n%s", out)
	}
}

func TestSweepNavigationMountSidebarFuncKeepsMountIdentity(t *testing.T) {
	func() {
		defer func() {
			if r := recover(); !strings.Contains(fmt.Sprint(r), "build is nil") {
				t.Errorf("MountSidebarFunc(nil) panicked with %v, want the nil-build message", r)
			}
		}()
		MountSidebarFunc(&mountRecorder{}, nil)
	}()
	type reqKey struct{}
	build := func(ctx context.Context) SidebarConfig {
		cfg := SidebarConfig{DrawerName: "boot", GroupMarkup: SidebarGroupButton, Items: []SidebarItem{
			{Label: "G", Children: []SidebarItem{{Label: "X", Href: "/x"}}},
		}}
		if ctx.Value(reqKey{}) != nil {
			cfg.DrawerName = "request"
		}
		return cfg
	}
	def := MountSidebarFunc(&mountRecorder{}, build)
	chrome := widget.RenderChromeCtx(context.WithValue(context.Background(), reqKey{}, true), &def)
	if !strings.Contains(chrome, `id="boot-drawer-g1"`) || strings.Contains(chrome, "request-drawer") {
		t.Errorf("the per-request config replaced the drawer's mount-time DrawerName:\n%s", chrome)
	}
}

func TestSweepNavigationSignOutRefusesScriptAction(t *testing.T) {
	out := string(SignOut(SignOutConfig{Action: "javascript:alert(1)"}))
	if strings.Contains(out, "javascript:") || !strings.Contains(out, `action="/auth/logout"`) {
		t.Errorf("a script action must fall back to the logout endpoint:\n%s", out)
	}
}

func TestSweepNavigationDrawerBarCopyTargetPerPath(t *testing.T) {
	wantPanic(t, "DrawerBar without Path", func() { DrawerBar(DrawerBarConfig{}) })
	id := func(path string) string {
		out := string(DrawerBar(DrawerBarConfig{Path: path, CopyURL: "https://x" + path}))
		_, rest, _ := strings.Cut(out, `id="fui-drawer-url-`)
		v, _, _ := strings.Cut(rest, `"`)
		if v == "" {
			t.Fatalf("no copy target id:\n%s", out)
		}
		return v
	}
	if id("/r/1") != id("/r/1") || id("/r/1") == id("/r/2") {
		t.Error("the copy target id must be a function of the layer's path")
	}
}

func TestSweepNavigationSidebarDrawerHeaderCarriesMarker(t *testing.T) {
	out := string(sidebarDrawerHeader{title: "Acme", labelID: "nav-label"}.Render())
	if !strings.Contains(out, `data-cui-comp="ui-sidebar"`) {
		t.Errorf("the drawer header must link the sidebar sheet on its own:\n%s", out)
	}
}

func TestSweepNavigationSidebarDrawerBreakpointReachesEverySite(t *testing.T) {
	cfg := SidebarConfig{DrawerBreakpoint: StackBelowLG, NativeMobile: true, Items: []SidebarItem{{Label: "Home", Href: "/"}}}
	out := string(Sidebar(cfg).Render())
	for _, want := range []string{"fui-sidebar--drawer-below-lg", "fui-sidebar__hamburger--drawer-below-lg", "fui-sidebar-native--drawer-below-lg"} {
		if !strings.Contains(out, want) {
			t.Errorf("StackBelowLG sidebar lacks %s:\n%s", want, out)
		}
	}
	if trigger := string(SidebarDrawerTrigger(cfg)); !strings.Contains(trigger, "fui-sidebar__hamburger--drawer-below-lg") {
		t.Errorf("the standalone trigger ignores DrawerBreakpoint:\n%s", trigger)
	}
}

func TestSweepNavigationBackToTopDefaults(t *testing.T) {
	out := string(BackToTop(BackToTopConfig{}))
	if !strings.Contains(out, `data-hui-back-to-top-threshold="400"`) || !strings.Contains(out, "data-hui-back-to-top-smooth") {
		t.Errorf("BackToTop defaults to a 400px threshold and smooth scrolling:\n%s", out)
	}
	if out := string(BackToTop(BackToTopConfig{Smooth: BackToTopInstant})); strings.Contains(out, "data-hui-back-to-top-smooth") {
		t.Errorf("BackToTopInstant still scrolls smoothly:\n%s", out)
	}
}

func TestSweepNavigationDrawerBarHistoryHooks(t *testing.T) {
	out := string(DrawerBar(DrawerBarConfig{Path: "/r/1", PageURL: "/r/1", Actions: []render.HTML{render.HTML(`<b id="act">x</b>`)}}))
	closeAt, pageAt := strings.Index(out, `data-cui-intercept-close`), strings.Index(out, `data-cui-intercept-page`)
	if closeAt < 0 || pageAt < 0 {
		t.Fatalf("the close and open-as-page controls must carry the intercept hooks:\n%s", out)
	}
	if actAt := strings.Index(out, `<b id="act">`); actAt < pageAt || strings.Contains(out[actAt:], "data-cui-internal") {
		t.Errorf("Actions trail the bar's own controls and stay the caller's:\n%s", out)
	}
}

func TestSweepNavigationLanguageMenuNilCtxRendersNothing(t *testing.T) {
	if out := LanguageMenu(LanguageMenuConfig{}); out != "" {
		t.Errorf("a LanguageMenu with no Ctx rendered %q", out)
	}
}

func TestSweepNavigationStepRailTitleIsNotAHeading(t *testing.T) {
	out := string(StepRail(StepRailConfig{Title: "The path", ActiveIndex: -1, Items: []StepRailItem{{Number: "01", Anchor: "a", Label: "A"}}}))
	if strings.Contains(out, "<h") || !strings.Contains(out, `aria-label="The path"`) {
		t.Errorf("the rail is named by Title and adds no heading to the outline:\n%s", out)
	}
}

func TestSweepNavigationMenuAvatarRefusesOtherTriggers(t *testing.T) {
	items := []MenuItem{{Label: "Profile", Href: "/me"}}
	av := &AvatarConfig{Name: "Ada"}
	for name, cfg := range map[string]MenuConfig{
		"html":     {Label: "Account", Avatar: av, TriggerHTML: "<b>x</b>", Items: items},
		"element":  {Label: "Account", Avatar: av, TriggerElement: `<button type="button">x</button>`, Items: items},
		"icononly": {Label: "Account", Avatar: av, IconOnly: true, Items: items},
	} {
		func() {
			defer func() {
				if r := recover(); !strings.Contains(fmt.Sprint(r), "Menu Avatar") {
					t.Errorf("%s: Avatar beside another trigger panicked with %v", name, r)
				}
			}()
			Menu(cfg)
		}()
	}
}

func TestSweepNavigationSidebarSheetIsPrewarmed(t *testing.T) {
	e, ok := registry.Lookup("ui-sidebar")
	if !ok || e.Load != registry.LoadPrewarm {
		t.Errorf("ui-sidebar must load on demand with an idle prewarm, got %+v", e)
	}
}

func TestSweepNavigationSidebarNativeMobileGroupIDs(t *testing.T) {
	out := string(Sidebar(SidebarConfig{NativeMobile: true, GroupMarkup: SidebarGroupButton, Items: []SidebarItem{
		{Label: "G", Children: []SidebarItem{{Label: "C", Href: "/c"}}},
	}}).Render())
	if !strings.Contains(out, `id="ui-sidebar-drawer-inline-g1"`) || !strings.Contains(out, `id="ui-sidebar-drawer-mobile-g1"`) {
		t.Errorf("the inline column and the no-script disclosure need distinct group ids:\n%s", out)
	}
}

func TestSweepNavigationCompactTriggerShowsNavLabel(t *testing.T) {
	cfg := SidebarConfig{Compact: true, NavLabel: "Docs", Items: []SidebarItem{{Label: "Home", Href: "/"}}}
	for name, out := range map[string]string{"shell": string(Sidebar(cfg).Render()), "standalone": string(SidebarDrawerTrigger(cfg))} {
		_, button, _ := strings.Cut(out, "<button")
		button, _, _ = strings.Cut(button, ">")
		if !strings.Contains(button, `aria-label="Docs"`) || !strings.Contains(button, "fui-sidebar__hamburger--labelled") {
			t.Errorf("%s: a compact sidebar's trigger must carry NavLabel:\n%s", name, out)
		}
	}
}
