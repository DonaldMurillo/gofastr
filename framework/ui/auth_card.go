package ui

// AuthCard — a centered, narrow card for auth surfaces (sign in, create
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
	// Body is the card contents — typically a ui.Form.
	Body render.HTML
	// Footer is an optional row below the body, e.g. a "Create an
	// account" link.
	Footer render.HTML
	Class  string
}

// AuthCard renders a centered, constrained auth card.
func AuthCard(cfg AuthCardConfig) render.HTML {
	inner := make([]render.HTML, 0, 3)
	if cfg.Title != "" {
		inner = append(inner, html.Heading(html.HeadingConfig{Level: 1, Class: "ui-auth-card__title"}, render.Text(cfg.Title)))
	}
	if cfg.Body != "" {
		inner = append(inner, cfg.Body)
	}
	if cfg.Footer != "" {
		inner = append(inner, html.Div(html.DivConfig{Class: "ui-auth-card__footer"}, cfg.Footer))
	}
	cls := "ui-auth-card"
	if cfg.Class != "" {
		cls = cls + " " + cfg.Class
	}
	panel := html.Div(html.DivConfig{Class: "ui-auth-card__panel"}, inner...)
	return authCardStyle.WrapHTML(html.Div(html.DivConfig{Class: cls}, panel))
}

var authCardStyle = registry.RegisterStyle("ui-auth-card", authCardCSS)

func authCardCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-auth-card"] {
  display: flex;
  justify-content: center;
  padding-block: clamp(24px, 6vw, 64px);
}
[data-fui-comp="ui-auth-card"] .ui-auth-card__panel {
  inline-size: 100%;
  max-inline-size: 24rem;
  display: flex;
  flex-direction: column;
  gap: var(--spacing-md, 16px);
  padding: clamp(20px, 5vw, 32px);
  background: var(--color-surface, #fff);
  border: 1px solid var(--color-border, #e4e4e7);
  border-radius: var(--radius-lg, 12px);
  box-shadow: 0 1px 3px rgba(15, 23, 42, 0.06);
}
[data-fui-comp="ui-auth-card"] .ui-auth-card__title {
  margin: 0;
  font-family: var(--font-heading, inherit);
  font-size: 1.25rem;
  letter-spacing: -0.01em;
}
[data-fui-comp="ui-auth-card"] .ui-auth-card__footer {
  font-size: 0.875rem;
  color: var(--color-text-muted, inherit);
}
[data-fui-comp="ui-auth-card"] .ui-auth-card__footer a {
  color: var(--color-primary, #4f46e5);
  text-decoration: underline;
  text-underline-offset: 2px;
}
`
}
