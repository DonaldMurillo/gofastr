package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── Breadcrumbs ───────────────────────────────────────────────────
//
// Renders headless.Breadcrumbs dressed with the fui-breadcrumbs class
// map: a labelled <nav> whose ordered list walks the hierarchy to the
// page, the last step marked aria-current="page" and rendered as text.
// No script, and none needed: the trail is server-rendered and the
// separators are aria-hidden so assistive tech reads the order alone.

// Crumb is one step in the breadcrumb trail.
type Crumb struct {
	// Text is the step's visible label. Required.
	Text string
	// Href is the step's destination; empty on the last step.
	Href string
	// Current marks the step as the current page whatever its Href.
	Current bool
}

// BreadcrumbsConfig configures the trail.
type BreadcrumbsConfig struct {
	// Items are the steps, shallowest first.
	Items []Crumb

	// Label is the nav landmark's accessible name. Defaults to
	// "Breadcrumb", resolved per request through StringsFor when a
	// translator is on the context.
	Label string

	ID    string
	Class string
	// ExtraAttrs forwards additional attributes to the <nav> root.
	// Keys the component owns are dropped: class and id (use Class /
	// ID) and aria-label (use Label).
	ExtraAttrs html.Attrs

	// Ctx resolves the Strings table through the request's translator.
	Ctx context.Context
}

// Breadcrumbs renders the trail. Crumbs may be passed variably or
// through Items; both land in the same ordered list.
func Breadcrumbs(cfg BreadcrumbsConfig, crumbs ...Crumb) render.HTML {
	items := make([]headless.Breadcrumb, 0, len(cfg.Items)+len(crumbs))
	for _, c := range cfg.Items {
		items = append(items, headless.Breadcrumb{Text: c.Text, Href: c.Href, Current: c.Current})
	}
	for _, c := range crumbs {
		items = append(items, headless.Breadcrumb{Text: c.Text, Href: c.Href, Current: c.Current})
	}
	classes := headless.Classes{
		headless.PartRoot:                "fui-breadcrumbs",
		headless.PartBreadcrumbList:      "fui-breadcrumbs__list",
		headless.PartBreadcrumbItem:      "fui-breadcrumbs__item",
		headless.PartBreadcrumbLink:      "fui-breadcrumbs__link",
		headless.PartBreadcrumbSeparator: "fui-breadcrumbs__sep",
	}
	if cfg.Class != "" {
		classes[headless.PartRoot] += " " + cfg.Class
	}
	nav := headless.Breadcrumbs(headless.BreadcrumbsProps{
		Label:      cfg.Label,
		Items:      items,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id", "aria-label"),
		Strings:    StringsFor(cfg.Ctx),
	}, classes)
	return breadcrumbsStyle.WrapHTML(nav)
}

var breadcrumbsStyle = registry.RegisterStyle("ui-breadcrumbs", breadcrumbsCSS)

func breadcrumbsCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-breadcrumbs"] .fui-breadcrumbs__list {
  display: flex;
  flex-wrap: wrap;
  gap: var(--spacing-sm, 4px);
  list-style: none;
  margin: 0;
  padding: 0;
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text-muted, #6B7280);
}
[data-fui-comp="ui-breadcrumbs"] .fui-breadcrumbs__item {
  display: flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
}
[data-fui-comp="ui-breadcrumbs"] .fui-breadcrumbs__sep {
  color: var(--color-text-muted, #6B7280);
  opacity: 0.5;
}
[data-fui-comp="ui-breadcrumbs"] .fui-breadcrumbs__link {
  color: var(--color-text-muted, #6B7280);
  text-decoration: none;
}
[data-fui-comp="ui-breadcrumbs"] .fui-breadcrumbs__link:hover {
  color: var(--color-primary, #4F46E5);
  text-decoration: underline;
}
[data-fui-comp="ui-breadcrumbs"] .fui-breadcrumbs__link[aria-current="page"] {
  color: var(--color-text, #1F2937);
  font-weight: 600;
  text-decoration: none;
}`
}
