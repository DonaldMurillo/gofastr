package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── SegmentedLinks ────────────────────────────────────────────────
//
// A small strip of links where one is current, drawn as a segmented
// control: a list's Table / Cards switch. Each segment is a plain link,
// so the choice lives in the URL and the server draws the page in it;
// the current one carries aria-current="true". Icon-only segments keep
// their text as the accessible name.

// SegmentLink is one segment.
type SegmentLink struct {
	// Text names the segment (shown, or the name of an icon-only one).
	Text string
	// Href is the segment's destination. Required.
	Href string
	// Icon is a registered icon drawn before the text.
	Icon    string
	Current bool
}

// SegmentedLinksConfig configures the strip.
type SegmentedLinksConfig struct {
	// Label names the strip ("Layout"). Required.
	Label string
	Items []SegmentLink
	// IconOnly hides each segment's text visually.
	IconOnly bool
	// ExtraAttrs forwards attributes to the root.
	ExtraAttrs html.Attrs
	Ctx        context.Context
}

// SegmentedLinks renders the strip.
func SegmentedLinks(cfg SegmentedLinksConfig) render.HTML {
	if cfg.Label == "" || len(cfg.Items) == 0 {
		panic("ui: SegmentedLinks requires Label and Items")
	}
	kids := make([]render.HTML, 0, len(cfg.Items))
	for _, it := range cfg.Items {
		if it.Href == "" || it.Text == "" {
			panic("ui: SegmentedLinks item requires Text and Href")
		}
		attrs := map[string]string{"class": "fui-seglinks__item", "href": it.Href, "data-cui-internal": ""}
		if it.Current {
			attrs["aria-current"] = "true"
		}
		var body []render.HTML
		if it.Icon != "" {
			body = append(body, Icon(it.Icon, IconConfig{Size: "16"}))
		}
		if cfg.IconOnly {
			attrs["aria-label"] = it.Text
			attrs["title"] = it.Text
		} else {
			body = append(body, render.Text(it.Text))
		}
		kids = append(kids, render.Tag("a", attrs, body...))
	}
	root := map[string]string{}
	for k, v := range html.SafeExtraAttrs(cfg.ExtraAttrs, "role", "aria-label") {
		root[k] = v
	}
	root["class"] = "fui-seglinks"
	root["role"] = "group"
	root["aria-label"] = cfg.Label
	return segmentedLinksStyle.WrapHTML(render.Tag("div", root, kids...))
}

var segmentedLinksStyle = registry.RegisterStyle("ui-segmented-links", segmentedLinksCSS)

// segmentedLinksCSS draws the strip as the segmented control's pill: a
// soft track, the current segment raised on the surface.
func segmentedLinksCSS(_ style.Theme) string {
	return `:where([data-cui-comp="ui-segmented-links"]).fui-seglinks {
  display: inline-flex;
  gap: var(--spacing-xs, 2px);
  padding: var(--spacing-xs, 2px);
  border: var(--stroke-thin, 1px) solid var(--color-border);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface-soft);
}
[data-cui-comp="ui-segmented-links"] .fui-seglinks__item {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
  min-block-size: calc(var(--fui-density-control-h, 36px) - 2 * var(--spacing-xs, 2px) - 2 * var(--stroke-thin, 1px));
  padding-inline: var(--spacing-md, 8px);
  border-radius: calc(var(--radii-md, 8px) - var(--spacing-xs, 2px));
  color: var(--color-text-muted);
  text-decoration: none;
  font-size: var(--text-sm, 0.875rem);
}
[data-cui-comp="ui-segmented-links"] .fui-seglinks__item:hover { color: var(--color-text); }
[data-cui-comp="ui-segmented-links"] .fui-seglinks__item[aria-current="true"] {
  background: var(--color-surface);
  color: var(--color-text);
  box-shadow: var(--shadow-xs);
  font-weight: var(--font-weight-medium);
}`
}
