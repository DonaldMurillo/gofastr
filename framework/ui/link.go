package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// LinkVariant chooses the visual treatment of a Link.
type LinkVariant string

const (
	// LinkInline is a normal in-flow text link — primary-colored, hover
	// underline, no min-height. Use this for links in prose.
	LinkInline LinkVariant = ""
	// LinkAction is a row-action / list-item link — 44×44 tap target
	// (WCAG 2.5.5) and inline-flex centering. Use this when the link
	// sits in a table row, list, or toolbar alongside Button siblings.
	LinkAction LinkVariant = "action"
	// LinkMuted is a subdued text-muted link — for "see all", "view
	// details" affordances that should not compete with primary CTAs.
	LinkMuted LinkVariant = "muted"
)

// LinkConfig configures a Link.
type LinkConfig struct {
	Href    string // required
	Text    string // required visible text
	Variant LinkVariant
	Class   string
	ID      string
	Attrs   html.Attrs
}

// Link renders an anchor with a typed variant. The component owns its
// CSS — the .ui-link class works without any app-level overrides.
//
// Defaults to LinkInline. Picking LinkAction gives the link a 44×44
// minimum tap area so it can stand next to a Button in a row action
// without violating WCAG 2.5.5.
func Link(cfg LinkConfig) render.HTML {
	if cfg.Href == "" {
		panic("ui: Link requires Href")
	}
	if cfg.Text == "" {
		panic("ui: Link requires Text")
	}
	switch cfg.Variant {
	case LinkInline, LinkAction, LinkMuted:
		// recognized
	default:
		panic("ui: Link unknown Variant " + string(cfg.Variant) +
			` — pick one of: "" (inline), action, muted`)
	}
	cls := "ui-link"
	if cfg.Variant != LinkInline {
		cls += " ui-link--" + string(cfg.Variant)
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	return linkStyle.WrapHTML(html.Link(html.LinkConfig{
		Href:  cfg.Href,
		Text:  cfg.Text,
		Class: cls,
		ID:    cfg.ID,
		Attrs: cfg.Attrs,
	}))
}

var linkStyle = registry.RegisterStyle("ui-link", linkCSS)

func linkCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-link"], .ui-link {
  color: var(--color-primary);
  text-decoration: none;
  font-weight: 500;
  cursor: pointer;
}
[data-fui-comp="ui-link"]:hover, .ui-link:hover { text-decoration: underline; }
[data-fui-comp="ui-link"]:focus-visible, .ui-link:focus-visible {
  outline: 2px solid var(--color-primary);
  outline-offset: 2px;
  border-radius: var(--radii-sm, 4px);
}

/* Action variant — 44×44 tap target so the link can sit beside a
   Button in a row action without violating WCAG 2.5.5. */
.ui-link--action {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-block-size: var(--spacing-touch-target);
  min-inline-size: var(--spacing-touch-target);
  padding: 0 var(--spacing-xs, 6px);
}

/* Muted — quieter affordance ("see all", "view details") that doesn't
   compete with primary CTAs. */
.ui-link--muted { color: var(--color-text-muted); font-weight: 400; }
.ui-link--muted:hover { color: var(--color-text); }`
}
