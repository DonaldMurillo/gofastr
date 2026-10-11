package ui

// StatusPill is a small, non-interactive status kicker: an optional leading
// dot plus a short mono label in a rounded pill ("● Get started · v0.0.4").
//
// Distinct from the two neighbours it sits between:
//   - StatusBadge:   status-coded label, no dot, sentence case.
//   - Tag / Chip:    interactive (dismissible / filter link).
//
// StatusPill is purely presentational: a hero kicker, a "pre-alpha" marker,
// a live-state caption. Two tones: neutral and accent (brand primary, with
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
	// StatusPillNeutral is the muted default: subtle text on the surface.
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
	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the pill's root element.
	// Keys the component owns are dropped: class and id (use Class /
	// ID) and data-cui-*.
	ExtraAttrs html.Attrs
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
			`. Pick "" (neutral) or "accent"`)
	}
	cls := "fui-status-pill"
	if cfg.Tone != StatusPillNeutral {
		cls += " fui-status-pill--" + string(cfg.Tone)
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	children := []render.HTML{}
	if cfg.Dot {
		children = append(children, html.Span(html.TextConfig{
			Class:      "fui-status-pill__dot",
			ExtraAttrs: html.Attrs{"aria-hidden": "true"},
		}))
	}
	children = append(children, render.Text(cfg.Label))
	return statusPillStyle.WrapHTML(
		html.Span(html.TextConfig{Class: cls, ID: cfg.ID, ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs)}, children...))
}

var statusPillStyle = registry.RegisterStyle("ui-status-pill", statusPillCSS)

func statusPillCSS(_ style.Theme) string {
	// Knobs: --ui-status-pill-dot-size (6px) sizes the status dot and
	// --ui-status-pill-case (none) sets the label's letter case (beside
	// the frame knobs below).
	return `[data-cui-comp="ui-status-pill"] {
  display: inline-flex;
  align-items: center;
  gap: calc(var(--spacing-sm, 4px) * 1.5);
  padding: var(--spacing-xs, 2px) calc(var(--spacing-sm, 4px) * 2.5);
  font-family: var(--ui-status-pill-font, inherit);
  font-size: var(--text-xs, 0.75rem);
  font-weight: var(--font-weight-medium);
  text-transform: var(--ui-status-pill-case, none);
  white-space: nowrap;
  color: var(--color-text-muted, #52525B);
  background: var(--color-surface, transparent);
  border: var(--stroke-thin, 1px) solid var(--ui-status-pill-border, var(--color-border, rgba(0,0,0,0.1)));
  border-radius: var(--radii-full, 9999px);
}
/* Author-origin display beats the UA's [hidden]{display:none}, so a
   pill a script hides with el.hidden = true would stay visible.
   hidden="until-found" is excluded: the UA keeps that state
   revealable (content-visibility), and display: none would break it. */
[data-cui-comp="ui-status-pill"][hidden]:not([hidden="until-found"]) {
  display: none;
}
[data-cui-comp="ui-status-pill"] .fui-status-pill__dot {
  width: var(--ui-status-pill-dot-size, 6px);
  height: var(--ui-status-pill-dot-size, 6px);
  border-radius: var(--radii-full, 9999px);
  background: var(--color-text-subtle, currentColor);
}
[data-cui-comp="ui-status-pill"].fui-status-pill--accent {
  /* Accent is carried by the dot and the full-strength text; the frame
     stays a hairline so a near-black primary never draws a heavy ring. */
  color: var(--color-text, currentColor);
  border-color: var(--ui-status-pill-accent-border, var(--color-border, rgba(0,0,0,0.1)));
  background: var(--ui-status-pill-accent-bg, var(--color-surface-soft, var(--color-surface, transparent)));
}
[data-cui-comp="ui-status-pill"].fui-status-pill--accent .fui-status-pill__dot {
  background: var(--color-primary, currentColor);
}`
}
