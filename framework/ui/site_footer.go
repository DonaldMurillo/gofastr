package ui

// SiteFooter — multi-column credits grid + optional bottom strip. The
// column model is intentionally simple: a brand-style intro on the
// left, then any number of link columns. A bottom strip below holds
// copyright + small text. Composes via core-ui/html primitives so a
// CSP-strict app gets escaping for free.

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// SiteFooterLink is one link inside a footer column.
type SiteFooterLink struct {
	Label    string
	Href     string
	External bool
}

// SiteFooterColumn is one labelled link list.
type SiteFooterColumn struct {
	Title string
	Links []SiteFooterLink
}

// SiteFooterConfig configures a SiteFooter.
type SiteFooterConfig struct {
	// Lead is the optional left-most slot (brand mark + tagline).
	// If empty, the grid is just columns.
	Lead render.HTML
	// Columns are the labelled link lists rendered after Lead.
	Columns []SiteFooterColumn
	// Bottom is the optional small-text strip below the grid (e.g.,
	// copyright + colophon). Children render in a horizontal cluster.
	Bottom []render.HTML
	// Class is appended to the ui-site-footer wrapper.
	Class string
}

// SiteFooter renders a multi-column footer. The wrapper is a <div>
// (not <footer>) because the framework Layout already wraps component
// output in <footer role="contentinfo">.
func SiteFooter(cfg SiteFooterConfig) render.HTML {
	gridChildren := []render.HTML{}
	if cfg.Lead != "" {
		gridChildren = append(gridChildren, html.Div(html.DivConfig{Class: "ui-site-footer__lead"}, cfg.Lead))
	}
	for _, c := range cfg.Columns {
		items := make([]render.HTML, 0, len(c.Links))
		for _, l := range c.Links {
			extra := html.Attrs{}
			if l.External {
				extra["rel"] = "external"
				extra["target"] = "_blank"
			}
			items = append(items, html.ListItem(html.ListItemConfig{},
				html.Link(html.LinkConfig{Href: l.Href, Text: l.Label, ExtraAttrs: extra}),
			))
		}
		col := html.Div(html.DivConfig{Class: "ui-site-footer__col"},
			render.Tag("p", map[string]string{"class": "ui-site-footer__col-title"}, render.Text(c.Title)),
			html.UnorderedList(html.ListConfig{}, items...),
		)
		gridChildren = append(gridChildren, col)
	}
	grid := html.Div(html.DivConfig{Class: "ui-site-footer__grid"}, gridChildren...)

	body := []render.HTML{grid}
	if len(cfg.Bottom) > 0 {
		body = append(body, html.Div(html.DivConfig{Class: "ui-site-footer__bottom"}, cfg.Bottom...))
	}

	cls := "ui-site-footer"
	if cfg.Class != "" {
		cls = cls + " " + cfg.Class
	}
	return siteFooterStyle.WrapHTML(html.Div(html.DivConfig{Class: cls}, body...))
}

var siteFooterStyle = registry.RegisterStyle("ui-site-footer", siteFooterCSS)

func siteFooterCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-site-footer"] {
  display: block;
  padding-block: var(--spacing-2xl, 48px);
  padding-inline: var(--spacing-lg, 24px);
  border-block-start: 1px solid var(--color-border, rgba(0,0,0,0.1));
}
[data-fui-comp="ui-site-footer"] .ui-site-footer__grid {
  display: grid;
  /* Default: as many auto-sized columns as fit, min 180px each. Hosts
     wanting a fixed N-column layout (5-col GoFastr-style, etc.) set
     --ui-site-footer-grid-template to override. */
  grid-template-columns: var(--ui-site-footer-grid-template, repeat(auto-fit, minmax(180px, 1fr)));
  gap: var(--ui-site-footer-grid-gap, var(--spacing-xl, 32px));
  /* Hosts that center the footer at a fixed measure set
     --ui-site-footer-max-width; default is the full inline space. */
  max-inline-size: var(--ui-site-footer-max-width, none);
  margin-inline: auto;
  margin-block-end: var(--spacing-xl, 32px);
}
[data-fui-comp="ui-site-footer"] .ui-site-footer__lead {
  /* Default span is 1 cell. Sites that want a wider, marketing-y
     lead column can override with .ui-site-footer__lead {
     grid-column: span 2 } in their app.css. */
  grid-column: auto;
}
[data-fui-comp="ui-site-footer"] .ui-site-footer__col-title {
  margin: 0 0 var(--spacing-sm, 8px);
  font-size: var(--text-xs, 11px);
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--ui-site-footer-title-color, var(--color-text-subtle, currentColor));
}
[data-fui-comp="ui-site-footer"] ul {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: var(--spacing-xs, 4px);
}
[data-fui-comp="ui-site-footer"] li a {
  color: currentColor;
  text-decoration: none;
  font-size: var(--text-sm, 13px);
  line-height: 1.6;
}
[data-fui-comp="ui-site-footer"] li a:hover,
[data-fui-comp="ui-site-footer"] li a:focus-visible {
  text-decoration: underline;
  text-underline-offset: 3px;
}
[data-fui-comp="ui-site-footer"] .ui-site-footer__bottom {
  display: flex;
  flex-wrap: wrap;
  gap: var(--spacing-md, 16px);
  justify-content: space-between;
  padding-block-start: var(--spacing-lg, 24px);
  border-block-start: 1px solid var(--color-border, rgba(0,0,0,0.1));
  color: var(--ui-site-footer-bottom-color, var(--color-text-subtle, currentColor));
  font-size: var(--text-xs, 12px);
}`
}
