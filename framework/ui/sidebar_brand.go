package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── SidebarBrand ──────────────────────────────────────────────────
//
// The product mark at the head of a sidebar: a square logo tile (the
// logo image, or the name's initial on the inverted surface), the name
// in semibold and an optional muted line under it ("Back office", a
// workspace's plan). Give it to SidebarConfig.Prepend through a
// component so the drawer body carries it too.

// SidebarBrandConfig configures a SidebarBrand.
type SidebarBrandConfig struct {
	// Name is the product or workspace name. Required.
	Name string
	// Sub is the muted line under the name. Optional.
	Sub string
	// Logo is the tile's image URL. Empty draws the name's initial.
	Logo string
	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers) to the root element. Keys the component owns
	// are dropped: class, id and data-cui-*.
	ExtraAttrs html.Attrs
}

// SidebarBrand renders the brand block.
func SidebarBrand(cfg SidebarBrandConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: SidebarBrand requires Name")
	}
	text := []render.HTML{html.Span(html.TextConfig{Class: "fui-sidebar-brand__name"}, render.Text(cfg.Name))}
	if cfg.Sub != "" {
		text = append(text, html.Span(html.TextConfig{Class: "fui-sidebar-brand__sub"}, render.Text(cfg.Sub)))
	}
	internal := html.Attrs{"data-cui-internal": ""}
	return sidebarBrandStyle.WrapHTML(html.Div(html.DivConfig{Class: "fui-sidebar-brand",
		ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs)},
		html.Span(html.TextConfig{Class: "fui-sidebar-brand__tile",
			ExtraAttrs: html.Attrs{"aria-hidden": "true", "data-cui-internal": ""}},
			Avatar(AvatarConfig{Name: cfg.Name, Src: cfg.Logo, Square: true})),
		html.Span(html.TextConfig{Class: "fui-sidebar-brand__text", ExtraAttrs: internal}, text...)))
}

var sidebarBrandStyle = registry.RegisterStyle("ui-sidebar-brand", sidebarBrandCSS)

// Knob: --ui-sidebar-brand-tile (1.75rem) is the logo tile's square.
func sidebarBrandCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-sidebar-brand"] {
  display: flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
  min-inline-size: 0;
}
[data-cui-comp="ui-sidebar-brand"] .fui-sidebar-brand__tile {
  display: inline-flex;
  --ui-avatar-size: var(--ui-sidebar-brand-tile, 1.75rem);
  --ui-avatar-bg: var(--color-text, #09090B);
  --ui-avatar-fg: var(--color-background, #FFF);
}
[data-cui-comp="ui-sidebar-brand"] .fui-sidebar-brand__text { display: flex; flex-direction: column; min-inline-size: 0; line-height: var(--leading-tight, 1.2); }
[data-cui-comp="ui-sidebar-brand"] .fui-sidebar-brand__name { font-size: var(--text-sm, 0.875rem); font-weight: var(--font-weight-semibold); color: var(--color-text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
[data-cui-comp="ui-sidebar-brand"] .fui-sidebar-brand__sub { font-size: var(--text-xs, 0.75rem); color: var(--color-text-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
/* In a collapsed sidebar rail only the tile shows, centred; the name
   is clipped, not removed, so assistive tech still reads it. */
[data-cui-comp="ui-sidebar"][data-collapsed="true"] .fui-sidebar__inline [data-cui-comp="ui-sidebar-brand"] { justify-content: center; }
[data-cui-comp="ui-sidebar"][data-collapsed="true"] .fui-sidebar__inline [data-cui-comp="ui-sidebar-brand"] .fui-sidebar-brand__text {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
}
`
}
