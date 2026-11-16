package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// PollingIndicatorConfig configures a PollingIndicator.
type PollingIndicatorConfig struct {
	// Label is the text beside the dot. Defaults to "Live".
	Label string
	// Paused stops the pulse and dims the dot.
	Paused bool
	ID     string
	Class  string
	// ExtraAttrs forwards attributes to the root; class, id, data-cui-*, role and aria-live are dropped.
	ExtraAttrs html.Attrs
	// Ctx resolves the default label; nil uses English.
	Ctx context.Context
}

// PollingIndicator renders a pulsing dot and a label, in a polite
// status region, that confirm a poll or live update is running.
func PollingIndicator(cfg PollingIndicatorConfig) render.HTML {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	label := cfg.Label
	if label == "" {
		label = i18nui.PollingLive.Text(ctx)
	}
	cls := "fui-polling-indicator"
	if cfg.Paused {
		cls += " fui-polling-indicator--paused"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := html.SafeExtraAttrs(cfg.ExtraAttrs, "role", "aria-live")
	if attrs == nil {
		attrs = html.Attrs{}
	}
	attrs["class"] = cls
	attrs["role"] = "status"
	attrs["aria-live"] = "polite"
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	return pollingIndicatorStyle.WrapHTML(render.Tag("span", attrs,
		render.Tag("span", map[string]string{
			"class":             "fui-polling-indicator__dot",
			"aria-hidden":       "true",
			"data-cui-internal": "",
		}),
		html.Span(html.TextConfig{Class: "fui-polling-indicator__label", ExtraAttrs: html.Attrs{"data-cui-internal": ""}}, render.Text(label)),
	))
}

var pollingIndicatorStyle = registry.RegisterStyle("ui-polling-indicator", func(_ style.Theme) string {
	return pollingIndicatorCSSText
})

const pollingIndicatorCSSText = `
.fui-polling-indicator {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
  font-size: var(--text-xs, 0.75rem);
  color: var(--color-text-muted, #52525B);
  line-height: 1;
}
.fui-polling-indicator__dot {
  inline-size: var(--ui-polling-indicator-dot-size, 0.5rem);
  block-size: var(--ui-polling-indicator-dot-size, 0.5rem);
  border-radius: var(--radii-full, 9999px);
  background: var(--color-success, #166534);
  animation: ui-polling-pulse 1.6s var(--easing-ease-in-out, ease-in-out) infinite;
}
.fui-polling-indicator--paused .fui-polling-indicator__dot {
  background: var(--color-text-muted, #52525B);
  animation: none;
  opacity: var(--opacity-muted, 0.6);
}
@keyframes ui-polling-pulse {
  0%   { transform: scale(1);   opacity: 1; }
  50%  { transform: scale(1.4); opacity: var(--opacity-disabled, 0.5); }
  100% { transform: scale(1);   opacity: 1; }
}
@media (prefers-reduced-motion: reduce) {
  .fui-polling-indicator__dot { animation: none; }
}
`
