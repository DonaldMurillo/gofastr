package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// LinkVariant chooses the visual treatment of a Link.
type LinkVariant string

const (
	LinkInline LinkVariant = ""       // a link in prose, underlined
	LinkAction LinkVariant = "action" // a row action with a touch-target tap area
	LinkMuted  LinkVariant = "muted"  // a quiet "see all" link
	LinkTitle  LinkVariant = "title"  // a record's name heading a row or card
)

// LinkConfig configures a Link.
type LinkConfig struct {
	Href    string // required
	Text    string // required visible text
	Variant LinkVariant
	Class   string
	ID      string
	// ExtraAttrs reach the <a>, data-cui-* wiring included; class, id,
	// href, on* handlers and control-byte values are dropped.
	ExtraAttrs html.Attrs
}

// Link renders an anchor with a typed variant; an unsafe Href becomes "#".
func Link(cfg LinkConfig) render.HTML {
	if cfg.Href == "" {
		panic("ui: Link requires Href")
	}
	if cfg.Text == "" {
		panic("ui: Link requires Text")
	}
	switch cfg.Variant {
	case LinkInline, LinkAction, LinkMuted, LinkTitle:
	default:
		panic("ui: Link unknown Variant " + string(cfg.Variant) +
			`. Pick one of: "" (inline), action, muted, title`)
	}
	cls := "fui-link"
	if cfg.Variant != LinkInline {
		cls += " fui-link--" + string(cfg.Variant)
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	href := urlsafe.CleanAnchor(cfg.Href)
	if href == "" {
		href = "#"
	}
	return linkStyle.WrapHTML(html.Link(html.LinkConfig{
		Href:       href,
		Text:       cfg.Text,
		Class:      cls,
		ID:         cfg.ID,
		ExtraAttrs: html.SafeCarrierAttrs(scrubAttrs(cfg.ExtraAttrs), "href"),
	}))
}

var linkStyle = registry.RegisterStyle("ui-link", linkCSS)

func linkCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-link"], .fui-link {
  color: var(--color-primary);
  text-decoration: none;
  font-weight: var(--font-weight-medium);
  cursor: pointer;
}
[data-cui-comp="ui-link"]:hover, .fui-link:hover { text-decoration: underline; }
/* An inline link sits in prose, where the neutral primary is the text's
   own colour: the underline is what tells it from its sentence (WCAG
   1.4.1). Action, muted and title links sit outside prose and stay bare. */
:where([data-cui-comp="ui-link"], .fui-link):not(.fui-link--action, .fui-link--muted, .fui-link--title) {
  text-decoration: underline;
  text-underline-offset: 0.2em;
}
[data-cui-comp="ui-link"]:focus-visible, .fui-link:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
  border-radius: var(--radii-sm, 6px);
}

/* Action variant — 44×44 tap target so the link can sit beside a
   Button in a row action without violating WCAG 2.5.5. */
.fui-link--action {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-block-size: var(--spacing-touch-target);
  min-inline-size: var(--spacing-touch-target);
  padding: 0 var(--spacing-xs, 2px);
}

/* Muted — quieter affordance ("see all", "view details") that doesn't
   compete with primary CTAs. */
.fui-link--muted { color: var(--color-text-muted); font-weight: var(--font-weight-normal); }
.fui-link--muted:hover { color: var(--color-text); }

/* Title — a record's name heading a row or card. */
.fui-link--title { color: var(--color-text); font-weight: var(--font-weight-semibold, 600); }`
}
