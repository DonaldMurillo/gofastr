package ui

// StatusPill — a small, non-interactive status kicker: an optional leading
// dot plus a short mono label in a rounded pill ("● Get started · v0.0.4").
//
// Distinct from the two neighbours it sits between:
//   - StatusBadge  — status-coded label, no dot, sentence case.
//   - Tag / Chip   — interactive (dismissible / filter link).
//
// StatusPill is purely presentational: a hero kicker, a "pre-alpha" marker,
// a live-state caption. Two tones — neutral and accent (brand primary, with
// a softly glowing dot).

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// StatusPillTone selects the colour treatment of a StatusPill.
type StatusPillTone string

const (
	// StatusPillNeutral is the muted default — subtle text on the surface.
	StatusPillNeutral StatusPillTone = ""
	// StatusPillAccent uses the brand primary colour with a glowing dot.
	StatusPillAccent StatusPillTone = "accent"
)

// StatusPillConfig configures a StatusPill.
type StatusPillConfig struct {
	Label string         // required visible text
	Tone  StatusPillTone // default StatusPillNeutral
	// Dot adds a leading status dot. Opt-in.
	Dot   bool
	Class string
	ID    string
}

// StatusPill renders a presentational status kicker.
func StatusPill(cfg StatusPillConfig) render.HTML {
	if cfg.Label == "" {
		panic("ui: StatusPill requires Label")
	}
	switch cfg.Tone {
	case StatusPillNeutral, StatusPillAccent:
	default:
		panic("ui: StatusPill unknown Tone " + string(cfg.Tone) +
			` — pick "" (neutral) or "accent"`)
	}
	cls := "ui-status-pill"
	if cfg.Tone != StatusPillNeutral {
		cls += " ui-status-pill--" + string(cfg.Tone)
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	children := []render.HTML{}
	if cfg.Dot {
		children = append(children, html.Span(html.TextConfig{
			Class:      "ui-status-pill__dot",
			ExtraAttrs: html.Attrs{"aria-hidden": "true"},
		}))
	}
	children = append(children, render.Text(cfg.Label))
	return statusPillStyle.WrapHTML(
		html.Span(html.TextConfig{Class: cls, ID: cfg.ID}, children...))
}

var statusPillStyle = registry.RegisterStyle("ui-status-pill", statusPillCSS)

func statusPillCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-status-pill"] {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 3px 10px;
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: var(--text-xs, 11px);
  white-space: nowrap;
  color: var(--color-text-muted, #52525B);
  background: var(--color-surface, transparent);
  border: 1px solid var(--ui-status-pill-border, var(--color-border, rgba(0,0,0,0.1)));
  border-radius: var(--radius-full, 999px);
}
[data-fui-comp="ui-status-pill"] .ui-status-pill__dot {
  width: 6px;
  height: 6px;
  border-radius: 999px;
  background: var(--color-text-subtle, currentColor);
}
[data-fui-comp="ui-status-pill"].ui-status-pill--accent {
  color: var(--color-primary, currentColor);
  border-color: var(--ui-status-pill-accent-border, var(--color-primary, currentColor));
  background: var(--ui-status-pill-accent-bg, color-mix(in oklch, var(--color-primary, currentColor) 8%, var(--color-surface, transparent)));
}
[data-fui-comp="ui-status-pill"].ui-status-pill--accent .ui-status-pill__dot {
  background: var(--color-primary, currentColor);
  box-shadow: 0 0 6px var(--color-primary, currentColor);
}`
}
