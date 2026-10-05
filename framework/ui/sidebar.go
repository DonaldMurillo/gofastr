package ui

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── Sidebar ────────────────────────────────────────────────────────
//
// The navigation column, rendered through headless.Sidebar with the
// fui-sidebar class map (the sheet name "ui-sidebar" and marker stay).
// The shell (drawer trigger, collapse toggle, the inline column), the
// two group dialects and the item tree are the primitive's; this
// adapter owns the config surface: role filtering, the active-path
// rules, the typed enums, the resolved strings and the storage-key
// default.

// SidebarVariant selects how the sidebar behaves at ≥ md viewports.
//
//	SidebarPersistent: fixed-width column, always visible.
//	SidebarCollapsible: column with a chevron that toggles a compact
//	  rail; expanded/collapsed state persists in localStorage.
//	SidebarOffCanvas: hidden by default. Opens via the hamburger
//	  trigger on every viewport (no inline column).
//	SidebarAutoHide: like persistent, but at >= md the column rests
//	  as a 64px icon rail and reveals the full column on :hover or
//	  :focus-within (keyboard). Pure CSS from the component's own
//	  stylesheet; no JavaScript.
//
// On `< md` every variant collapses to a hamburger + drawer.
type SidebarVariant string

const (
	SidebarPersistent  SidebarVariant = "persistent"
	SidebarCollapsible SidebarVariant = "collapsible"
	SidebarOffCanvas   SidebarVariant = "off-canvas"
	SidebarAutoHide    SidebarVariant = "auto-hide"
)

// checkSidebarVariant panics on a variant outside the built-in set.
// Matches the Card/Button contract: every variant-taking component
// validates at render so a typo'd variant is loud, not silently
// unstyled. Empty is allowed (the documented default: Sidebar()
// normalizes it to SidebarPersistent).
func checkSidebarVariant(v SidebarVariant) {
	switch v {
	case "", SidebarPersistent, SidebarCollapsible, SidebarOffCanvas, SidebarAutoHide:
	default:
		panic("ui: Sidebar unknown Variant " + string(v) +
			`. Pick one of: "" (persistent), "collapsible", "off-canvas", "auto-hide"`)
	}
}

// SidebarCollapse selects who owns the collapsed state of the
// collapsible variant: the server (per-request, e.g. restored from the
// signed-in user's stored preference) or the runtime (localStorage).
//
//	SidebarCollapseAuto: zero value. The runtime owns the state and
//	  restores it from localStorage after hydration — the default
//	  behaviour since the component exists.
//	SidebarCollapseCollapsed: the server renders the collapsed rail
//	  (data-collapsed="true" on the root). The runtime neither reads
//	  nor writes localStorage: a stale local value cannot overwrite
//	  the server's state on first paint or after a toggle.
//	SidebarCollapseExpanded: the server renders the expanded column
//	  (data-collapsed="false"). Same localStorage suppression.
//
// This mirrors CurrentPath's split: when set, the server decides and
// the state ships in the SSR bytes; when empty, the runtime decides
// after hydration. Only meaningful with Variant: SidebarCollapsible.
type SidebarCollapse string

const (
	SidebarCollapseAuto      SidebarCollapse = ""
	SidebarCollapseCollapsed SidebarCollapse = "collapsed"
	SidebarCollapseExpanded  SidebarCollapse = "expanded"
)

// checkSidebarCollapse panics on a collapse owner outside the built-in
// set, for the same reason as checkSidebarVariant: a typo'd
// "collpased" would silently fall back to Auto and the user's stored
// preference would never render.
func checkSidebarCollapse(c SidebarCollapse) {
	switch c {
	case SidebarCollapseAuto, SidebarCollapseCollapsed, SidebarCollapseExpanded:
	default:
		panic("ui: Sidebar unknown Collapse " + string(c) +
			`. Pick one of: "" (auto), "collapsed", "expanded"`)
	}
}

// SidebarGroupMarkup selects the markup dialect used for groups
// (items with Children).
//
//	SidebarGroupDetails: zero value. <details data-hui-disclosure
//	  data-hui-disclosure-persist><summary>, the default since the
//	  component exists.
//	SidebarGroupButton: a <button type="button" aria-expanded
//	  aria-controls data-hui-sidebar-group-toggle> header plus the
//	  child links in a container that carries the hidden attribute
//	  when closed. For hosts whose contract pins that shape for
//	  keyboard/AT parity with the rest of their app; the sidebar
//	  runtime module toggles aria-expanded and hidden on click.
//
// Two details-dialect behaviours do not carry over to the button
// dialect: group open state is not persisted across navigation (the
// details dialect carries data-hui-disclosure-persist; every swap
// resets a button-dialect group to its server-rendered state), and
// the dialect needs JavaScript — <details> opens natively without
// it, but without the runtime module a closed button-dialect group's
// links are unreachable.
type SidebarGroupMarkup string

const (
	SidebarGroupDetails SidebarGroupMarkup = ""
	SidebarGroupButton  SidebarGroupMarkup = "button"
)

// checkSidebarGroupMarkup panics on a dialect outside the built-in
// set. Called from every render path (inline sidebar, drawer body,
// SidebarBody).
func checkSidebarGroupMarkup(m SidebarGroupMarkup) {
	switch m {
	case SidebarGroupDetails, SidebarGroupButton:
	default:
		panic("ui: Sidebar unknown GroupMarkup " + string(m) +
			`. Pick one of: "" (details), "button"`)
	}
}

// StackBreakpoint picks the viewport width a responsive piece switches
// its posture at: below md (48rem — phones; the default) or below lg
// (64rem — phones and tablets, leaving the wide form to large
// screens). SidebarConfig.DrawerBreakpoint and ContentRowConfig
// .Breakpoint share it so a page's sidebar and its content row collapse
// at the same width.
type StackBreakpoint string

const (
	// StackBelowMD is the zero value: switch below md (48rem).
	StackBelowMD StackBreakpoint = ""
	// StackBelowLG switches below lg (64rem): tablets join phones in
	// the collapsed form.
	StackBelowLG StackBreakpoint = "lg"
)

// checkStackBreakpoint panics on a value outside the built-in set, the
// same contract as checkSidebarVariant: a typo'd "lge" would silently
// fall back to md and the column would collapse one step too early.
func checkStackBreakpoint(b StackBreakpoint) {
	switch b {
	case StackBelowMD, StackBelowLG:
	default:
		panic("ui: unknown StackBreakpoint " + string(b) + `. Pick one of: "" (below md), "lg" (below lg)`)
	}
}

// SidebarItem is one navigation entry. Children nest one level deep.
// Deeper nesting is unsupported by design. Sidebars should not be
// trees.
type SidebarItem struct {
	Label    string
	Href     string
	Icon     render.HTML
	Children []SidebarItem

	// Roles, when non-empty, restricts the item to users holding at least
	// one of the named roles. Empty = visible to everyone. Filtering happens
	// at render time via the roles extractor (SetRolesExtractor); when no
	// extractor is registered, items render unfiltered (opt-in feature).
	Roles []string

	// Active forces the item into the active state regardless of the
	// caller's MatchPath. Useful for pages that don't map 1:1 to a URL.
	Active bool

	// MatchPath, when set, overrides the default "current URL equals
	// Href" check used to mark the item as active. Pass a section
	// prefix ("/customers") to highlight on the section root and its
	// sub-paths; the match is on the path-segment boundary, so
	// "/customers" does not light up "/customers-archive". For anything
	// non-trivial, use CurrentPath in your screen and set Active
	// manually.
	MatchPath string

	// Open forces a group to render expanded on first paint regardless
	// of active-state rules — for hosts whose contract pins certain
	// sections open by default (metacollector's My Inventory group).
	// Leaf items ignore it.
	Open bool
}

// SidebarConfig describes a navigation sidebar.
type SidebarConfig struct {
	// Title is rendered as the sidebar's top heading. Empty omits it.
	Title string

	// NavLabel names the navigation landmark. Defaults to "Primary".
	// Set a distinct label when more than one navigation landmark appears.
	NavLabel string

	// Items is the navigation tree. With no items, or none left after
	// the role filter, the sidebar, SidebarBody and the drawer body
	// render nothing rather than an empty navigation landmark.
	Items []SidebarItem

	// CurrentPath is the screen's current path, used for active-state
	// highlighting. When empty, falls back to JS: the runtime stamps
	// aria-current on any matching <a> after hydration.
	CurrentPath string

	// Variant defaults to SidebarPersistent.
	Variant SidebarVariant
	// Compact is a documentation rail: tighter links and a thin current marker.
	// On phones its full-width trigger shows NavLabel and links keep 44px targets.
	Compact bool

	// DrawerBreakpoint picks the viewport width below which the
	// sidebar collapses to its hamburger drawer instead of the inline
	// column. The default (StackBelowMD, 48rem) serves phones; below
	// lg (StackBelowLG, 64rem) collapses the column on tablets too.
	// Every place the component encodes the switch follows it: the
	// inline column's hiding, the drawer trigger's self-hiding, and
	// the NativeMobile no-script disclosure.
	DrawerBreakpoint StackBreakpoint

	// Collapse decides who owns the collapsed state when Variant is
	// SidebarCollapsible. Zero (SidebarCollapseAuto) keeps the
	// localStorage-driven behaviour. Collapsed/Expanded make the server
	// own the state: the collapsed rail (or expanded column) ships in
	// the SSR bytes and the runtime never reads or writes the
	// localStorage key. Use it for per-user preferences restored from
	// the database that must survive first paint on any device.
	Collapse SidebarCollapse

	// GroupMarkup selects the dialect used for items with Children.
	// Zero (SidebarGroupDetails) renders <details><summary>.
	// SidebarGroupButton renders button[aria-expanded][aria-controls]
	// plus a hidden-when-closed container of the child links.
	GroupMarkup SidebarGroupMarkup

	// Prepend is an optional component rendered between the title and
	// the nav, on every body path: the inline sidebar, SidebarBody, and
	// the MountSidebar drawer. Use it for a section switcher that the
	// phone drawer must carry because the header hides it there. It is
	// a component, not HTML, because MountSidebar runs once at boot and
	// the drawer body renders per request: a Prepend that implements
	// component.ContextComponent sees the request (current section,
	// signed-in user) on the inline and drawer paths alike. Wrap static
	// markup in app.NewStaticComponent. Hidden with the title in the
	// collapsed rail and the auto-hide rest state. Nil emits no markup.
	Prepend component.Component

	// Footer is optional content rendered at the bottom (signed-in
	// user pill, settings link, etc.).
	Footer render.HTML

	// DrawerTitle is the brand text the < md drawer's header row
	// shows. Falls back to Title when empty; when both are empty the
	// drawer renders no header (just the nav, as before). Set it to
	// the same brand the top bar shows when the layout does not
	// expose one.
	DrawerTitle string

	// DrawerName overrides the widget name used for the < md drawer.
	// Defaults to "ui-sidebar-drawer". Apps that host multiple
	// sidebars per page must override to avoid collisions.
	DrawerName string

	// CollapseStorageKey overrides the localStorage key used by the
	// collapsible variant. Defaults to "gofastr.sidebar.<DrawerName>.collapsed".
	// Ignored when Collapse is Collapsed/Expanded: server-owned state
	// never touches localStorage.
	CollapseStorageKey string

	// CollapseLabel is the collapse button's aria-label when the
	// sidebar is expanded. Defaults to "Collapse navigation".
	CollapseLabel string

	// ExpandLabel is the collapse button's aria-label when the sidebar
	// is collapsed (the same button expands the rail). Defaults to
	// "Expand navigation". Both labels are also emitted as
	// data-hui-sidebar-collapse-label / data-hui-sidebar-expand-label
	// so the runtime keeps using them after a client-side toggle.
	ExpandLabel string

	// SuppressDrawerTrigger hides the hamburger button rendered by
	// Sidebar (some apps put their hamburger in the page header
	// instead and call MountSidebar themselves).
	SuppressDrawerTrigger bool

	// NativeMobile adds a native disclosure when scripting is disabled.
	// Scripted browsers keep the mounted drawer and its keyboard behavior.
	NativeMobile bool

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers) to the sidebar's root element. Keys the
	// component owns are dropped: class and id, aria-label (the
	// navigation landmark's name comes from NavLabel), data-collapsed,
	// and every data-cui-*/data-hui-* wiring key (the sidebar marker
	// and collapse-storage contract).
	ExtraAttrs html.Attrs
}

var sidebarStyle = registry.RegisterStyle("ui-sidebar", sidebarCSS,
	registry.WithLoad(registry.LoadAlways))

// sidebarClasses is the fui-sidebar class map over the primitive's
// parts. Variant classes ride the root's class string; the sub-item
// and fallback-icon variants ride their part's variant slot.
func sidebarClasses(variant SidebarVariant) headless.Classes {
	return headless.Classes{
		headless.PartRoot:                  "fui-sidebar fui-sidebar--" + string(variant),
		headless.PartSidebarDrawer:         "fui-sidebar__hamburger fui-sidebar__hamburger--" + string(variant),
		headless.PartSidebarInline:         "fui-sidebar__inline",
		headless.PartSidebarToggle:         "fui-sidebar__collapse",
		headless.PartTitle:                 "fui-sidebar__title",
		headless.PartSidebarPrepend:        "fui-sidebar__prepend",
		headless.PartSidebarNav:            "fui-sidebar__nav",
		headless.PartBody:                  "fui-sidebar__list",
		headless.PartSidebarItem:           "fui-sidebar__item",
		headless.Part("sidebar-item--sub"): "fui-sidebar__item--sub",
		headless.PartControl:               "fui-sidebar__link",
		headless.PartSidebarGroup:          "fui-sidebar__group",
		headless.PartSidebarGroupToggle:    "fui-sidebar__group-toggle",
		headless.PartSidebarGroupList:      "fui-sidebar__sublist",
		headless.PartIcon:                  "fui-sidebar__icon",
		headless.Part("icon--fallback"):    "fui-sidebar__icon--fallback",
		headless.PartText:                  "fui-sidebar__label",
		headless.PartFooter:                "fui-sidebar__footer",
	}
}

// Sidebar renders the inline nav column + the hamburger trigger that
// opens the < md drawer. The drawer widget itself is mounted by the
// caller via MountSidebar (once per app, at startup).
//
// Slot it into a layout as the shell's static chrome: render it inside
// an app.NewLayout build function, beside l.Primary() — the way
// examples/tracker's buildShell does. Inline use is also fine. The
// component is self-contained.
func Sidebar(cfg SidebarConfig) component.Component {
	if cfg.Variant == "" {
		cfg.Variant = SidebarPersistent
	}
	if cfg.DrawerName == "" {
		cfg.DrawerName = "ui-sidebar-drawer"
	}
	return sidebarComponent{cfg: cfg}
}

// rolesExtractor reads the signed-in user's roles from the request context.
// nil = role-aware nav not wired (items render unfiltered). The app registers
// it once via SetRolesExtractor (the generated app wires it to the auth user).
var rolesExtractor func(ctx context.Context) []string

// SidebarDrawerTrigger renders the sidebar's hamburger button on its
// own, for hosts that place it in their own chrome — the page header —
// and pass SidebarConfig.SuppressDrawerTrigger so the sidebar itself
// draws no second copy. Same button, class, and widget contract as the
// trigger Sidebar renders (data-cui-open names the MountSidebar
// drawer), and the same >= md self-hiding from the component's own
// stylesheet: at widths where the inline column shows, the header
// trigger disappears on its own. The drawer widget still has to be
// mounted once via MountSidebar.
func SidebarDrawerTrigger(cfg SidebarConfig) render.HTML {
	if cfg.Variant == "" {
		cfg.Variant = SidebarPersistent
	}
	if cfg.DrawerName == "" {
		cfg.DrawerName = "ui-sidebar-drawer"
	}
	checkSidebarVariant(cfg.Variant)
	checkStackBreakpoint(cfg.DrawerBreakpoint)
	classes := drawerTriggerClasses(cfg.Variant)
	if cfg.DrawerBreakpoint == StackBelowLG {
		classes[headless.PartRoot] += " fui-sidebar__hamburger--drawer-below-lg"
	}
	drawerLabel := "Open navigation"
	if cfg.Compact {
		drawerLabel = cfg.navLabel()
		classes[headless.PartRoot] += " fui-sidebar__hamburger--labelled"
	}
	if cfg.NativeMobile {
		classes[headless.PartRoot] += " fui-sidebar__hamburger--native"
	}
	// The standalone trigger's button IS its component root (see the
	// headless primitive), so the hamburger part's classes map onto
	// the root here — the same bytes the shell's inline copy renders.
	return sidebarStyle.WrapHTML(headless.SidebarDrawerTrigger(headless.SidebarProps{
		DrawerName:  cfg.DrawerName,
		DrawerLabel: drawerLabel,
		Variant:     string(cfg.Variant),
	}, classes))
}

// drawerTriggerClasses is sidebarClasses with the hamburger's own
// part mapped onto the root: the button wears fui-sidebar__hamburger
// (and its variant), never the column shell's fui-sidebar classes.
func drawerTriggerClasses(variant SidebarVariant) headless.Classes {
	return headless.Classes{
		headless.PartRoot: "fui-sidebar__hamburger fui-sidebar__hamburger--" + string(variant),
	}
}

// SetRolesExtractor installs the function that pulls the current user's roles
// from a request context, enabling SidebarItem.Roles filtering. Idempotent;
// pass nil to disable.
func SetRolesExtractor(f func(ctx context.Context) []string) { rolesExtractor = f }

// sidebarVisible reports whether an item is visible to the ctx user: items with
// no Roles are always visible; otherwise the user must hold one of them. With
// no extractor wired, everything is visible (the feature is opt-in).
func sidebarVisible(ctx context.Context, it SidebarItem) bool {
	if len(it.Roles) == 0 || rolesExtractor == nil {
		return true
	}
	for _, r := range rolesExtractor(ctx) {
		for _, want := range it.Roles {
			if r == want {
				return true
			}
		}
	}
	return false
}

// filterSidebarItems returns the items visible to the ctx user, recursing into
// children. Returns the input unchanged when no extractor is wired.
func filterSidebarItems(ctx context.Context, items []SidebarItem) []SidebarItem {
	if rolesExtractor == nil {
		return items
	}
	out := make([]SidebarItem, 0, len(items))
	for _, it := range items {
		if !sidebarVisible(ctx, it) {
			continue
		}
		if len(it.Children) > 0 {
			it.Children = filterSidebarItems(ctx, it.Children)
			// A group whose every child is gated away is an empty
			// disclosure; a group with no href is not a link either.
			// Drop it rather than render a dead entry.
			if len(it.Children) == 0 && it.Href == "" {
				continue
			}
		}
		out = append(out, it)
	}
	return out
}

// withFilteredItems returns a copy of cfg whose Items are filtered for the ctx
// user. Footer/Title/etc. are unchanged.
func (c SidebarConfig) withFilteredItems(ctx context.Context) SidebarConfig {
	c.Items = filterSidebarItems(ctx, c.Items)
	return c
}

type sidebarComponent struct{ cfg SidebarConfig }

// RenderCtx renders the sidebar with role-filtered items. The app layout
// threads the request context here (WrapCtx), so role-gated entries (e.g. an
// admin-only link) never appear for users who lack the role.
//
// When the filter leaves nothing (a guest on an admin-only nav), the
// sidebar renders nothing: an empty navigation landmark names a region
// with nothing in it, and the headless primitive refuses one. Roles are
// request data, so this path renders instead of panicking.
func (s sidebarComponent) RenderCtx(ctx context.Context) render.HTML {
	cfg := s.cfg.withFilteredItems(ctx)
	if len(cfg.Items) == 0 && len(s.cfg.Items) > 0 {
		return ""
	}
	return sidebarComponent{cfg: cfg}.render(ctx)
}

func (s sidebarComponent) Render() render.HTML { return s.render(context.Background()) }

func (s sidebarComponent) render(ctx context.Context) render.HTML {
	// Unknown variants panic like every other variant-taking component
	// (Card, Button, Notification). A typo'd variant used to render
	// an unstyled fui-sidebar--<anything> class silently. Empty is the
	// documented default (Sidebar() normalizes it to persistent).
	cfg := s.cfg
	classes := sidebarClasses(cfg.Variant)
	drawerLabel := "Open navigation"
	if cfg.Compact {
		classes[headless.PartRoot] += " fui-sidebar--compact"
		drawerLabel = cfg.navLabel()
		classes[headless.PartSidebarDrawer] += " fui-sidebar__hamburger--labelled"
	}
	checkSidebarVariant(cfg.Variant)
	checkSidebarCollapse(cfg.Collapse)
	checkSidebarGroupMarkup(cfg.GroupMarkup)
	checkStackBreakpoint(cfg.DrawerBreakpoint)
	if len(cfg.Items) == 0 {
		// No entries at all renders nothing, the same answer as a role
		// filter that leaves nothing (RenderCtx): the headless primitive
		// refuses an empty navigation landmark, and an app whose nav is
		// built from data may have none yet. Config mistakes above still
		// panic.
		return ""
	}
	if cfg.DrawerBreakpoint == StackBelowLG {
		classes[headless.PartRoot] += " fui-sidebar--drawer-below-lg"
		classes[headless.PartSidebarDrawer] += " fui-sidebar__hamburger--drawer-below-lg"
	}

	// The collapse contract exists only on the collapsible variant:
	// the other variants carry no collapse attrs at all.
	var serverCollapsed *bool
	collapse := "none"
	storageKey := ""
	if cfg.Variant == SidebarCollapsible {
		switch cfg.Collapse {
		case SidebarCollapseAuto:
			// Auto: the runtime owns the state and restores it from
			// localStorage after hydration.
			collapse = "auto"
			storageKey = cfg.CollapseStorageKey
			if storageKey == "" {
				storageKey = "gofastr.sidebar." + cfg.DrawerName + ".collapsed"
			}
		case SidebarCollapseCollapsed:
			v := true
			serverCollapsed = &v
		case SidebarCollapseExpanded:
			v := false
			serverCollapsed = &v
		}
	}

	// The collapse button's two names resolve through i18nui the way the
	// component's other strings resolve (a caller's label wins). Both ride
	// the button as data attributes: the runtime module carries no English
	// fallback, so the pair in the markup is the only wording a
	// client-side toggle can re-say.
	collapseLabel := cfg.CollapseLabel
	if collapseLabel == "" {
		collapseLabel = i18nui.T(ctx, i18nui.KeyHuiSidebarCollapse)
	}
	expandLabel := cfg.ExpandLabel
	if expandLabel == "" {
		expandLabel = i18nui.T(ctx, i18nui.KeyHuiSidebarExpand)
	}

	out := headless.Sidebar(headless.SidebarProps{
		NavLabel:           cfg.navLabel(),
		Title:              cfg.Title,
		Items:              sidebarNavItems(cfg),
		Variant:            string(cfg.Variant),
		Collapse:           collapse,
		ServerCollapsed:    serverCollapsed,
		DrawerName:         cfg.DrawerName,
		DrawerLabel:        drawerLabel,
		CollapseStorageKey: storageKey,
		HideDrawerTrigger:  cfg.SuppressDrawerTrigger,
		CollapseLabel:      collapseLabel,
		ExpandLabel:        expandLabel,
		GroupMarkup:        string(cfg.GroupMarkup),
		GroupIDPrefix:      cfg.DrawerName + "-inline",
		Prepend:            sidebarPrepend(ctx, cfg),
		Footer:             cfg.Footer,
		ExtraAttrs:         headless.Safe(cfg.ExtraAttrs),
	}, classes)
	if cfg.NativeMobile {
		nativeClass := "fui-sidebar-native"
		if cfg.DrawerBreakpoint == StackBelowLG {
			nativeClass += " fui-sidebar-native--drawer-below-lg"
		}
		return html.Div(html.DivConfig{Class: nativeClass},
			sidebarStyle.WrapHTML(out),
			html.Div(html.DivConfig{Class: "fui-sidebar-native__mobile"},
				Collapsible(CollapsibleConfig{Summary: cfg.navLabel()},
					// The mobile disclosure sits OUTSIDE the marked
					// inline root, so it carries its own marker: the
					// sheet's [data-cui-comp]-scoped rules (link
					// styling, the compact variants) must reach it.
					sidebarStyle.WrapHTML(sidebarBodyRegion(ctx, cfg, cfg.DrawerName+"-mobile", "fui-sidebar__body")))),
		)
	}
	return sidebarStyle.WrapHTML(out)
}

// navLabel is the nav landmark's name, defaulted.
func (c SidebarConfig) navLabel() string {
	if c.NavLabel == "" {
		return "Primary"
	}
	return c.NavLabel
}

// sidebarPrepend renders the Prepend component for ctx (its Render
// fallback when it takes no context).
func sidebarPrepend(ctx context.Context, cfg SidebarConfig) render.HTML {
	if cfg.Prepend == nil {
		return ""
	}
	return component.RenderComponentCtx(ctx, cfg.Prepend)
}

// sidebarNavItems maps the config's tree onto the primitive's,
// settling active and open state on the way: Active wins, then
// MatchPath on a segment boundary, then the exact Href; a group opens
// when it is active itself, any descendant matches, or Open forces it.
func sidebarNavItems(cfg SidebarConfig) []headless.SidebarItem {
	var walk func(items []SidebarItem) []headless.SidebarItem
	walk = func(items []SidebarItem) []headless.SidebarItem {
		out := make([]headless.SidebarItem, 0, len(items))
		for _, it := range items {
			mapped := headless.SidebarItem{
				Label: it.Label, Href: it.Href, Icon: it.Icon,
				// MatchPath rides the leaf as data-cui-match-prefix so
				// the runtime's active-link sweep keeps the item lit on
				// sub-paths after a client navigation (the server owns
				// only first paint).
				MatchPrefix: it.MatchPath,
			}
			if len(it.Children) > 0 {
				kids := walk(it.Children)
				open := it.Open || sidebarActive(cfg.CurrentPath, it) || sidebarHasActiveDescendant(it, cfg.CurrentPath)
				mapped.Open = open
				mapped.Children = kids
			} else {
				mapped.Active = sidebarActive(cfg.CurrentPath, it)
			}
			out = append(out, mapped)
		}
		return out
	}
	return walk(cfg.Items)
}

// sidebarActive reports the item's active state under currentPath.
func sidebarActive(currentPath string, it SidebarItem) bool {
	if it.Active {
		return true
	}
	if currentPath == "" {
		return false
	}
	if it.MatchPath != "" {
		return pathMatches(currentPath, it.MatchPath)
	}
	if it.Href != "" {
		return currentPath == it.Href
	}
	return false
}

// SidebarBody renders the navigation content only: no sidebar shell,
// no hamburger. Use it as the Slot content of a preset.Drawer widget
// that mirrors the sidebar at narrow viewports. It has no request
// context, so a context-aware Prepend renders its Render fallback
// here; MountSidebar's drawer renders it per request.
//
// The region renders through headless.SidebarRegion (no shell hooks,
// so the runtime never treats the host's chrome as a sidebar root)
// with the fui-sidebar class map's body spelling on its root.
func SidebarBody(cfg SidebarConfig) render.HTML {
	if cfg.Variant == "" {
		cfg.Variant = SidebarPersistent
	}
	if cfg.DrawerName == "" {
		cfg.DrawerName = "ui-sidebar-drawer"
	}
	checkSidebarGroupMarkup(cfg.GroupMarkup)
	return sidebarBodyRegion(context.Background(), cfg, cfg.DrawerName+"-body", "fui-sidebar fui-sidebar__body")
}

// sidebarBodyRegion renders a body path (SidebarBody, the drawer
// slot): the primitive's region — no shell hooks, so the runtime never
// treats the host's chrome as a sidebar root — under the body root
// class spelling and the given group-id prefix.
func sidebarBodyRegion(ctx context.Context, cfg SidebarConfig, idPrefix, rootClass string) render.HTML {
	checkSidebarGroupMarkup(cfg.GroupMarkup)
	if len(cfg.Items) == 0 {
		// Same answer as the inline sidebar: no entries, no region.
		return ""
	}
	classes := sidebarClasses(cfg.Variant)
	classes[headless.PartRoot] = rootClass
	if cfg.Compact {
		classes[headless.PartRoot] += " fui-sidebar--compact"
	}
	out := headless.SidebarRegion(headless.SidebarProps{
		NavLabel:      cfg.navLabel(),
		Title:         cfg.Title,
		Items:         sidebarNavItems(cfg),
		GroupMarkup:   string(cfg.GroupMarkup),
		GroupIDPrefix: idPrefix,
		Prepend:       sidebarPrepend(ctx, cfg),
		Footer:        cfg.Footer,
	}, classes)
	return sidebarStyle.WrapHTML(out)
}

// pathMatches reports whether currentPath is the prefix itself or falls
// under it on a segment boundary. A boundary-less prefix match would
// mark "/customers-archive" active for MatchPath "/customers".
func pathMatches(currentPath, prefix string) bool {
	return currentPath == prefix || strings.HasPrefix(currentPath, strings.TrimSuffix(prefix, "/")+"/")
}

// sidebarHasActiveDescendant returns true when any descendant of it matches
// currentPath via the same rules used at the leaf level (MatchPath
// prefix, or exact Href).
func sidebarHasActiveDescendant(it SidebarItem, currentPath string) bool {
	for _, c := range it.Children {
		if sidebarActive(currentPath, c) || sidebarHasActiveDescendant(c, currentPath) {
			return true
		}
	}
	return false
}

// sidebarDrawerSlot renders the drawer's body: same content as the
// inline sidebar minus the hamburger button. Wraps in
// data-cui-comp="ui-sidebar" so the sidebar stylesheet applies
// inside the drawer too (the framework's per-component CSS scoping
// keys on that marker).
//
// resolve, when set (MountSidebarFunc), rebuilds the config from the
// request on every render, so a footer or item list that depends on the
// session matches the inline sidebar's; cfg then carries the mount-time
// identity (DrawerName, Variant) the resolved config keeps.
type sidebarDrawerSlot struct {
	cfg     SidebarConfig
	resolve func(context.Context) SidebarConfig
}

// config returns the drawer's config for ctx: the per-request one when
// the drawer was mounted with a resolver, the boot-time one otherwise.
func (s sidebarDrawerSlot) config(ctx context.Context) SidebarConfig {
	if s.resolve == nil {
		return s.cfg
	}
	cfg := s.resolve(ctx)
	cfg.DrawerName, cfg.Variant = s.cfg.DrawerName, s.cfg.Variant
	return cfg
}

// RenderCtx renders the drawer body with role-filtered items. The widget host
// serves the drawer chrome per-request (serveChrome) and threads the request
// context here, so the mobile drawer hides the same role-gated entries the
// desktop sidebar does.
func (s sidebarDrawerSlot) RenderCtx(ctx context.Context) render.HTML {
	base := s.config(ctx)
	cfg := base.withFilteredItems(ctx)
	if len(cfg.Items) == 0 && len(base.Items) > 0 {
		// Every entry gated away: the drawer body is empty, as the
		// inline sidebar is (see sidebarComponent.RenderCtx).
		return ""
	}
	return sidebarDrawerSlot{cfg: cfg}.render(ctx)
}

func (s sidebarDrawerSlot) Render() render.HTML {
	return sidebarDrawerSlot{cfg: s.config(context.Background())}.render(context.Background())
}

func (s sidebarDrawerSlot) render(ctx context.Context) render.HTML {
	cfg := s.cfg
	checkSidebarGroupMarkup(cfg.GroupMarkup)
	// "-drawer" prefix keeps button-dialect group ids distinct from
	// the inline sidebar's when both are in the DOM.
	return sidebarBodyRegion(ctx, cfg, cfg.DrawerName+"-drawer", "fui-sidebar fui-sidebar--drawer-body")
}

// sidebarDrawerHeader renders the drawer's header row: the app's
// brand (DrawerTitle, falling back to Title) beside a 44px close
// button that rides the widget runtime's data-cui-action="close"
// contract — the same dismiss path the backdrop and Escape take, and
// dismiss returns focus to the trigger that opened the drawer.
type sidebarDrawerHeader struct{ title string }

func (h sidebarDrawerHeader) Render() render.HTML {
	if h.title == "" {
		return ""
	}
	// Wrapped in the ui-sidebar marker so the drawer's chrome fetch
	// pulls this sheet too — the header slot sits in the widget
	// chrome, beside (not inside) the body's marked region.
	return sidebarStyle.WrapHTML(html.Div(html.DivConfig{Class: "fui-sidebar__drawer-head"},
		html.Span(html.TextConfig{Class: "fui-sidebar__drawer-brand"}, render.Text(h.title)),
		render.Tag("button", map[string]string{
			"type":            "button",
			"class":           "fui-sidebar__drawer-close",
			"aria-label":      "Close navigation",
			"data-cui-action": "close",
		}, render.Text("×")),
	))
}

// MountSidebar registers BOTH the sidebar drawer widget (for < md
// viewports) AND mounts it on r. Returns the widget definition. Call
// once per app at startup. The same SidebarConfig is passed to
// `Sidebar(cfg)` when rendering screens so the two views stay in
// sync.
//
// The drawer renders cfg as given on every request (only the role
// filter sees the request). When the sidebar's content depends on the
// request beyond Roles (a footer with the signed-in user's Sign out),
// use MountSidebarFunc with the same builder the layout calls.
//
// Generic signature: `r` is anything widget.Mount accepts (the
// gofastr router). We use a tiny adapter type so this package doesn't
// need to import the router directly.
func MountSidebar(r WidgetMounter, cfg SidebarConfig, pages ...string) widget.Definition {
	return mountSidebar(r, cfg, nil, pages)
}

// MountSidebarFunc is MountSidebar for a sidebar built per request:
// the drawer body calls build with each chrome request's context, so
// its footer and items match what the inline Sidebar(build(ctx))
// renders for the same session. Pass the function the app layout
// already uses. The drawer's identity (DrawerName, Variant) and its
// header brand (DrawerTitle, falling back to Title) come from
// build(context.Background()) at mount time.
func MountSidebarFunc(r WidgetMounter, build func(ctx context.Context) SidebarConfig, pages ...string) widget.Definition {
	if build == nil {
		panic("ui.MountSidebarFunc: build is nil")
	}
	return mountSidebar(r, build(context.Background()), build, pages)
}

func mountSidebar(r WidgetMounter, cfg SidebarConfig, build func(context.Context) SidebarConfig, pages []string) widget.Definition {
	if cfg.Variant == "" {
		cfg.Variant = SidebarPersistent
	}
	if cfg.DrawerName == "" {
		cfg.DrawerName = "ui-sidebar-drawer"
	}
	title := cfg.DrawerTitle
	if title == "" {
		title = cfg.Title
	}
	b := preset.Drawer(cfg.DrawerName).
		Hidden().
		Slot("header", sidebarDrawerHeader{title: title}).
		Slot("body", sidebarDrawerSlot{cfg: cfg, resolve: build})
	// Optional page scoping: apps that only use the sidebar on a
	// subset of routes can declare them explicitly; omitting `pages`
	// keeps the drawer globally available.
	if len(pages) > 0 {
		b = b.Pages(pages...)
	}
	def := b.Build()
	r.MountWidget(&def)
	return def
}

// WidgetMounter is the minimal contract for hosting a widget on a
// router. Apps adapt the framework's *router.Router with a three-line
// shim (wiring is intentionally pluggable so this package stays
// router-agnostic):
//
//	type routerMounter struct{ r *router.Router }
//
//	func (m routerMounter) MountWidget(def *widget.Definition) {
//		widget.Mount(m.r, def)
//	}
//
//	ui.MountSidebar(routerMounter{app.Router()}, sidebarCfg)
type WidgetMounter interface {
	MountWidget(def *widget.Definition)
}

func sidebarCSS(_ style.Theme) string {
	return `.fui-sidebar-native { min-inline-size: 0; }
.fui-sidebar-native__mobile { display: none; }
[data-cui-comp="ui-sidebar"].fui-sidebar--compact .fui-sidebar__inline { width: 100%; min-width: 0; padding: 0; }
[data-cui-comp="ui-sidebar"].fui-sidebar--compact .fui-sidebar__link { min-height: 0; padding: var(--spacing-xs) var(--spacing-sm); font-size: var(--text-sm); border-radius: 0; border-inline-start: 1px solid transparent; }
[data-cui-comp="ui-sidebar"].fui-sidebar--compact .fui-sidebar__link[aria-current="page"] { background: transparent; color: var(--color-primary); border-inline-start-color: var(--color-primary); }
[data-cui-comp="ui-sidebar"].fui-sidebar--compact .fui-sidebar__sublist { padding: 0; margin-inline-start: var(--spacing-md); box-shadow: inset 1px 0 var(--color-border); }
[data-cui-comp="ui-sidebar"].fui-sidebar--compact .fui-sidebar__group > summary,
[data-cui-comp="ui-sidebar"].fui-sidebar--compact .fui-sidebar__group-toggle { font-weight: var(--font-weight-semibold); }
[data-cui-comp="ui-sidebar"].fui-sidebar--compact .fui-sidebar__item + .fui-sidebar__item > .fui-sidebar__group { margin-block-start: var(--spacing-sm); }
[data-cui-comp="ui-sidebar"].fui-sidebar--compact .fui-sidebar__group-toggle { padding: var(--spacing-sm); font-size: var(--text-xs); }
[data-cui-comp="ui-sidebar"].fui-sidebar--compact .fui-sidebar__list { gap: 0; }
@media (scripting: none) { .fui-sidebar__hamburger.fui-sidebar__hamburger--native { display: none; } }
@media (max-width: 47.99rem) and (scripting: none) {
  .fui-sidebar-native:not(.fui-sidebar-native--drawer-below-lg) > .fui-sidebar-native__mobile { display: block; }
  .fui-sidebar-native:not(.fui-sidebar-native--drawer-below-lg) .fui-sidebar__hamburger { display: none; }
}
/* DrawerBreakpoint lg: the no-script disclosure takes over below lg
   (64rem) instead, so tablets without script get the native menu too. */
@media (max-width: 63.99rem) and (scripting: none) {
  .fui-sidebar-native--drawer-below-lg > .fui-sidebar-native__mobile { display: block; }
  .fui-sidebar-native--drawer-below-lg .fui-sidebar__hamburger { display: none; }
}
:where([data-cui-comp="ui-sidebar"]).fui-sidebar {
  display: contents;
}
.fui-sidebar__hamburger {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: var(--spacing-touch-target, 44px);
  height: var(--spacing-touch-target, 44px);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFF);
  color: var(--color-text, #18181B);
  cursor: pointer;
  font-size: var(--text-xl, 1.25rem);
  line-height: 1;
}
.fui-sidebar__hamburger--labelled { width: 100%; justify-content: flex-start; gap: var(--spacing-md); padding-inline: var(--spacing-md); font-size: var(--text-sm); }
.fui-sidebar__hamburger--labelled::after { content: attr(aria-label); }
[data-cui-comp="ui-sidebar"] .fui-sidebar__collapse {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  justify-self: end;
  width: var(--spacing-touch-target, 44px);
  height: var(--spacing-touch-target, 44px);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-sm, 4px);
  background: var(--color-surface, #FFF);
  color: var(--color-text, #18181B);
  cursor: pointer;
  font-size: var(--text-xl, 1.25rem);
}
[data-cui-comp="ui-sidebar"] .fui-sidebar__collapse:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}
[data-cui-comp="ui-sidebar"] .fui-sidebar__icon--fallback {
  display: none;
}
[data-cui-comp="ui-sidebar"] .fui-sidebar__inline {
  display: grid;
  gap: var(--spacing-md, 8px);
  padding: var(--spacing-lg, 16px);
  min-width: 220px;
}
[data-cui-comp="ui-sidebar"] .fui-sidebar__title {
  font-size: var(--text-sm, 0.875rem);
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--color-text-muted, #52525B);
  margin: 0;
}
[data-cui-comp="ui-sidebar"] .fui-sidebar__list,
[data-cui-comp="ui-sidebar"] .fui-sidebar__sublist {
  list-style: none;
  padding: 0;
  margin: 0;
  display: grid;
  gap: var(--spacing-xs, 2px);
}
[data-cui-comp="ui-sidebar"] .fui-sidebar__sublist {
  margin-inline-start: var(--spacing-lg, 16px);
}
/* Button-dialect group containers carry the hidden attribute when
   closed. The grid display above would override the UA's
   [hidden] { display: none } (same specificity class, later author
   rule), so the attribute must win explicitly or a closed group's
   links stay visible. */
/* Button-dialect group headers: strip the UA button chrome so the
   toggle renders like the <summary> it replaces (the shared
   .fui-sidebar__link rule supplies layout, color, hover, focus). */
[data-cui-comp="ui-sidebar"] .fui-sidebar__group-toggle {
  width: 100%;
  border: none;
  background: none;
  font: inherit;
  cursor: pointer;
}
[data-cui-comp="ui-sidebar"] .fui-sidebar__sublist[hidden] {
  display: none;
}
[data-cui-comp="ui-sidebar"] .fui-sidebar__link {
  display: flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
  padding: var(--spacing-sm, 4px) var(--spacing-md, 8px);
  border-radius: var(--radii-sm, 4px);
  color: var(--color-text, #18181B);
  text-decoration: none;
  min-height: var(--spacing-touch-target, 44px);
  cursor: pointer;
}
[data-cui-comp="ui-sidebar"] .fui-sidebar__link:hover,
[data-cui-comp="ui-sidebar"] .fui-sidebar__link:focus-visible {
  background: var(--color-surface-soft, #F4F4F5);
}
[data-cui-comp="ui-sidebar"] .fui-sidebar__link:focus-visible {
  /* Visible focus ring on BOTH the default and the active
     (primary-background) link. The previous background-only signal was
     invisible on the active link: the [aria-current="page"] rule below
     (equal specificity, later source) overrode the focus background, and
     outline:none removed the ring — so a keyboard user could not see
     focus land on the current page's nav item. */
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}
[data-cui-comp="ui-sidebar"] .fui-sidebar__link[aria-current="page"] {
  /* Use the primary + primary-fg token pair so contrast is guaranteed
     AA regardless of theme. The previous 12%-primary tinted bg + raw
     primary text failed contrast for some primary hues. */
  background: var(--color-primary, #4F46E5);
  color: var(--color-primary-fg, #FFFFFF);
  font-weight: var(--font-weight-semibold);
}
[data-cui-comp="ui-sidebar"] .fui-sidebar__group > summary {
  list-style: none;
}
[data-cui-comp="ui-sidebar"] .fui-sidebar__group > summary::-webkit-details-marker {
  display: none;
}
[data-cui-comp="ui-sidebar"] .fui-sidebar__footer {
  margin-top: auto;
  padding-top: var(--spacing-md, 8px);
  border-top: 1px solid var(--color-border, #E4E4E7);
}
[data-cui-comp="ui-sidebar"] .fui-sidebar__prepend {
  padding-bottom: var(--spacing-md, 8px);
  border-bottom: 1px solid var(--color-border, #E4E4E7);
}
/* The drawer body has no column padding (each nav row pads itself), so
   the title, Prepend and footer take the rows' inline inset: their text
   starts where the rows' text does, not flush against the drawer edge.
   Compact rows add a 1px current-marker border before their padding. */
[data-cui-comp="ui-sidebar"].fui-sidebar--drawer-body .fui-sidebar__title,
[data-cui-comp="ui-sidebar"].fui-sidebar--drawer-body .fui-sidebar__prepend,
[data-cui-comp="ui-sidebar"].fui-sidebar--drawer-body .fui-sidebar__footer {
  padding-inline: var(--spacing-md, 8px);
}
[data-cui-comp="ui-sidebar"].fui-sidebar--drawer-body.fui-sidebar--compact .fui-sidebar__title,
[data-cui-comp="ui-sidebar"].fui-sidebar--drawer-body.fui-sidebar--compact .fui-sidebar__prepend,
[data-cui-comp="ui-sidebar"].fui-sidebar--drawer-body.fui-sidebar--compact .fui-sidebar__footer {
  padding-inline: calc(var(--spacing-sm) + 1px);
}
[data-cui-comp="ui-sidebar"].fui-sidebar--collapsible[data-collapsed="true"] .fui-sidebar__inline {
  min-width: 64px;
  width: 64px;
  padding-inline: var(--spacing-sm, 4px);
}
[data-cui-comp="ui-sidebar"].fui-sidebar--collapsible[data-collapsed="true"] .fui-sidebar__collapse {
  justify-self: center;
  transform: rotate(180deg);
}
[data-cui-comp="ui-sidebar"].fui-sidebar--collapsible[data-collapsed="true"] .fui-sidebar__title,
[data-cui-comp="ui-sidebar"].fui-sidebar--collapsible[data-collapsed="true"] .fui-sidebar__prepend,
[data-cui-comp="ui-sidebar"].fui-sidebar--collapsible[data-collapsed="true"] .fui-sidebar__footer,
[data-cui-comp="ui-sidebar"].fui-sidebar--collapsible[data-collapsed="true"] .fui-sidebar__sublist {
  display: none;
}
/* Labels go visually-hidden (clip), NOT display:none — the links and
   disclosure summaries stay focusable in the collapsed rail, so their
   only accessible name must survive for AT (WCAG 4.1.2). */
[data-cui-comp="ui-sidebar"].fui-sidebar--collapsible[data-collapsed="true"] .fui-sidebar__label {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}
[data-cui-comp="ui-sidebar"].fui-sidebar--collapsible[data-collapsed="true"] .fui-sidebar__link {
  justify-content: center;
  padding-inline: var(--spacing-sm, 4px);
}
[data-cui-comp="ui-sidebar"].fui-sidebar--collapsible[data-collapsed="true"] .fui-sidebar__icon--fallback {
  display: inline-flex;
}
[data-cui-comp="ui-sidebar"].fui-sidebar--off-canvas .fui-sidebar__inline {
  display: none;
}
/* Viewport behaviour: < md collapses to the hamburger; ≥ md the
   inline column appears and the hamburger hides. OffCanvas keeps the
   hamburger on every viewport. DrawerBreakpoint lg moves the whole
   switch to < lg (64rem): the marker classes ride the component root
   (and the standalone trigger's own button), and the md rules scope
   themselves with :not() so the two postures never both apply. */
@media (max-width: 47.99rem) {
  [data-cui-comp="ui-sidebar"]:not(.fui-sidebar--drawer-below-lg) .fui-sidebar__inline { display: none; }
  [data-cui-comp="ui-sidebar"].fui-sidebar--compact:not(.fui-sidebar--drawer-below-lg) .fui-sidebar__link { min-block-size: var(--spacing-touch-target, 44px); }
}
@media (max-width: 63.99rem) {
  [data-cui-comp="ui-sidebar"].fui-sidebar--drawer-below-lg .fui-sidebar__inline { display: none; }
  [data-cui-comp="ui-sidebar"].fui-sidebar--drawer-below-lg.fui-sidebar--compact .fui-sidebar__link { min-block-size: var(--spacing-touch-target, 44px); }
}
/* Auto-hide variant: icon rail at rest, full column on :hover OR
   :focus-within. The focus-within half is load-bearing, not a
   nicety: hover-only would hide every link from keyboard users
   tabbing through the page. Rest-state widths, spacing, and the
   visually-hidden label clip mirror the collapsible collapsed rail;
   the reveal restores the persistent column's sizing. The framework
   ships this styling (one styling surface — hosts write no CSS). */
[data-cui-comp="ui-sidebar"].fui-sidebar--auto-hide .fui-sidebar__inline {
  min-width: 64px;
  width: 64px;
  padding-inline: var(--spacing-sm, 4px);
  transition: width var(--duration-fast, 150ms) var(--easing-ease-out, cubic-bezier(0.16, 1, 0.3, 1)),
    min-width var(--duration-fast, 150ms) var(--easing-ease-out, cubic-bezier(0.16, 1, 0.3, 1)),
    padding-inline var(--duration-fast, 150ms) var(--easing-ease-out, cubic-bezier(0.16, 1, 0.3, 1));
}
[data-cui-comp="ui-sidebar"].fui-sidebar--auto-hide:hover .fui-sidebar__inline,
[data-cui-comp="ui-sidebar"].fui-sidebar--auto-hide:focus-within .fui-sidebar__inline {
  min-width: 220px;
  width: 220px;
  padding-inline: var(--spacing-lg, 16px);
}
/* Rest-state chrome rules apply only while NOT revealed: on hover
   or focus-within they stop matching and the base (expanded) styles
   take over, so the reveal needs no mirrored overrides. */
[data-cui-comp="ui-sidebar"].fui-sidebar--auto-hide:not(:hover):not(:focus-within) .fui-sidebar__title,
[data-cui-comp="ui-sidebar"].fui-sidebar--auto-hide:not(:hover):not(:focus-within) .fui-sidebar__prepend,
[data-cui-comp="ui-sidebar"].fui-sidebar--auto-hide:not(:hover):not(:focus-within) .fui-sidebar__footer,
[data-cui-comp="ui-sidebar"].fui-sidebar--auto-hide:not(:hover):not(:focus-within) .fui-sidebar__sublist {
  display: none;
}
[data-cui-comp="ui-sidebar"].fui-sidebar--auto-hide:not(:hover):not(:focus-within) .fui-sidebar__label {
  /* Same visually-hidden clip as the collapsed rail: the links stay
     focusable, so their accessible name must survive (WCAG 4.1.2). */
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}
[data-cui-comp="ui-sidebar"].fui-sidebar--auto-hide:not(:hover):not(:focus-within) .fui-sidebar__link {
  justify-content: center;
  padding-inline: var(--spacing-sm, 4px);
}
[data-cui-comp="ui-sidebar"].fui-sidebar--auto-hide:not(:hover):not(:focus-within) .fui-sidebar__icon--fallback {
  display: inline-flex;
}
/* The MountSidebar drawer's header row: the app's brand beside a
   44px close button, so the panel reads as the app's own and closes
   without hunting for the scrim. Keyed on the drawer classes (not
   the ui-sidebar marker prefix) because the header slot lives in the
   widget chrome, beside the body's marked region — the marker rides
   the header root only to fetch this sheet. */
.fui-sidebar__drawer-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--spacing-md, 8px);
  /* The same INLINE inset the nav rows' own padding gives their
     icons/text (the drawer body carries no column padding, so the
     row's padding IS the inset): the brand lines up with the first
     row's icon instead of starting 8px deeper. */
  padding: var(--spacing-md, 8px) var(--spacing-md, 8px);
  border-bottom: 1px solid var(--color-border, #E4E4E7);
}
.fui-sidebar__drawer-brand {
  font-weight: var(--font-weight-bold);
  font-size: var(--text-base, 1rem);
  color: var(--color-text, #18181B);
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.fui-sidebar__drawer-close {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: 0 0 auto;
  width: var(--spacing-touch-target, 44px);
  height: var(--spacing-touch-target, 44px);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFF);
  color: var(--color-text, #18181B);
  cursor: pointer;
  font: inherit;
  font-size: var(--text-xl, 1.25rem);
  line-height: 1;
}
.fui-sidebar__drawer-close:hover {
  background: var(--color-surface-soft, #F4F4F5);
}
.fui-sidebar__drawer-close:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}
@media (prefers-reduced-motion: reduce) {
  [data-cui-comp="ui-sidebar"].fui-sidebar--auto-hide .fui-sidebar__inline { transition: none; }
}
@media (min-width: 48rem) {
  .fui-sidebar__hamburger--persistent:not(.fui-sidebar__hamburger--drawer-below-lg),
  .fui-sidebar__hamburger--collapsible:not(.fui-sidebar__hamburger--drawer-below-lg),
  .fui-sidebar__hamburger--auto-hide:not(.fui-sidebar__hamburger--drawer-below-lg) {
    display: none;
  }
}
@media (min-width: 64rem) {
  .fui-sidebar__hamburger--drawer-below-lg.fui-sidebar__hamburger--persistent,
  .fui-sidebar__hamburger--drawer-below-lg.fui-sidebar__hamburger--collapsible,
  .fui-sidebar__hamburger--drawer-below-lg.fui-sidebar__hamburger--auto-hide {
    display: none;
  }
}
`
}
