package ui

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// SidebarVariant selects how the sidebar behaves at ≥ md viewports.
//
//	SidebarPersistent: fixed-width column, always visible.
//	SidebarCollapsible: column with a chevron that toggles a compact
//	  rail; expanded/collapsed state persists in localStorage.
//	SidebarOffCanvas: hidden by default — opens via the hamburger
//	  trigger on every viewport (no inline column).
//
// On `< md` every variant collapses to a hamburger + drawer.
type SidebarVariant string

const (
	SidebarPersistent  SidebarVariant = "persistent"
	SidebarCollapsible SidebarVariant = "collapsible"
	SidebarOffCanvas   SidebarVariant = "off-canvas"
)

// SidebarItem is one navigation entry. Children nest one level deep.
// Deeper nesting is unsupported by design — sidebars should not be
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
	// Href" check used to mark the item as active. Pass a literal
	// prefix ("/customers") to highlight on sub-paths, or use
	// CurrentPath in your screen and set Active manually.
	MatchPath string
}

// SidebarConfig describes a navigation sidebar.
type SidebarConfig struct {
	// Title is rendered as the sidebar's top heading. Empty omits it.
	Title string

	// Items is the navigation tree.
	Items []SidebarItem

	// CurrentPath is the screen's current path, used for active-state
	// highlighting. When empty, falls back to JS: the runtime stamps
	// aria-current on any matching <a> after hydration.
	CurrentPath string

	// Variant defaults to SidebarPersistent.
	Variant SidebarVariant

	// Footer is optional content rendered at the bottom (signed-in
	// user pill, settings link, etc.).
	Footer render.HTML

	// DrawerName overrides the widget name used for the < md drawer.
	// Defaults to "ui-sidebar-drawer". Apps that host multiple
	// sidebars per page must override to avoid collisions.
	DrawerName string

	// SuppressDrawerTrigger hides the hamburger button rendered by
	// Sidebar (some apps put their hamburger in the page header
	// instead and call MountSidebar themselves).
	SuppressDrawerTrigger bool
}

var sidebarStyle = registry.RegisterStyle("ui-sidebar", sidebarCSS,
	registry.WithLoad(registry.LoadAlways))

// Sidebar renders the inline nav column + the hamburger trigger that
// opens the < md drawer. The drawer widget itself is mounted by the
// caller via MountSidebar (once per app, at startup).
//
// Pair with core-ui/app/layout.Layout.WithSidebar to slot it into the
// canonical chrome. Inline use is also fine — the component is
// self-contained.
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
	have := rolesExtractor(ctx)
	for _, want := range it.Roles {
		for _, h := range have {
			if h == want {
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
func (s sidebarComponent) RenderCtx(ctx context.Context) render.HTML {
	return sidebarComponent{cfg: s.cfg.withFilteredItems(ctx)}.render()
}

func (s sidebarComponent) Render() render.HTML { return s.render() }

func (s sidebarComponent) render() render.HTML {
	cfg := s.cfg
	var b strings.Builder
	// A <div>, not <aside>: when slotted into a layout the layout wraps
	// the sidebar in its own <nav aria-label="Sidebar"> landmark, so an
	// <aside> here would nest complementary inside navigation — axe's
	// landmark-complementary-is-top-level rule fires on the double
	// landmark. The layout's <nav> is the sole landmark; this element
	// is the styled shell (display:contents, so it adds no box).
	b.WriteString(`<div class="ui-sidebar ui-sidebar--` + string(cfg.Variant) + `" data-fui-sidebar>`)

	if !cfg.SuppressDrawerTrigger {
		b.WriteString(`<button class="ui-sidebar__hamburger" type="button" ` +
			`data-fui-open="` + escAttr(cfg.DrawerName) + `" aria-label="Open navigation">` +
			`<span aria-hidden="true">☰</span></button>`)
	}

	b.WriteString(`<div class="ui-sidebar__inline">`)
	b.WriteString(string(sidebarBody(cfg)))
	b.WriteString(`</div></div>`)
	return sidebarStyle.WrapHTML(render.HTML(b.String()))
}

// SidebarBody renders the navigation content only — no sidebar shell,
// no hamburger. Use it as the Slot content of a preset.Drawer widget
// that mirrors the sidebar at narrow viewports.
func SidebarBody(cfg SidebarConfig) render.HTML {
	if cfg.Variant == "" {
		cfg.Variant = SidebarPersistent
	}
	return sidebarBody(cfg)
}

func sidebarBody(cfg SidebarConfig) render.HTML {
	var b strings.Builder
	if cfg.Title != "" {
		b.WriteString(`<h2 class="ui-sidebar__title">` + escText(cfg.Title) + `</h2>`)
	}
	b.WriteString(`<nav class="ui-sidebar__nav" aria-label="Primary">`)
	b.WriteString(`<ul class="ui-sidebar__list">`)
	for _, it := range cfg.Items {
		writeSidebarItem(&b, it, cfg.CurrentPath, 0)
	}
	b.WriteString(`</ul></nav>`)
	if cfg.Footer != "" {
		b.WriteString(`<div class="ui-sidebar__footer">` + string(cfg.Footer) + `</div>`)
	}
	return render.HTML(b.String())
}

func writeSidebarItem(b *strings.Builder, it SidebarItem, currentPath string, depth int) {
	active := it.Active
	if !active && currentPath != "" {
		if it.MatchPath != "" {
			active = strings.HasPrefix(currentPath, it.MatchPath)
		} else if it.Href != "" {
			active = currentPath == it.Href
		}
	}
	hasChildren := len(it.Children) > 0
	cls := "ui-sidebar__item"
	if depth > 0 {
		cls += " ui-sidebar__item--sub"
	}
	b.WriteString(`<li class="` + cls + `">`)
	if hasChildren {
		// Group with a disclosure for child items; <details> reuses
		// the framework's data-fui-disclosure machinery (Esc-close,
		// SPA-nav close, aria-expanded mirror) without extra wiring.
		// Open the disclosure when the group itself is active OR any
		// descendant matches CurrentPath, so navigating to a nested
		// route lands on a section that's already expanded.
		openAttr := ""
		if active || hasActiveDescendant(it, currentPath) {
			openAttr = " open"
		}
		b.WriteString(`<details class="ui-sidebar__group" data-fui-disclosure` + openAttr + `>`)
		b.WriteString(`<summary class="ui-sidebar__link">`)
		if it.Icon != "" {
			b.WriteString(`<span class="ui-sidebar__icon" aria-hidden="true">` + string(it.Icon) + `</span>`)
		}
		b.WriteString(`<span class="ui-sidebar__label">` + escText(it.Label) + `</span></summary>`)
		b.WriteString(`<ul class="ui-sidebar__sublist">`)
		for _, child := range it.Children {
			writeSidebarItem(b, child, currentPath, depth+1)
		}
		b.WriteString(`</ul></details>`)
	} else {
		linkAttrs := ` class="ui-sidebar__link"`
		if active {
			linkAttrs += ` aria-current="page"`
		}
		// Same idiom as ui.Menu items: neutralise javascript:/vbscript:/
		// data: schemes to "#" before attribute-escaping.
		b.WriteString(`<a href="` + escAttr(sanitizeHref(it.Href)) + `"` + linkAttrs + `>`)
		if it.Icon != "" {
			b.WriteString(`<span class="ui-sidebar__icon" aria-hidden="true">` + string(it.Icon) + `</span>`)
		}
		b.WriteString(`<span class="ui-sidebar__label">` + escText(it.Label) + `</span></a>`)
	}
	b.WriteString(`</li>`)
}

// sidebarDrawerSlot renders the drawer's body — same content as the
// inline sidebar minus the hamburger button. Wraps in
// data-fui-comp="ui-sidebar" so the sidebar stylesheet applies
// inside the drawer too (the framework's per-component CSS scoping
// keys on that marker).
type sidebarDrawerSlot struct{ cfg SidebarConfig }

// RenderCtx renders the drawer body with role-filtered items. The widget host
// serves the drawer chrome per-request (serveChrome) and threads the request
// context here, so the mobile drawer hides the same role-gated entries the
// desktop sidebar does.
func (s sidebarDrawerSlot) RenderCtx(ctx context.Context) render.HTML {
	return sidebarDrawerSlot{cfg: s.cfg.withFilteredItems(ctx)}.Render()
}

func (s sidebarDrawerSlot) Render() render.HTML {
	return sidebarStyle.WrapHTML(render.HTML(
		`<div class="ui-sidebar ui-sidebar--drawer-body">` +
			string(sidebarBody(s.cfg)) +
			`</div>`,
	))
}

// MountSidebar registers BOTH the sidebar drawer widget (for < md
// viewports) AND mounts it on r. Returns the widget definition. Call
// once per app at startup. The same SidebarConfig is passed to
// `Sidebar(cfg)` when rendering screens so the two views stay in
// sync.
//
// Generic signature: `r` is anything widget.Mount accepts (the
// gofastr router). We use a tiny adapter type so this package doesn't
// need to import the router directly.
func MountSidebar(r WidgetMounter, cfg SidebarConfig, pages ...string) widget.Definition {
	if cfg.Variant == "" {
		cfg.Variant = SidebarPersistent
	}
	if cfg.DrawerName == "" {
		cfg.DrawerName = "ui-sidebar-drawer"
	}
	b := preset.Drawer(cfg.DrawerName).
		Hidden().
		Slot("body", sidebarDrawerSlot{cfg: cfg})
	// Optional page scoping — apps that only use the sidebar on a
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

// hasActiveDescendant returns true when any descendant of it matches
// currentPath via the same rules used at the leaf level (MatchPath
// prefix, or exact Href).
func hasActiveDescendant(it SidebarItem, currentPath string) bool {
	if currentPath == "" {
		return false
	}
	for _, c := range it.Children {
		if c.Active {
			return true
		}
		if c.MatchPath != "" && strings.HasPrefix(currentPath, c.MatchPath) {
			return true
		}
		if c.Href != "" && currentPath == c.Href {
			return true
		}
		if hasActiveDescendant(c, currentPath) {
			return true
		}
	}
	return false
}

func sidebarCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-sidebar"].ui-sidebar {
  display: contents;
}
[data-fui-comp="ui-sidebar"] .ui-sidebar__hamburger {
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
[data-fui-comp="ui-sidebar"] .ui-sidebar__inline {
  display: grid;
  gap: var(--spacing-md, 8px);
  padding: var(--spacing-lg, 16px);
  min-width: 220px;
}
[data-fui-comp="ui-sidebar"] .ui-sidebar__title {
  font-size: var(--text-sm, 0.875rem);
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--color-text-muted, #52525B);
  margin: 0;
}
[data-fui-comp="ui-sidebar"] .ui-sidebar__list,
[data-fui-comp="ui-sidebar"] .ui-sidebar__sublist {
  list-style: none;
  padding: 0;
  margin: 0;
  display: grid;
  gap: var(--spacing-xs, 2px);
}
[data-fui-comp="ui-sidebar"] .ui-sidebar__sublist {
  margin-inline-start: var(--spacing-lg, 16px);
}
[data-fui-comp="ui-sidebar"] .ui-sidebar__link {
  display: flex;
  align-items: center;
  gap: var(--spacing-sm, 8px);
  padding: var(--spacing-sm, 8px) var(--spacing-md, 12px);
  border-radius: var(--radii-sm, 4px);
  color: var(--color-text, #18181B);
  text-decoration: none;
  min-height: var(--spacing-touch-target, 44px);
  cursor: pointer;
}
[data-fui-comp="ui-sidebar"] .ui-sidebar__link:hover,
[data-fui-comp="ui-sidebar"] .ui-sidebar__link:focus-visible {
  background: var(--color-surface-soft, #F4F4F5);
}
[data-fui-comp="ui-sidebar"] .ui-sidebar__link:focus-visible {
  /* Visible focus ring on BOTH the default and the active
     (primary-background) link. The previous background-only signal was
     invisible on the active link: the [aria-current="page"] rule below
     (equal specificity, later source) overrode the focus background, and
     outline:none removed the ring — so a keyboard user could not see
     focus land on the current page's nav item. */
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}
[data-fui-comp="ui-sidebar"] .ui-sidebar__link[aria-current="page"] {
  /* Use the primary + primary-fg token pair so contrast is guaranteed
     AA regardless of theme. The previous 12%-primary tinted bg + raw
     primary text failed contrast for some primary hues. */
  background: var(--color-primary, #4F46E5);
  color: var(--color-primary-fg, #FFFFFF);
  font-weight: 600;
}
[data-fui-comp="ui-sidebar"] .ui-sidebar__group > summary {
  list-style: none;
}
[data-fui-comp="ui-sidebar"] .ui-sidebar__group > summary::-webkit-details-marker {
  display: none;
}
[data-fui-comp="ui-sidebar"] .ui-sidebar__footer {
  margin-top: auto;
  padding-top: var(--spacing-md, 12px);
  border-top: 1px solid var(--color-border, #E4E4E7);
}
/* Viewport behaviour: < md collapses to the hamburger; ≥ md the
   inline column appears and the hamburger hides. OffCanvas keeps the
   hamburger on every viewport.                                       */
@media (max-width: 47.99rem) {
  [data-fui-comp="ui-sidebar"] .ui-sidebar__inline { display: none; }
}
@media (min-width: 48rem) {
  [data-fui-comp="ui-sidebar"].ui-sidebar--persistent .ui-sidebar__hamburger,
  [data-fui-comp="ui-sidebar"].ui-sidebar--collapsible .ui-sidebar__hamburger {
    display: none;
  }
}`
}
