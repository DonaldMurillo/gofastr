package ui

// AuthCard is a centered, narrow card for auth surfaces (sign in, create
// account, reset password). It owns the centering, the constrained measure,
// the surface/border/shadow, and the title + footer-link slots, so auth
// screens compose a ui.Form into Body without shipping any layout CSS.

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// AuthCardConfig configures an AuthCard.
type AuthCardConfig struct {
	// Title is the card heading (e.g. "Sign in to Acme").
	Title string
	// Alert is an optional message shown above the body, typically a
	// failed-login notice. Empty renders nothing.
	Alert render.HTML
	// Body is the card contents, typically a ui.Form.
	Body render.HTML
	// Footer is an optional row below the body, e.g. a "Create an
	// account" link.
	Footer render.HTML
	Class  string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the card's root element.
	// Keys the component owns are dropped: class (use Class), id, and
	// data-cui-*.
	ExtraAttrs html.Attrs
}

// AuthCard renders a centered, constrained auth card.
func AuthCard(cfg AuthCardConfig) render.HTML {
	// The panel holds only the title when Alert, Body and Footer are
	// all empty — the whole panel is then the component's own — and
	// holds a slot the moment any of them arrives, so the boundary
	// moves down to the title alone and the panel is left for an owner
	// to reach.
	hasSlot := cfg.Alert != "" || cfg.Body != "" || cfg.Footer != ""
	var titleAttrs html.Attrs
	if hasSlot {
		titleAttrs = html.Attrs{"data-cui-internal": ""}
	}
	inner := make([]render.HTML, 0, 4)
	if cfg.Title != "" {
		inner = append(inner, html.Heading(html.HeadingConfig{Level: 1, Class: "fui-auth-card__title", ExtraAttrs: titleAttrs}, render.Text(cfg.Title)))
	}
	if cfg.Alert != "" {
		inner = append(inner, html.Div(html.DivConfig{Class: "fui-auth-card__alert", Role: "alert"}, cfg.Alert))
	}
	if cfg.Body != "" {
		inner = append(inner, cfg.Body)
	}
	if cfg.Footer != "" {
		inner = append(inner, html.Div(html.DivConfig{Class: "fui-auth-card__footer"}, cfg.Footer))
	}
	cls := "fui-auth-card"
	if cfg.Class != "" {
		cls = cls + " " + cfg.Class
	}
	var panelAttrs html.Attrs
	if !hasSlot {
		panelAttrs = html.Attrs{"data-cui-internal": ""}
	}
	panel := html.Div(html.DivConfig{Class: "fui-auth-card__panel", ExtraAttrs: panelAttrs}, inner...)
	return authCardStyle.WrapHTML(html.Div(html.DivConfig{
		Class: cls, ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs),
	}, panel))
}

var authCardStyle = registry.RegisterStyle("ui-auth-card", authCardCSS)

func authCardCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-auth-card"] {
  display: flex;
  justify-content: center;
  padding-block: clamp(24px, 6vw, 64px);
}
[data-cui-comp="ui-auth-card"] .fui-auth-card__panel {
  inline-size: 100%;
  max-inline-size: 24rem;
  display: flex;
  flex-direction: column;
  gap: var(--spacing-xl, 24px);
  padding: var(--spacing-xl, 24px);
  background: var(--color-surface, #FFFFFF);
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-xl, 14px);
  box-shadow: var(--shadow-sm);
}
[data-cui-comp="ui-auth-card"] .fui-auth-card__title {
  margin: 0;
  font-family: var(--font-heading, inherit);
  font-size: var(--text-xl, 1.25rem);
  font-weight: var(--font-weight-semibold);
  line-height: 1.3;
  letter-spacing: -0.02em;
}
[data-cui-comp="ui-auth-card"] .fui-auth-card__alert {
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-danger, #DC2626);
  background: color-mix(in srgb, var(--color-danger, #DC2626) 8%, transparent);
  border: var(--stroke-thin, 1px) solid color-mix(in srgb, var(--color-danger, #DC2626) 28%, transparent);
  border-radius: var(--radii-lg, 10px);
  padding: 0.625rem 0.75rem;
}
[data-cui-comp="ui-auth-card"] .fui-auth-card__footer {
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text-muted, #52525B);
  text-align: center;
}
[data-cui-comp="ui-auth-card"] .fui-auth-card__footer a {
  color: var(--color-text, #09090B);
  text-decoration: underline;
  text-underline-offset: 4px;
}
`
}
