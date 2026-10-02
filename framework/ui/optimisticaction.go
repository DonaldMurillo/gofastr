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

// The behaviour is the headless module's: the primitive's
// data-hui-action* hooks bind through the kernel's action primitive,
// and the retired optimisticaction adapter module (its .js and its
// registration) is deleted with this move.

// ─── OptimisticAction ───────────────────────────────────────────────
//
// Wraps a trigger button with optimistic UI: the button declares both
// its idle and success labels in SSR markup. On click the runtime flips
// to the success state IMMEDIATELY, then fires the RPC. On non-2xx
// (or network error) the button rolls back to idle and surfaces a
// brief error tooltip.
//
// Use for "Follow", "Like", "Subscribe", "Add to cart": actions where
// the user expects instant feedback and the server response is just a
// confirmation. NOT for irreversible / destructive actions (delete,
// charge, …). Pair those with ConfirmAction instead.
//
// SSR shape (the headless action contract):
//
//	<button data-fui-comp="ui-optimistic-action"
//	        data-hui-action="" data-hui-action-endpoint="/follow"
//	        data-hui-action-failed="…"
//	        data-state="idle"
//	        class="fui-button fui-button--primary fui-optimistic-action">
//	    <span data-hui-action-idle>Follow</span>
//	    <span data-hui-action-done hidden>Following ✓</span>
//	    …a visually-hidden role=status span the rollback sentence is
//	    announced into…
//	</button>
//
// The headless module binds the data-hui-action* hooks through the
// kernel's action primitive — the retired optimisticaction.js is
// deleted, and a host that still hand-loads it gets a 404.

// OptimisticActionConfig configures an OptimisticAction button.
type OptimisticActionConfig struct {
	// Endpoint is the URL the click fires against. Required.
	Endpoint string

	// Method is "POST" (default), "DELETE", "PATCH", or "PUT".
	Method string

	// IdleLabel is the button text in the rest state. Required.
	IdleLabel string

	// SuccessLabel is shown immediately on click (the optimistic
	// flip). Required.
	SuccessLabel string

	// IdleIcon optionally renders alongside IdleLabel.
	IdleIcon render.HTML

	// SuccessIcon optionally renders alongside SuccessLabel. Common
	// usage: a small check or filled-heart SVG.
	SuccessIcon render.HTML

	// Variant maps to the standard Button variant ("primary"/""
	// default, "secondary", "danger", "ghost").
	Variant ButtonVariant

	// Size maps to the standard Button size ("" default, "small",
	// "large").
	Size ButtonSize

	ID    string
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the button's root
	// element. Keys the component owns are dropped: class and id
	// (use Class / ID), data-fui-* (endpoint and method wiring),
	// type, and data-state — the optimistic lifecycle contract.
	ExtraAttrs html.Attrs

	// FailedText is what the status span announces when the server
	// refuses. Empty takes the Strings default.
	FailedText string
	// Ctx carries the per-request context used to resolve the
	// failure sentence. When nil, English fallbacks apply.
	Ctx context.Context
}

// OptimisticAction renders the button. The marker fetches this sheet;
// the clicks are bound through the data-hui-action* hooks the
// primitive renders.
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
	ov := cfg.Variant
	if ov == "" {
		ov = ButtonPrimary
	}
	checkButtonVariant("OptimisticAction", ov)
	checkButtonSize("OptimisticAction", cfg.Size)

	// The optimistic classes ride beside the button family's own so
	// this sheet's flip styling reaches the same button.
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
		Variant:      string(cfg.Variant),
		Size:         string(cfg.Size),
		FailedText:   cfg.FailedText,
		ID:           cfg.ID,
		ExtraAttrs:   headless.Safe(cfg.ExtraAttrs, "type", "data-state"),
		Parts:        parts,
		Strings:      StringsFor(ctx),
	}, optimisticActionClasses))
}

// optimisticActionClasses dresses the primitive's parts in the button
// family's vocabulary.
var optimisticActionClasses = headless.Classes{
	headless.PartRoot: "fui-button",
}

var optimisticActionStyle = registry.RegisterStyle("ui-optimistic-action", func(_ style.Theme) string {
	return `[data-fui-comp="ui-optimistic-action"] {
  /* Inherits .fui-button base; override only what the optimistic flip needs. */
  position: relative;
  transition: background-color 120ms ease, color 120ms ease;
}
/* Committed state — slightly darker background to signal "done". */
[data-fui-comp="ui-optimistic-action"][data-state="committed"] {
  background: var(--color-success, #16A34A);
  color: var(--color-primary-fg, #FFFFFF);
  border-color: var(--color-success, #16A34A);
}
/* Pending state — same look as committed (optimistic) plus a subtle
   busy cursor while the RPC is in flight. */
[data-fui-comp="ui-optimistic-action"][data-state="pending"] {
  cursor: progress;
  background: var(--color-success, #16A34A);
  color: var(--color-primary-fg, #FFFFFF);
  border-color: var(--color-success, #16A34A);
}
/* Roll-back animation: a tiny shake when the server rejects the
   action. Pure CSS, respects prefers-reduced-motion. */
[data-fui-comp="ui-optimistic-action"][data-state="error"] {
  animation: ui-optimistic-action-shake 0.4s ease-in-out;
}
@keyframes ui-optimistic-action-shake {
  0%, 100% { transform: translateX(0); }
  20% { transform: translateX(-4px); }
  40% { transform: translateX(4px); }
  60% { transform: translateX(-3px); }
  80% { transform: translateX(3px); }
}
@media (prefers-reduced-motion: reduce) {
  [data-fui-comp="ui-optimistic-action"][data-state="error"] { animation: none; }
}
`
})
