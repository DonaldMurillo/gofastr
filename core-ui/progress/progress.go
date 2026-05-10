// Package progress provides a thin wrapper around the native <progress>
// element with theme-aware styling.
//
// Two variants:
//
//   - Determinate — Value/Max are set; bar fills proportionally.
//   - Indeterminate — Value < 0; the browser shows its native animated
//     "in progress" stripe.
//
// The component is server-rendered and does not require JavaScript.
// Drive updates from the server via a signal binding (data-fui-signal-attr=value)
// when you want a live progress bar without a page reload.
package progress

import (
	"fmt"

	"github.com/gofastr/gofastr/core/render"
)

// Config configures a progress bar.
type Config struct {
	// Value is the current progress, between 0 and Max. A negative
	// Value renders an indeterminate (animated) bar.
	Value float64
	Max   float64 // defaults to 100 when zero

	// Label is the accessible name. Required for screen readers.
	Label string

	// Description optionally renders alongside the bar (e.g. "73 of 100"
	// or "Uploading file…"). Empty = no description rendered.
	Description string

	ID    string
	Class string
}

// New renders a <progress> element with the given configuration.
//
// Required: Label (becomes aria-label).
func New(cfg Config) render.HTML {
	if cfg.Label == "" {
		panic("progress: New requires Label")
	}
	max := cfg.Max
	if max == 0 {
		max = 100
	}

	cls := "progress"
	if cfg.Class != "" {
		cls = cls + " " + cfg.Class
	}

	progAttrs := map[string]string{
		"class":      "progress-bar",
		"max":        fmt.Sprintf("%v", max),
		"aria-label": cfg.Label,
	}
	if cfg.Value >= 0 {
		progAttrs["value"] = fmt.Sprintf("%v", cfg.Value)
	}

	wrapAttrs := map[string]string{"class": cls}
	if cfg.ID != "" {
		wrapAttrs["id"] = cfg.ID
	}

	children := []render.HTML{render.Tag("progress", progAttrs)}
	if cfg.Description != "" {
		children = append(children,
			render.Tag("span", map[string]string{"class": "progress-description"},
				render.Text(cfg.Description)),
		)
	}
	return render.Tag("div", wrapAttrs, children...)
}

// BaseCSS returns the stylesheet for the progress component. Tokens
// consumed (with fallbacks): --color-surface, --color-border,
// --color-primary, --radii-full, --spacing-xs.
func BaseCSS() string {
	return `
.progress {
  display: grid;
  gap: var(--spacing-xs, 2px);
}
.progress-bar {
  appearance: none;
  -webkit-appearance: none;
  inline-size: 100%;
  block-size: 0.5rem;
  border: 0;
  border-radius: var(--radii-full, 9999px);
  background: var(--color-border, #E5E7EB);
  overflow: hidden;
}
.progress-bar::-webkit-progress-bar {
  background: var(--color-border, #E5E7EB);
  border-radius: var(--radii-full, 9999px);
}
.progress-bar::-webkit-progress-value {
  background: var(--color-primary, #4F46E5);
  border-radius: var(--radii-full, 9999px);
  transition: inline-size 200ms ease;
}
.progress-bar::-moz-progress-bar {
  background: var(--color-primary, #4F46E5);
  border-radius: var(--radii-full, 9999px);
}
.progress-description {
  font-size: 0.85rem;
  color: var(--color-text-muted, #6B7280);
}

@media (prefers-reduced-motion: reduce) {
  .progress-bar::-webkit-progress-value { transition: none; }
}
`
}
