package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Spinner ────────────────────────────────────────────────────────
//
// A pure-CSS inline spinner. Two visual variants (ring + dots) and
// three sizes. role="status" + aria-busy="true" so assistive tech
// announces "loading" once. The visible spin animation respects
// prefers-reduced-motion (falls back to a low-frequency pulse).

// SpinnerSize selects a named size.
type SpinnerSize string

const (
	SpinnerSm SpinnerSize = "sm"
	SpinnerMd SpinnerSize = "" // default
	SpinnerLg SpinnerSize = "lg"
)

// SpinnerVariant selects the visual style.
type SpinnerVariant string

const (
	SpinnerRing SpinnerVariant = "" // default — bordered ring
	SpinnerDots SpinnerVariant = "dots"
	// SpinnerGrid renders a 3×3 grid of small squares animated in a
	// staggered ripple. Distinct enough from ring/dots to be the
	// "loading…heavy" indicator on long-running operations.
	SpinnerGrid SpinnerVariant = "grid"
)

// SpinnerConfig configures a spinner.
type SpinnerConfig struct {
	// Label is the assistive-text announced by screen readers.
	// Defaults to "Loading…" when empty.
	Label string

	// Size selects a named size (sm | md (default) | lg).
	Size SpinnerSize

	// Variant selects the visual treatment.
	Variant SpinnerVariant

	// Inline true renders inline-flex (sits next to text); false
	// renders block (centered in its own row).
	Inline bool

	ID    string
	Class string
}

// Spinner renders a loading indicator.
//
// Pair with data-fui-rpc lifecycle to surface pending state on
// island-side updates: the runtime adds `aria-busy="true"` to the
// containing form / button while the RPC is in flight, so a CSS
// rule can switch a sibling Spinner from `visibility:hidden` to
// visible without any per-component wiring.
func Spinner(cfg SpinnerConfig) render.HTML {
	cls := "ui-spinner"
	if cfg.Variant != SpinnerRing {
		cls += " ui-spinner--" + string(cfg.Variant)
	}
	if cfg.Size != SpinnerMd {
		cls += " ui-spinner--" + string(cfg.Size)
	}
	if cfg.Inline {
		cls += " ui-spinner--inline"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	label := cfg.Label
	if label == "" {
		label = "Loading…"
	}

	// Variant visuals: SpinnerDots → three dots; SpinnerGrid → 3×3
	// squares; otherwise the bordered ring.
	var visual render.HTML
	if cfg.Variant == SpinnerDots {
		visual = html.Span(html.TextConfig{
			Class:      "ui-spinner__dots",
			ExtraAttrs: html.Attrs{"aria-hidden": "true"},
		},
			html.Span(html.TextConfig{Class: "ui-spinner__dot"}),
			html.Span(html.TextConfig{Class: "ui-spinner__dot"}),
			html.Span(html.TextConfig{Class: "ui-spinner__dot"}),
		)
	} else if cfg.Variant == SpinnerGrid {
		cells := make([]render.HTML, 9)
		for i := range cells {
			cells[i] = html.Span(html.TextConfig{Class: "ui-spinner__cell"})
		}
		visual = html.Span(html.TextConfig{
			Class:      "ui-spinner__grid",
			ExtraAttrs: html.Attrs{"aria-hidden": "true"},
		}, cells...)
	} else {
		visual = html.Span(html.TextConfig{
			Class:      "ui-spinner__ring",
			ExtraAttrs: html.Attrs{"aria-hidden": "true"},
		})
	}

	return spinnerStyle.WrapHTML(html.Span(html.TextConfig{
		Class: cls, ID: cfg.ID,
		ExtraAttrs: html.Attrs{
			"role":      "status",
			"aria-live": "polite",
			"aria-busy": "true",
		},
	},
		visual,
		html.Span(html.TextConfig{Class: "ui-visually-hidden"}, render.Text(label)),
	))
}
