package ui

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ListDetailConfig configures the two panes of a ListDetail.
type ListDetailConfig struct {
	// List is kept chrome in a group layout, not a per-route outlet.
	List render.HTML
	// ListLabel labels the independently scrolling list. Required.
	ListLabel string
	// Detail is the group's primary slot, replaced on detail navigation.
	Detail render.HTML
	// MobileSinglePane shows either list or detail on phones instead of
	// stacking them. Render ListDetailPlaceholder for the index screen.
	MobileSinglePane bool
	// BackHref adds a link back to the list at the top of the detail
	// pane. It shows only while the detail pane is alone: a phone, with
	// a selected detail (not ListDetailPlaceholder). It sits outside
	// Detail, so it survives detail navigation. Requires MobileSinglePane.
	BackHref string
	// BackLabel is the back link's text. Default "Back".
	BackLabel string
	// ExtraAttrs forwards safe data-* and ARIA attributes to the root.
	// LayoutTree.VTRegion's two transition attributes are also accepted.
	ExtraAttrs html.Attrs
}

// ListDetail places a scrolling list beside a detail region, stacked on
// phones. Keeping it in a group layout preserves the list's DOM and scroll
// while navigation replaces Detail. No pane lifecycle or JavaScript is added.
//
// A list/detail page wants the row's whole content column, and the
// component cannot take it: it fills 100% of its container, and the width
// comes from the content row around it. Leave `ContentRowConfig.Aside`
// unfilled on screens that don't need it (an empty aside outlet releases
// its column). `ContentRowConfig.Viewport` (the tracker's spelling) makes
// the panes scroll on their own; it does not change their width. A sidebar
// plus an aside leaves the detail pane roughly 300px wide at 1280px.
func ListDetail(cfg ListDetailConfig) render.HTML {
	if cfg.ListLabel == "" {
		panic("ui: ListDetail requires ListLabel")
	}
	attrs := html.SafeCarrierAttrs(cfg.ExtraAttrs)
	for key := range attrs {
		if lk := strings.ToLower(key); (strings.HasPrefix(lk, "data-cui-") || strings.HasPrefix(lk, "data-fui-")) && key != "data-cui-vt" && key != "data-cui-vt-when" {
			delete(attrs, key)
		}
	}
	if cfg.BackHref != "" && !cfg.MobileSinglePane {
		panic("ui: ListDetail BackHref requires MobileSinglePane: the back link only exists when a phone shows the detail pane alone")
	}
	class := "fui-list-detail"
	if cfg.MobileSinglePane {
		class += " fui-list-detail--single-mobile"
	}
	// back is built entirely from BackHref/BackLabel through LinkButton,
	// never caller markup, so it always marks itself as this
	// component's own — except when Detail is empty, when the detail
	// pane around it holds nothing else either and takes the one mark
	// as a whole instead.
	var back render.HTML
	if cfg.BackHref != "" {
		label := cfg.BackLabel
		if label == "" {
			label = "Back"
		}
		backAttrs := html.Attrs{}
		if cfg.Detail != "" {
			backAttrs["data-cui-internal"] = ""
		}
		back = html.Div(html.DivConfig{Class: "fui-list-detail__back", ExtraAttrs: backAttrs}, LinkButton(LinkButtonConfig{
			Href: cfg.BackHref, Label: label, Variant: ButtonGhost, Icon: "chevron-left",
		}))
	}
	listAttrs := map[string]string{"class": "fui-list-detail__list", "aria-label": cfg.ListLabel, "tabindex": "0"}
	if cfg.List == "" {
		listAttrs["data-cui-internal"] = ""
	}
	detailAttrs := html.Attrs{}
	if cfg.Detail == "" {
		detailAttrs["data-cui-internal"] = ""
	}
	return listDetailStyle.WrapHTML(html.Div(html.DivConfig{Class: class, ExtraAttrs: attrs},
		render.Tag("nav", listAttrs, cfg.List),
		html.Div(html.DivConfig{Class: "fui-list-detail__detail", ExtraAttrs: detailAttrs}, back, cfg.Detail),
	))
}

// ListDetailPlaceholder marks an unselected detail screen. A ListDetail
// using MobileSinglePane shows the list instead on phones; desktop keeps
// this content beside it. Missing-record screens should not use this marker.
func ListDetailPlaceholder(body render.HTML) render.HTML {
	return listDetailStyle.WrapHTML(html.Div(html.DivConfig{Class: "fui-list-detail__placeholder"}, body))
}

var listDetailStyle = registry.RegisterStyle("ui-list-detail", listDetailCSS)

func listDetailCSS(_ style.Theme) string {
	return `
.fui-list-detail { display: grid; grid-template-columns: minmax(0, var(--ui-list-detail-list-width, 20rem)) minmax(0, 1fr); min-inline-size: 0; gap: var(--spacing-lg); }
.fui-list-detail__list { min-inline-size: 0; max-block-size: var(--ui-list-detail-list-height, 70vh); overflow-y: auto; overscroll-behavior: contain; }
.fui-list-detail__detail { min-inline-size: 0; }
.fui-list-detail__back { display: none; margin-block-end: var(--spacing-md); }
@media (max-width: 47.99rem) {
  .fui-list-detail--single-mobile > .fui-list-detail__detail > .fui-list-detail__back { display: block; }
  .fui-list-detail { grid-template-columns: minmax(0, 1fr); }
  .fui-list-detail__detail { order: -1; }
  .fui-list-detail--single-mobile:has(> .fui-list-detail__detail .fui-list-detail__placeholder) > .fui-list-detail__detail { display: none; }
  .fui-list-detail--single-mobile:not(:has(> .fui-list-detail__detail .fui-list-detail__placeholder)) > .fui-list-detail__list { display: none; }
}
/* A viewport row confines scrolling to the panes and pads its cells;
  the layout cell a group layer renders inside the DETAIL pane carries no
  row padding — this pane owns its own insets. The condition keys the
  row's public --viewport modifier (ContentRowConfig.Viewport), the styled
  element is this component's own subtree. Lived in the page shell's
  sheet before, keyed on this component's pane class from outside. */
.fui-content-row--viewport .fui-list-detail__detail > .layout-content { padding: 0; gap: 0; }
`
}
