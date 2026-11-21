package ui

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// OptimisticActionConfig configures an OptimisticAction button.
type OptimisticActionConfig struct {
	// Endpoint is the URL the click fires against. Required.
	Endpoint string
	// Method is "POST" (default), "DELETE", "PATCH" or "PUT".
	Method string
	// IdleLabel is the rest-state text. Required.
	IdleLabel string
	// SuccessLabel is shown at once on click. Required.
	SuccessLabel string
	// IdleIcon renders beside IdleLabel.
	IdleIcon render.HTML
	// SuccessIcon renders beside SuccessLabel.
	SuccessIcon render.HTML
	// Variant is the Button variant. Default primary.
	Variant ButtonVariant
	// Size is the Button size.
	Size  ButtonSize
	ID    string
	Class string
	// ExtraAttrs land on the button; class, id, data-cui-*, type and data-state are dropped.
	ExtraAttrs html.Attrs
	// FailedText is the sentence announced on a refusal. Empty takes the Strings default.
	FailedText string
	// Ctx resolves the default words. Nil uses English.
	Ctx context.Context
}

// OptimisticAction renders a button that flips to SuccessLabel on
// click and rolls back when the request fails.
func OptimisticAction(cfg OptimisticActionConfig) render.HTML {
	if cfg.Endpoint == "" {
		panic("ui: OptimisticAction requires Endpoint")
	}
	if cfg.IdleLabel == "" {
		panic("ui: OptimisticAction requires IdleLabel")
	}
	if cfg.SuccessLabel == "" {
		panic("ui: OptimisticAction requires SuccessLabel")
	}
	ov := buttonVariant("OptimisticAction", cfg.Variant)
	checkButtonSize("OptimisticAction", cfg.Size)

	parts := headless.Parts{}
	if extra := strings.TrimSpace(buttonClassTokens(ov, cfg.Size) + " fui-optimistic-action " + cfg.Class); extra != "" {
		parts.Attrs = headless.PartAttrs{headless.PartRoot: {"class": strings.TrimSpace(extra)}}
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return optimisticActionStyle.WrapHTML(headless.OptimisticAction(headless.OptimisticActionProps{
		Endpoint:     cfg.Endpoint,
		Method:       cfg.Method,
		IdleLabel:    cfg.IdleLabel,
		SuccessLabel: cfg.SuccessLabel,
		IdleIcon:     cfg.IdleIcon,
		DoneIcon:     cfg.SuccessIcon,
		Variant:      string(ov),
		Size:         string(cfg.Size),
		FailedText:   cfg.FailedText,
		ID:           cfg.ID,
		ExtraAttrs:   headless.Safe(cfg.ExtraAttrs, "type", "data-state"),
		Parts:        parts,
		Strings:      StringsFor(ctx),
	}, optimisticActionClasses))
}

var optimisticActionClasses = headless.Classes{
	headless.PartRoot: "fui-button",
}

var optimisticActionStyle = registry.RegisterStyle("ui-optimistic-action", func(_ style.Theme) string {
	return `[data-cui-comp="ui-optimistic-action"] {
  /* Inherits .fui-button base; override only what the optimistic flip needs. */
  position: relative;
  transition: background-color var(--duration-fast, 150ms) var(--easing-ease-in-out, ease), color var(--duration-fast, 150ms) var(--easing-ease-in-out, ease);
}
/* Committed state — the soft secondary surface marks "done" without a
   status color: success is for outcomes, not for a pressed control. */
[data-cui-comp="ui-optimistic-action"][data-state="committed"] {
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text, #09090B);
  border-color: var(--color-border, #E4E4E7);
}
/* Pending state — same look as committed (optimistic) plus a subtle
   busy cursor while the RPC is in flight. */
[data-cui-comp="ui-optimistic-action"][data-state="pending"] {
  cursor: progress;
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text, #09090B);
  border-color: var(--color-border, #E4E4E7);
}
/* Roll-back animation: a tiny shake when the server rejects the
   action. Pure CSS, respects prefers-reduced-motion. */
[data-cui-comp="ui-optimistic-action"][data-state="error"] {
  animation: ui-optimistic-action-shake var(--duration-slow, 400ms) var(--easing-ease-in-out, ease-in-out);
}
@keyframes ui-optimistic-action-shake {
  0%, 100% { transform: translateX(0); }
  20% { transform: translateX(-4px); }
  40% { transform: translateX(4px); }
  60% { transform: translateX(-3px); }
  80% { transform: translateX(3px); }
}
@media (prefers-reduced-motion: reduce) {
  [data-cui-comp="ui-optimistic-action"][data-state="error"] { animation: none; }
}
`
})
