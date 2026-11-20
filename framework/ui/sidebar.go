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

// SidebarVariant selects the sidebar's wide-screen form; below the drawer breakpoint every variant is a drawer.
type SidebarVariant string

const (
	SidebarPersistent  SidebarVariant = "persistent"
	SidebarCollapsible SidebarVariant = "collapsible"
	SidebarOffCanvas   SidebarVariant = "off-canvas"
	SidebarAutoHide    SidebarVariant = "auto-hide"
)

func checkSidebarVariant(v SidebarVariant) {
	switch v {
	case "", SidebarPersistent, SidebarCollapsible, SidebarOffCanvas, SidebarAutoHide:
	default:
		panic("ui: Sidebar unknown Variant " + string(v) +
			`. Pick one of: "" (persistent), "collapsible", "off-canvas", "auto-hide"`)
	}
}

// SidebarCollapse selects who owns the collapsed rail: the runtime's localStorage (Auto) or the server.
type SidebarCollapse string

const (
	SidebarCollapseAuto      SidebarCollapse = ""
	SidebarCollapseCollapsed SidebarCollapse = "collapsed"
	SidebarCollapseExpanded  SidebarCollapse = "expanded"
)

func checkSidebarCollapse(c SidebarCollapse) {
	switch c {
	case SidebarCollapseAuto, SidebarCollapseCollapsed, SidebarCollapseExpanded:
	default:
		panic("ui: Sidebar unknown Collapse " + string(c) +
			`. Pick one of: "" (auto), "collapsed", "expanded"`)
	}
}

// SidebarGroupMarkup selects the group dialect: details (the zero value) or a script-driven button.
type SidebarGroupMarkup string

const (
	SidebarGroupDetails SidebarGroupMarkup = ""
	SidebarGroupButton  SidebarGroupMarkup = "button"
)

func checkSidebarGroupMarkup(m SidebarGroupMarkup) {
	switch m {
	case SidebarGroupDetails, SidebarGroupButton:
	default:
		panic("ui: Sidebar unknown GroupMarkup " + string(m) +
			`. Pick one of: "" (details), "button"`)
	}
}

// StackBreakpoint is the width a responsive piece switches posture at.
type StackBreakpoint string

const (
	// StackBelowMD switches below md (48rem); the zero value.
	StackBelowMD StackBreakpoint = ""
	// StackBelowLG switches below lg (64rem), tablets with phones.
	StackBelowLG StackBreakpoint = "lg"
)

func checkStackBreakpoint(b StackBreakpoint) {
	switch b {
	case StackBelowMD, StackBelowLG:
	default:
		panic("ui: unknown StackBreakpoint " + string(b) + `. Pick one of: "" (below md), "lg" (below lg)`)
	}
}

// SidebarItem is one navigation entry; Children form a nested group.
type SidebarItem struct {
	Label    string
	Href     string
	Icon     render.HTML
	Children []SidebarItem
	// Roles limits the item to users holding one of them (SetRolesExtractor).
	Roles []string
	// Active marks the item current whatever the path.
	Active bool
	// MatchPath replaces the exact Href match with a section prefix.
	MatchPath string
	// Count is a muted figure at the row's end, hidden in the collapsed rail.
	Count render.HTML
	// Open renders a group expanded on first paint.
	Open bool
}

// SidebarConfig describes a navigation sidebar.
type SidebarConfig struct {
	// Ctx is the language of a standalone SidebarDrawerTrigger; nil is English.
	Ctx context.Context

	// Title is the sidebar's top heading. Empty omits it.
	Title string

	// NavLabel names the navigation landmark. Defaults to "Primary".
	NavLabel string

	// Items is the navigation tree. None left after the role filter renders nothing.
	Items []SidebarItem

	// CurrentPath marks the current link on the server; empty leaves it to the runtime.
	CurrentPath string

	// Variant defaults to SidebarPersistent.
	Variant SidebarVariant
	// Compact is a documentation rail: tighter links and a thin current marker.
	Compact bool
	// SectionLabels draws group headers as small uppercase section labels.
	SectionLabels bool
	// RaisedCurrent draws the current link as a raised pill, not a grey fill.
	RaisedCurrent bool

	// DrawerBreakpoint is the width below which the column becomes the drawer.
	DrawerBreakpoint StackBreakpoint

	// Collapse picks who owns the collapsible variant's state.
	Collapse SidebarCollapse

	// GroupMarkup selects the dialect used for items with Children.
	GroupMarkup SidebarGroupMarkup

	// Prepend renders between the title and the nav on every body path, with the request where there is one.
	Prepend component.Component

	// Footer is optional content at the bottom.
	Footer render.HTML

	// DrawerTitle is the drawer header's brand; empty falls back to Title.
	DrawerTitle string
	// DrawerCloseLabel names the drawer's dismiss button; empty is the i18nui phrase.
	DrawerCloseLabel string

	// DrawerName is the drawer widget's name. Defaults to "ui-sidebar-drawer".
	DrawerName string

	// CollapseStorageKey overrides the SidebarCollapseAuto localStorage key.
	CollapseStorageKey string

	// CollapseLabel names the collapse button while expanded.
	CollapseLabel string

	// ExpandLabel names the collapse button while collapsed.
	ExpandLabel string

	// CollapseText is the collapse button's visible word; keep it inside CollapseLabel.
	CollapseText string

	// SuppressDrawerTrigger omits the hamburger, for a SidebarDrawerTrigger elsewhere.
	SuppressDrawerTrigger bool

	// NativeMobile adds a native disclosure for when scripting is disabled.
	NativeMobile bool

	// ExtraAttrs land on the root; class, id, aria-label, data-collapsed and the wiring are dropped.
	ExtraAttrs html.Attrs
}

var sidebarStyle = registry.RegisterStyle("ui-sidebar", sidebarCSS,
	registry.WithLoad(registry.LoadPrewarm))

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
		headless.PartSidebarCount:          "fui-sidebar__count",
		headless.PartFooter:                "fui-sidebar__footer",
	}
}

// Sidebar renders the inline nav column and the hamburger that opens the MountSidebar drawer.
func Sidebar(cfg SidebarConfig) component.Component {
	if cfg.Variant == "" {
		cfg.Variant = SidebarPersistent
	}
	if cfg.DrawerName == "" {
		cfg.DrawerName = "ui-sidebar-drawer"
	}
	return sidebarComponent{cfg: cfg}
}

var rolesExtractor func(ctx context.Context) []string

// SidebarDrawerTrigger renders the hamburger alone, for a header beside SuppressDrawerTrigger.
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
	drawerLabel := i18nui.SidebarOpenDrawer.Text(cfg.Ctx)
	if cfg.Compact {
		drawerLabel = cfg.navLabel(cfg.Ctx)
		classes[headless.PartRoot] += " fui-sidebar__hamburger--labelled"
	}
	if cfg.NativeMobile {
		classes[headless.PartRoot] += " fui-sidebar__hamburger--native"
	}
	return sidebarStyle.WrapHTML(headless.SidebarDrawerTrigger(headless.SidebarProps{
		DrawerName:  cfg.DrawerName,
		DrawerLabel: drawerLabel,
		Variant:     string(cfg.Variant),
	}, classes))
}

func drawerTriggerClasses(variant SidebarVariant) headless.Classes {
	return headless.Classes{
		headless.PartRoot: "fui-sidebar__hamburger fui-sidebar__hamburger--" + string(variant),
	}
}

// SetRolesExtractor installs the request's roles for SidebarItem.Roles; nil disables it.
func SetRolesExtractor(f func(ctx context.Context) []string) { rolesExtractor = f }

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
			if len(it.Children) == 0 && it.Href == "" {
				continue
			}
		}
		out = append(out, it)
	}
	return out
}

func (c SidebarConfig) withFilteredItems(ctx context.Context) SidebarConfig {
	c.Items = filterSidebarItems(ctx, c.Items)
	return c
}

type sidebarComponent struct{ cfg SidebarConfig }

// RenderCtx renders the sidebar with the items ctx's roles may see.
func (s sidebarComponent) RenderCtx(ctx context.Context) render.HTML {
	cfg := s.cfg.withFilteredItems(ctx)
	if len(cfg.Items) == 0 && len(s.cfg.Items) > 0 {
		return ""
	}
	return sidebarComponent{cfg: cfg}.render(ctx)
}

func (s sidebarComponent) Render() render.HTML { return s.render(context.Background()) }

func (s sidebarComponent) render(ctx context.Context) render.HTML {
	cfg := s.cfg
	classes := sidebarClasses(cfg.Variant)
	drawerLabel := i18nui.SidebarOpenDrawer.Text(ctx)
	if cfg.SectionLabels {
		classes[headless.PartRoot] += " fui-sidebar--section-labels"
	}
	if cfg.RaisedCurrent {
		classes[headless.PartRoot] += " fui-sidebar--raised-current"
	}
	if cfg.Compact {
		classes[headless.PartRoot] += " fui-sidebar--compact"
		drawerLabel = cfg.navLabel(ctx)
		classes[headless.PartSidebarDrawer] += " fui-sidebar__hamburger--labelled"
	}
	checkSidebarVariant(cfg.Variant)
	checkSidebarCollapse(cfg.Collapse)
	checkSidebarGroupMarkup(cfg.GroupMarkup)
	checkStackBreakpoint(cfg.DrawerBreakpoint)
	if len(cfg.Items) == 0 {
		return ""
	}
	if cfg.DrawerBreakpoint == StackBelowLG {
		classes[headless.PartRoot] += " fui-sidebar--drawer-below-lg"
		classes[headless.PartSidebarDrawer] += " fui-sidebar__hamburger--drawer-below-lg"
	}

	var serverCollapsed *bool
	collapse := "none"
	storageKey := ""
	if cfg.Variant == SidebarCollapsible {
		switch cfg.Collapse {
		case SidebarCollapseAuto:
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

	collapseLabel := cfg.CollapseLabel
	if collapseLabel == "" {
		collapseLabel = i18nui.HuiSidebarCollapse.Text(ctx)
	}
	expandLabel := cfg.ExpandLabel
	if expandLabel == "" {
		expandLabel = i18nui.HuiSidebarExpand.Text(ctx)
	}
	collapseText := cfg.CollapseText
	if collapseText == "" {
		collapseText = i18nui.HuiSidebarCollapseText.Text(ctx)
	}

	out := headless.Sidebar(headless.SidebarProps{
		NavLabel:           cfg.navLabel(ctx),
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
		ToggleIcon:         Icon("panel-left", IconConfig{Size: "16"}),
		ToggleText:         collapseText,
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
				Collapsible(CollapsibleConfig{Summary: cfg.navLabel(ctx)},
					sidebarStyle.WrapHTML(sidebarBodyRegion(ctx, cfg, cfg.DrawerName+"-mobile", "fui-sidebar__body")))),
		)
	}
	return sidebarStyle.WrapHTML(out)
}

func (c SidebarConfig) navLabel(ctx context.Context) string {
	if c.NavLabel == "" {
		return i18nui.NavPrimary.Text(ctx)
	}
	return c.NavLabel
}

func sidebarPrepend(ctx context.Context, cfg SidebarConfig) render.HTML {
	if cfg.Prepend == nil {
		return ""
	}
	return component.RenderComponentCtx(ctx, cfg.Prepend)
}

func sidebarNavItems(cfg SidebarConfig) []headless.SidebarItem {
	var walk func(items []SidebarItem) []headless.SidebarItem
	walk = func(items []SidebarItem) []headless.SidebarItem {
		out := make([]headless.SidebarItem, 0, len(items))
		for _, it := range items {
			mapped := headless.SidebarItem{
				Label: it.Label, Href: it.Href, Icon: it.Icon, Count: it.Count,
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

// SidebarBody renders the navigation content alone, for a host's own drawer, without a request.
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

func sidebarBodyRegion(ctx context.Context, cfg SidebarConfig, idPrefix, rootClass string) render.HTML {
	checkSidebarGroupMarkup(cfg.GroupMarkup)
	if len(cfg.Items) == 0 {
		return ""
	}
	classes := sidebarClasses(cfg.Variant)
	classes[headless.PartRoot] = rootClass
	if cfg.SectionLabels {
		classes[headless.PartRoot] += " fui-sidebar--section-labels"
	}
	if cfg.Compact {
		classes[headless.PartRoot] += " fui-sidebar--compact"
	}
	if cfg.RaisedCurrent {
		classes[headless.PartRoot] += " fui-sidebar--raised-current"
	}
	out := headless.SidebarRegion(headless.SidebarProps{
		NavLabel:      cfg.navLabel(ctx),
		Title:         cfg.Title,
		Items:         sidebarNavItems(cfg),
		GroupMarkup:   string(cfg.GroupMarkup),
		GroupIDPrefix: idPrefix,
		Prepend:       sidebarPrepend(ctx, cfg),
		Footer:        cfg.Footer,
	}, classes)
	return sidebarStyle.WrapHTML(out)
}

func pathMatches(currentPath, prefix string) bool {
	bare := func(p string) string {
		if len(p) > 1 {
			return strings.TrimSuffix(p, "/")
		}
		return p
	}
	cur, pre := bare(currentPath), bare(prefix)
	if pre == "/" || pre == "" {
		return cur == "/"
	}
	return cur == pre || strings.HasPrefix(cur, pre+"/")
}

func sidebarHasActiveDescendant(it SidebarItem, currentPath string) bool {
	for _, c := range it.Children {
		if sidebarActive(currentPath, c) || sidebarHasActiveDescendant(c, currentPath) {
			return true
		}
	}
	return false
}

type sidebarDrawerSlot struct {
	cfg     SidebarConfig
	resolve func(context.Context) SidebarConfig
}

func (s sidebarDrawerSlot) config(ctx context.Context) SidebarConfig {
	if s.resolve == nil {
		return s.cfg
	}
	cfg := s.resolve(ctx)
	cfg.DrawerName, cfg.Variant = s.cfg.DrawerName, s.cfg.Variant
	return cfg
}

// RenderCtx renders the drawer body with the items ctx's roles may see.
func (s sidebarDrawerSlot) RenderCtx(ctx context.Context) render.HTML {
	base := s.config(ctx)
	cfg := base.withFilteredItems(ctx)
	if len(cfg.Items) == 0 && len(base.Items) > 0 {
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
	return sidebarBodyRegion(ctx, cfg, cfg.DrawerName+"-drawer", "fui-sidebar fui-sidebar--drawer-body")
}

type sidebarDrawerHeader struct {
	title, closeLabel string
	labelID           string
	compact           bool
	resolve           func(context.Context) SidebarConfig
}

func (h sidebarDrawerHeader) Render() render.HTML {
	return h.RenderCtx(context.Background())
}

func (h sidebarDrawerHeader) RenderCtx(ctx context.Context) render.HTML {
	name := html.Span(html.TextConfig{ID: h.labelID, ExtraAttrs: html.Attrs{"hidden": ""}},
		render.Text(i18nui.SidebarDrawerLabel.Text(ctx)))
	if h.title == "" {
		return name
	}
	label, compact := h.closeLabel, h.compact
	if h.resolve != nil {
		cfg := h.resolve(ctx)
		label, compact = cfg.DrawerCloseLabel, cfg.Compact
	}
	if label == "" {
		label = i18nui.SidebarCloseDrawer.Text(ctx)
	}
	class := "fui-sidebar__drawer-head"
	if compact {
		class += " fui-sidebar__drawer-head--compact"
	}
	return name + sidebarStyle.WrapHTML(html.Div(html.DivConfig{Class: class},
		html.Span(html.TextConfig{Class: "fui-sidebar__drawer-brand"}, render.Text(h.title)),
		Button(ButtonConfig{Ctx: ctx, Label: label, Icon: "close", IconOnly: true,
			Variant: ButtonGhost, Class: "fui-sidebar__drawer-close",
			ExtraAttrs: html.Attrs{"data-cui-action": "close"}}),
	))
}

// MountSidebar mounts the sidebar's drawer widget on r at startup, with the cfg the screens pass to Sidebar.
func MountSidebar(r WidgetMounter, cfg SidebarConfig, pages ...string) widget.Definition {
	return mountSidebar(r, cfg, nil, pages)
}

// MountSidebarFunc is MountSidebar for a sidebar that build makes per request.
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
	labelID := cfg.DrawerName + "-label"
	b := preset.Drawer(cfg.DrawerName).
		Hidden().
		LabelledBy(labelID).
		Slot("header", sidebarDrawerHeader{title: title, closeLabel: cfg.DrawerCloseLabel, labelID: labelID, compact: cfg.Compact, resolve: build}).
		Slot("body", sidebarDrawerSlot{cfg: cfg, resolve: build})
	if len(pages) > 0 {
		b = b.Pages(pages...)
	}
	def := b.Build()
	r.MountWidget(&def)
	return def
}

// WidgetMounter hosts a widget on a router, through widget.Mount.
type WidgetMounter interface {
	MountWidget(def *widget.Definition)
}

func sidebarCSS(_ style.Theme) string {
	return `.fui-sidebar-native { min-inline-size: 0; }
.fui-sidebar-native__mobile { display: none; }
[data-cui-comp="ui-sidebar"].fui-sidebar--compact .fui-sidebar__inline { width: 100%; min-width: 0; padding: 0; }
[data-cui-comp="ui-sidebar"].fui-sidebar--compact .fui-sidebar__link { min-height: 0; padding: var(--spacing-xs) var(--spacing-sm); font-size: var(--text-sm); border-radius: 0; border-inline-start: var(--stroke-thin, 1px) solid transparent; }
[data-cui-comp="ui-sidebar"].fui-sidebar--compact .fui-sidebar__link[aria-current="page"] { background: transparent; color: var(--color-text); font-weight: var(--font-weight-medium); border-inline-start-color: var(--color-text); }
[data-cui-comp="ui-sidebar"].fui-sidebar--compact .fui-sidebar__sublist { padding: 0; margin-inline-start: var(--spacing-md); box-shadow: inset 1px 0 var(--color-border); }
/* The guide line rides the inline start, like the indent and the
   current marker: in a right-to-left page that is the right edge. */
[data-cui-comp="ui-sidebar"].fui-sidebar--compact .fui-sidebar__sublist:dir(rtl) { box-shadow: inset -1px 0 var(--color-border); }
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
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFF);
  color: var(--color-text, #09090B);
  cursor: pointer;
  font-size: var(--text-xl, 1.25rem);
  line-height: 1;
}
.fui-sidebar__hamburger--labelled { width: 100%; justify-content: flex-start; gap: var(--spacing-md); padding-inline: var(--spacing-md); font-size: var(--text-sm); }
.fui-sidebar__hamburger--labelled::after { content: attr(aria-label); }
/* The collapse toggle is the column's foot row: the panel icon and its
   word, muted like a nav link, held at the bottom of a scrolling column
   on the column's own surface. */
[data-cui-comp="ui-sidebar"] .fui-sidebar__collapse {
  position: sticky;
  inset-block-end: 0;
  margin-block: auto calc(-1 * var(--spacing-lg, 16px));
  margin-inline: calc(-1 * var(--spacing-lg, 16px));
  display: flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
  padding: var(--spacing-md, 8px) calc(var(--spacing-lg, 16px) + var(--spacing-md, 8px));
  min-height: var(--ui-sidebar-row-height, var(--spacing-touch-target, 44px));
  border: 0;
  border-block-start: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  background: var(--color-surface, #FFF);
  color: var(--color-text-muted, #52525B);
  font: inherit;
  font-size: var(--text-sm, 0.875rem);
  cursor: pointer;
}
[data-cui-comp="ui-sidebar"] .fui-sidebar__collapse:hover { color: var(--color-text, #09090B); }
[data-cui-comp="ui-sidebar"] .fui-sidebar__collapse > [aria-hidden] { display: inline-flex; }
[data-cui-comp="ui-sidebar"] .fui-sidebar__collapse:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
/* A flex box, so the glyph centres on the label instead of sitting on
   an inline line box's baseline. */
[data-cui-comp="ui-sidebar"] .fui-sidebar__icon {
  display: inline-flex;
  flex-shrink: 0;
}
[data-cui-comp="ui-sidebar"] .fui-sidebar__icon--fallback {
  display: none;
}
/* A link's count sits at the row's end, small and muted, in tabular
   figures so the column of counts lines up. */
[data-cui-comp="ui-sidebar"] .fui-sidebar__count {
  margin-inline-start: auto;
  padding-inline-start: var(--spacing-sm, 4px);
  color: var(--color-text-subtle, #71717A);
  font-size: var(--text-xs, 0.75rem);
  font-variant-numeric: tabular-nums;
}
[data-cui-comp="ui-sidebar"].fui-sidebar--collapsible[data-collapsed="true"] .fui-sidebar__count,
[data-cui-comp="ui-sidebar"].fui-sidebar--auto-hide:not(:hover):not(:focus-within) .fui-sidebar__count {
  display: none;
}
/* Knobs: --ui-sidebar-width (220px) is the expanded inline column's
   width (min-width and width); --ui-sidebar-rail-width (64px) is the
   collapsed icon rail's width (the collapsible collapsed state and the
   auto-hide rest state); --ui-sidebar-drawer-inset (--spacing-md) is
   the gap between the < md drawer's edges and its rows, header and
   footer; --ui-sidebar-row-height is a row's minimum
   height: the touch target on a coarse pointer, 2.25rem under a fine
   one, where a 44px row spreads a long nav over two screens. */
@media (pointer: fine) {
  [data-cui-comp="ui-sidebar"] .fui-sidebar__inline { --ui-sidebar-row-height: 2.25rem; }
}
/* In a column of fixed height (a sticky ContentRow's nav, one screen
   tall) the inline sidebar fills it and its nav scrolls in its own
   region, so the brand stays put and the collapse row sits under the
   links instead of painting over the last one. In a column that grows
   with the page, 100% resolves to auto and nothing changes. */
[data-cui-comp="ui-sidebar"] .fui-sidebar__inline {
  display: flex;
  flex-direction: column;
  min-block-size: 100%;
  block-size: 100%;
  box-sizing: border-box;
  gap: var(--spacing-md, 8px);
  padding: var(--spacing-lg, 16px);
  min-width: var(--ui-sidebar-width, 220px);
}
/* The scroll region clips what its links draw outside their box (the
   raised current link's ring, a focus outline), so it pads by the
   focus ring's reach and takes the same back with a negative margin:
   the links stay where they were. */
[data-cui-comp="ui-sidebar"] .fui-sidebar__inline > .fui-sidebar__nav {
  flex: 1 1 auto;
  min-block-size: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
  padding: calc(var(--stroke-focus, 2px) + var(--stroke-focus-offset, 2px));
  margin: calc(-1 * (var(--stroke-focus, 2px) + var(--stroke-focus-offset, 2px)));
}
[data-cui-comp="ui-sidebar"] .fui-sidebar__title {
  font-size: var(--text-xs, 0.75rem);
  font-weight: var(--font-weight-medium);
  color: var(--color-text-muted, #52525B);
  padding-inline: var(--spacing-md, 8px);
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
  border-radius: var(--radii-sm, 6px);
  color: var(--color-text-muted, #52525B);
  font-size: var(--text-sm, 0.875rem);
  text-decoration: none;
  min-height: var(--ui-sidebar-row-height, var(--spacing-touch-target, 44px));
  cursor: pointer;
  transition: background-color var(--duration-fast, 150ms), color var(--duration-fast, 150ms);
}
[data-cui-comp="ui-sidebar"] .fui-sidebar__link:hover,
[data-cui-comp="ui-sidebar"] .fui-sidebar__link:focus-visible {
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text, #09090B);
}
[data-cui-comp="ui-sidebar"] .fui-sidebar__link:focus-visible {
  /* Visible focus ring on BOTH the default and the active
     (primary-background) link. The previous background-only signal was
     invisible on the active link: the [aria-current="page"] rule below
     (equal specificity, later source) overrode the focus background, and
     outline:none removed the ring — so a keyboard user could not see
     focus land on the current page's nav item. */
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
[data-cui-comp="ui-sidebar"] .fui-sidebar__link[aria-current="page"] {
  /* The neutral surface-soft + text pair: contrast holds in every
     theme because both are neutrals, and the current page reads as
     selected without a saturated slab. (A 12%-primary tint with raw
     primary text failed contrast for some primary hues.) */
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text, #09090B);
  font-weight: var(--font-weight-medium);
}
/* RaisedCurrent: the current link sits on the surface as a pill, ringed
   by a hairline and lifted by the smallest shadow. */
[data-cui-comp="ui-sidebar"].fui-sidebar--raised-current .fui-sidebar__link[aria-current="page"] {
  background: var(--color-surface);
  box-shadow: var(--shadow-xs), 0 0 0 var(--stroke-thin, 1px) var(--color-border);
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
  border-top: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
}
[data-cui-comp="ui-sidebar"] .fui-sidebar__prepend {
  padding-bottom: var(--spacing-md, 8px);
  border-bottom: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
}
/* The drawer body is display: contents, so its parts stand off the
   panel's edges themselves, by --ui-sidebar-drawer-inset: a row's hover
   and current fill no longer runs into both sides of the drawer. */
[data-cui-comp="ui-sidebar"].fui-sidebar--drawer-body > * {
  margin-inline: var(--ui-sidebar-drawer-inset, var(--spacing-md, 8px));
}
/* Inside that inset each nav row pads itself, so the title, Prepend and
   footer take the rows' inline padding: their text starts where the
   rows' text does. Compact rows add a 1px current-marker border before
   their padding. */
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
  min-width: var(--ui-sidebar-rail-width, 64px);
  width: var(--ui-sidebar-rail-width, 64px);
  padding-inline: var(--spacing-sm, 4px);
}
[data-cui-comp="ui-sidebar"].fui-sidebar--collapsible[data-collapsed="true"] .fui-sidebar__collapse {
  justify-content: center;
  margin-inline: calc(-1 * var(--spacing-sm, 4px));
  padding-inline: var(--spacing-sm, 4px);
}
[data-cui-comp="ui-sidebar"].fui-sidebar--collapsible[data-collapsed="true"] .fui-sidebar__title,
[data-cui-comp="ui-sidebar"].fui-sidebar--collapsible[data-collapsed="true"] .fui-sidebar__prepend:not(:has(> [data-cui-comp="ui-sidebar-brand"]:only-child)),
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
  clip-path: inset(50%);
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
/* The collapsed rail keeps a Prepend that is only a SidebarBrand (its
   logo tile heads the rail) and opens each group under a rule, so the
   rail still reads as groups once their labels are gone. */
[data-cui-comp="ui-sidebar"].fui-sidebar--collapsible[data-collapsed="true"] .fui-sidebar__item:has(> .fui-sidebar__group, > .fui-sidebar__sublist) {
  margin-block-start: var(--spacing-sm, 4px);
  padding-block-start: var(--spacing-sm, 4px);
  border-block-start: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
}
/* The collapsed rail: an open group's header gives way to its links,
   so every page keeps its own icon in the rail; a closed group keeps
   its header (the one way back into it). */
[data-cui-comp="ui-sidebar"].fui-sidebar--collapsible[data-collapsed="true"] .fui-sidebar__group[open] > .fui-sidebar__sublist,
[data-cui-comp="ui-sidebar"].fui-sidebar--collapsible[data-collapsed="true"] .fui-sidebar__group-toggle[aria-expanded="true"] + .fui-sidebar__sublist {
  display: grid;
  margin-inline-start: 0;
}
[data-cui-comp="ui-sidebar"].fui-sidebar--collapsible[data-collapsed="true"] .fui-sidebar__group[open] > summary,
[data-cui-comp="ui-sidebar"].fui-sidebar--collapsible[data-collapsed="true"] .fui-sidebar__group-toggle[aria-expanded="true"] {
  display: none;
}
/* SectionLabels: group headers read as section labels over flush
   links, with a chevron that turns when the group closes. The header
   keeps a 28px box (--ui-sidebar-section-height) so it stays a
   comfortable target, and drops its icon outside the collapsed rail,
   where the label alone names it. --ui-sidebar-chevron-size (12px)
   sizes the chevron. */
[data-cui-comp="ui-sidebar"].fui-sidebar--section-labels .fui-sidebar__group > summary,
[data-cui-comp="ui-sidebar"].fui-sidebar--section-labels .fui-sidebar__group-toggle {
  min-height: var(--ui-sidebar-section-height, 1.75rem);
  margin-block-start: var(--spacing-md, 8px);
  padding-block: var(--spacing-xs, 2px);
  font-size: var(--text-xs, 0.75rem);
  font-weight: var(--font-weight-semibold);
  text-transform: uppercase;
  letter-spacing: var(--tracking-wide, 0.04em);
  color: var(--color-text-muted, #52525B);
  background: none;
}
[data-cui-comp="ui-sidebar"].fui-sidebar--section-labels .fui-sidebar__group > summary::after,
[data-cui-comp="ui-sidebar"].fui-sidebar--section-labels .fui-sidebar__group-toggle::after {
  content: "";
  margin-inline-start: auto;
  inline-size: var(--ui-sidebar-chevron-size, 12px);
  block-size: var(--ui-sidebar-chevron-size, 12px);
  background: currentColor;
  -webkit-mask: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 12 12'%3E%3Cpath d='M3 4.5l3 3 3-3' fill='none' stroke='black' stroke-width='1.5' stroke-linecap='round' stroke-linejoin='round'/%3E%3C/svg%3E") center / contain no-repeat;
  mask: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 12 12'%3E%3Cpath d='M3 4.5l3 3 3-3' fill='none' stroke='black' stroke-width='1.5' stroke-linecap='round' stroke-linejoin='round'/%3E%3C/svg%3E") center / contain no-repeat;
  transition: rotate var(--duration-fast, 150ms) var(--easing-ease-in-out, ease);
}
/* A mask icon is painted by its background, which forced colours turn
   to Canvas; opting out and painting CanvasText keeps it visible. */
@media (forced-colors: active) {
  [data-cui-comp="ui-sidebar"].fui-sidebar--section-labels .fui-sidebar__group > summary::after,
  [data-cui-comp="ui-sidebar"].fui-sidebar--section-labels .fui-sidebar__group-toggle::after { forced-color-adjust: none; background: CanvasText; }
}
[data-cui-comp="ui-sidebar"].fui-sidebar--section-labels .fui-sidebar__group:not([open]) > summary::after,
[data-cui-comp="ui-sidebar"].fui-sidebar--section-labels .fui-sidebar__group-toggle[aria-expanded="false"]::after {
  rotate: -90deg;
}
/* A closed chevron points toward the inline end: right in a
   left-to-right page, left in a right-to-left one. */
[data-cui-comp="ui-sidebar"].fui-sidebar--section-labels .fui-sidebar__group:not([open]) > summary:dir(rtl)::after,
[data-cui-comp="ui-sidebar"].fui-sidebar--section-labels .fui-sidebar__group-toggle[aria-expanded="false"]:dir(rtl)::after {
  rotate: 90deg;
}
[data-cui-comp="ui-sidebar"].fui-sidebar--section-labels:not([data-collapsed="true"]) .fui-sidebar__group > summary .fui-sidebar__icon,
[data-cui-comp="ui-sidebar"].fui-sidebar--section-labels:not([data-collapsed="true"]) .fui-sidebar__group-toggle .fui-sidebar__icon {
  display: none;
}
[data-cui-comp="ui-sidebar"].fui-sidebar--section-labels .fui-sidebar__sublist {
  margin-inline-start: 0;
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
  min-width: var(--ui-sidebar-rail-width, 64px);
  width: var(--ui-sidebar-rail-width, 64px);
  padding-inline: var(--spacing-sm, 4px);
  transition: width var(--duration-fast, 150ms) var(--easing-ease-out, cubic-bezier(0.16, 1, 0.3, 1)),
    min-width var(--duration-fast, 150ms) var(--easing-ease-out, cubic-bezier(0.16, 1, 0.3, 1)),
    padding-inline var(--duration-fast, 150ms) var(--easing-ease-out, cubic-bezier(0.16, 1, 0.3, 1));
}
[data-cui-comp="ui-sidebar"].fui-sidebar--auto-hide:hover .fui-sidebar__inline,
[data-cui-comp="ui-sidebar"].fui-sidebar--auto-hide:focus-within .fui-sidebar__inline {
  min-width: var(--ui-sidebar-width, 220px);
  width: var(--ui-sidebar-width, 220px);
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
  clip-path: inset(50%);
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
  /* The brand starts where the first row's icon does (the drawer
     inset plus the row's own padding), and the close button ends where
     the rows do. */
  padding-block: var(--spacing-md, 8px);
  padding-inline: calc(var(--ui-sidebar-drawer-inset, var(--spacing-md, 8px)) + var(--spacing-md, 8px)) var(--ui-sidebar-drawer-inset, var(--spacing-md, 8px));
  border-bottom: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
}
/* Compact rows pad by --spacing-sm after a 1px current marker. */
.fui-sidebar__drawer-head--compact {
  padding-inline-start: calc(var(--ui-sidebar-drawer-inset, var(--spacing-md, 8px)) + var(--spacing-sm, 4px) + 1px);
}
.fui-sidebar__drawer-brand {
  font-weight: var(--font-weight-bold);
  font-size: var(--text-base, 1rem);
  color: var(--color-text, #09090B);
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
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFF);
  color: var(--color-text, #09090B);
  cursor: pointer;
  font: inherit;
  font-size: var(--text-xl, 1.25rem);
  line-height: 1;
}
.fui-sidebar__drawer-close:hover {
  background: var(--color-surface-soft, #F4F4F5);
}
.fui-sidebar__drawer-close:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
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
