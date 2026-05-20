package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── OptimisticAction ───────────────────────────────────────────────
//
// Wraps a trigger button with optimistic UI: the button declares both
// its idle and success labels in SSR markup. On click the runtime flips
// to the success state IMMEDIATELY, then fires the RPC. On non-2xx
// (or network error) the button rolls back to idle and surfaces a
// brief error tooltip.
//
// Use for "Follow", "Like", "Subscribe", "Add to cart" — actions where
// the user expects instant feedback and the server response is just a
// confirmation. NOT for irreversible / destructive actions (delete,
// charge, …) — pair those with ConfirmAction instead.
//
// SSR shape:
//
//	<button data-fui-comp="ui-optimistic-action"
//	        data-fui-optimistic-endpoint="/follow"
//	        data-fui-optimistic-method="POST"
//	        data-state="idle"
//	        class="ui-button ui-optimistic-action">
//	    <span data-fui-optimistic-idle>Follow</span>
//	    <span data-fui-optimistic-success hidden>Following ✓</span>
//	</button>
//
// The runtime auto-loads `/runtime/optimisticaction.js` on first
// appearance.

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

	// SuccessIcon optionally renders alongside SuccessLabel — common
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
}

// OptimisticAction renders the button. The runtime listens for clicks
// via the data-fui-comp marker.
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
	method := cfg.Method
	if method == "" {
		method = "POST"
	}

	cls := "ui-button ui-optimistic-action"
	// Always append the variant modifier when set — primary needs the
	// class too because the base .ui-button selectors include the
	// primary colors via ui-button--primary on some themes. The
	// earlier `!= ButtonPrimary` guard silently dropped the class for
	// explicit-primary callers.
	if cfg.Variant != "" {
		cls += " ui-button--" + string(cfg.Variant)
	}
	if cfg.Size != "" {
		cls += " ui-button--" + string(cfg.Size)
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	attrs := map[string]string{
		"class":                          cls,
		"type":                           "button",
		"data-fui-optimistic-endpoint":   cfg.Endpoint,
		"data-fui-optimistic-method":     method,
		"data-state":                     "idle",
	}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}

	idleChildren := []render.HTML{}
	if cfg.IdleIcon != "" {
		idleChildren = append(idleChildren, cfg.IdleIcon)
	}
	idleChildren = append(idleChildren, render.Text(cfg.IdleLabel))

	successChildren := []render.HTML{}
	if cfg.SuccessIcon != "" {
		successChildren = append(successChildren, cfg.SuccessIcon)
	}
	successChildren = append(successChildren, render.Text(cfg.SuccessLabel))

	idleSpan := render.Tag("span", map[string]string{
		"data-fui-optimistic-idle": "",
		"class":                    "ui-optimistic-action__idle",
	}, idleChildren...)

	successSpan := render.Tag("span", map[string]string{
		"data-fui-optimistic-success": "",
		"class":                       "ui-optimistic-action__success",
		"hidden":                      "",
	}, successChildren...)

	return optimisticActionStyle.WrapHTML(render.Tag("button", attrs, idleSpan, successSpan))
}

var optimisticActionStyle = registry.RegisterStyle("ui-optimistic-action", func(_ style.Theme) string {
	return `[data-fui-comp="ui-optimistic-action"] {
  /* Inherits .ui-button base; override only what the optimistic flip needs. */
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
