package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── PollingIndicator ───────────────────────────────────────────────
//
// Tiny pulsing dot + label that confirms a polling RPC or live-update
// pipeline is firing. Pairs with `data-fui-rpc-trigger="input"` to give
// users feedback that the live-search / live-validate is actually
// searching. Pure CSS — no runtime module needed.

// PollingIndicatorConfig configures a PollingIndicator.
type PollingIndicatorConfig struct {
	// Label is the text rendered next to the pulsing dot.
	// Defaults to "Live".
	Label string
	// Paused freezes the pulse animation and dims the dot — use when
	// the upstream polling has been paused or completed.
	Paused bool
	ID     string
	Class  string
}

// PollingIndicator renders the small pulsing-dot + label combination.
// Uses role="status" + aria-live="polite" so the label text is
// announced when it changes (e.g. swapping "Live" for "Paused").
func PollingIndicator(cfg PollingIndicatorConfig) render.HTML {
	label := cfg.Label
	if label == "" {
		label = "Live"
	}
	cls := "ui-polling-indicator"
	if cfg.Paused {
		cls += " ui-polling-indicator--paused"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := map[string]string{
		"class":     cls,
		"role":      "status",
		"aria-live": "polite",
	}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	return pollingIndicatorStyle.WrapHTML(render.Tag("span", attrs,
		render.Tag("span", map[string]string{
			"class":       "ui-polling-indicator__dot",
			"aria-hidden": "true",
		}),
		html.Span(html.TextConfig{Class: "ui-polling-indicator__label"}, render.Text(label)),
	))
}

var pollingIndicatorStyle = registry.RegisterStyle("ui-polling-indicator", func(_ style.Theme) string {
	return pollingIndicatorCSSText
})

const pollingIndicatorCSSText = `
.ui-polling-indicator {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-xs, 6px);
  font-size: var(--text-xs, 0.75rem);
  color: var(--color-text-muted, #6B7280);
  line-height: 1;
}
.ui-polling-indicator__dot {
  inline-size: 0.5rem;
  block-size: 0.5rem;
  border-radius: 9999px;
  background: var(--color-success, #16A34A);
  animation: ui-polling-pulse 1.6s ease-in-out infinite;
}
.ui-polling-indicator--paused .ui-polling-indicator__dot {
  background: var(--color-text-muted, #6B7280);
  animation: none;
  opacity: 0.6;
}
@keyframes ui-polling-pulse {
  0%   { transform: scale(1);   opacity: 1; }
  50%  { transform: scale(1.4); opacity: 0.5; }
  100% { transform: scale(1);   opacity: 1; }
}
@media (prefers-reduced-motion: reduce) {
  .ui-polling-indicator__dot { animation: none; }
}
`
