package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── ToggleAction ───────────────────────────────────────────────────
//
// The three-state cousin of OptimisticAction. OptimisticAction commits
// once and stays committed; ToggleAction cycles idle → pending →
// committed and supports two additional patterns:
//
//  1. Mutex groups (Group): buttons sharing the same group key form a
//     mutex — committing one optimistically reverts any sibling that
//     was committed (no extra RPC; the server stays the source of
//     truth and a later navigation refreshes from server state).
//
//  2. Untoggle (AllowUntoggle / UntoggleEndpoint): clicking an
//     already-committed button reverts it to idle. If UntoggleEndpoint
//     is set the runtime POSTs it; otherwise the state just flips
//     locally. Without either, the button is sticky once committed —
//     matching OptimisticAction's behaviour.
//
// Use for "Follow / Following", plan pickers, watch/unwatch — binary
// server-backed state the user flips in place. For one-shot commits
// prefer OptimisticAction; for destructive actions pair with
// ConfirmAction instead.
//
// SSR shape (state ships server-rendered; the runtime only flips it):
//
//	<button data-fui-comp="ui-toggle-action"
//	        data-fui-toggle-endpoint="/follow"
//	        data-fui-toggle-method="POST"
//	        data-state="idle" aria-pressed="false"
//	        class="ui-button ui-toggle-action">
//	    <span data-fui-toggle-idle>Follow</span>
//	    <span data-fui-toggle-committed hidden>Following ✓</span>
//	</button>
//
// The runtime auto-loads `/runtime/toggleaction.js` on first
// appearance and mirrors the committed state onto aria-pressed.

// ToggleActionConfig configures a ToggleAction button.
type ToggleActionConfig struct {
	// Endpoint is the URL hit when toggling idle → committed. Required.
	Endpoint string

	// Method is "POST" (default), "DELETE", "PATCH", or "PUT". Applies
	// to both the commit and the untoggle request.
	Method string

	// IdleLabel is the button text in the un-committed state. Required.
	IdleLabel string

	// CommittedLabel is shown while committed (the runtime flips to it
	// optimistically on click). Required.
	CommittedLabel string

	// IdleIcon optionally renders alongside IdleLabel.
	IdleIcon render.HTML

	// CommittedIcon optionally renders alongside CommittedLabel.
	CommittedIcon render.HTML

	// Committed sets the SSR initial state. Render true when the
	// server already knows the action is active (user follows, plan
	// selected) so first paint matches server state.
	Committed bool

	// Group, when set, joins this button to a client-side mutex:
	// committing any button with the same Group key reverts the
	// previously-committed sibling. Maps to data-fui-toggle-group.
	Group string

	// AllowUntoggle lets a click on a committed button revert it to
	// idle. Maps to data-fui-toggle-allow-untoggle="true".
	AllowUntoggle bool

	// UntoggleEndpoint is the URL hit when reverting committed → idle.
	// Setting it implies AllowUntoggle. When empty (with AllowUntoggle
	// true) the revert flips locally with no request.
	UntoggleEndpoint string

	// Variant maps to the standard Button variant ("primary"/""
	// default, "secondary", "danger", "ghost").
	Variant ButtonVariant

	// Size maps to the standard Button size ("" default, "small",
	// "large").
	Size ButtonSize

	ID    string
	Class string
}

// ToggleAction renders the button. The runtime listens for clicks via
// the data-fui-comp marker.
func ToggleAction(cfg ToggleActionConfig) render.HTML {
	if cfg.Endpoint == "" {
		panic("ui: ToggleAction requires Endpoint")
	}
	if cfg.IdleLabel == "" {
		panic("ui: ToggleAction requires IdleLabel")
	}
	if cfg.CommittedLabel == "" {
		panic("ui: ToggleAction requires CommittedLabel")
	}
	method := cfg.Method
	if method == "" {
		method = "POST"
	}
	state, pressed := "idle", "false"
	if cfg.Committed {
		state, pressed = "committed", "true"
	}

	cls := "ui-button ui-toggle-action"
	if cfg.Variant != "" {
		checkButtonVariant("ToggleAction", cfg.Variant)
		cls += " ui-button--" + string(cfg.Variant)
	}
	if cfg.Size != "" {
		checkButtonSize("ToggleAction", cfg.Size)
		cls += " ui-button--" + string(cfg.Size)
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	attrs := map[string]string{
		"class":                    cls,
		"type":                     "button",
		"data-fui-toggle-endpoint": cfg.Endpoint,
		"data-fui-toggle-method":   method,
		"data-state":               state,
		"aria-pressed":             pressed,
	}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	if cfg.Group != "" {
		attrs["data-fui-toggle-group"] = cfg.Group
	}
	if cfg.AllowUntoggle || cfg.UntoggleEndpoint != "" {
		attrs["data-fui-toggle-allow-untoggle"] = "true"
	}
	if cfg.UntoggleEndpoint != "" {
		attrs["data-fui-toggle-untoggle-endpoint"] = cfg.UntoggleEndpoint
	}

	idleChildren := []render.HTML{}
	if cfg.IdleIcon != "" {
		idleChildren = append(idleChildren, cfg.IdleIcon)
	}
	idleChildren = append(idleChildren, render.Text(cfg.IdleLabel))

	committedChildren := []render.HTML{}
	if cfg.CommittedIcon != "" {
		committedChildren = append(committedChildren, cfg.CommittedIcon)
	}
	committedChildren = append(committedChildren, render.Text(cfg.CommittedLabel))

	idleAttrs := map[string]string{
		"data-fui-toggle-idle": "",
		"class":                "ui-toggle-action__idle",
	}
	committedAttrs := map[string]string{
		"data-fui-toggle-committed": "",
		"class":                     "ui-toggle-action__committed",
	}
	if cfg.Committed {
		idleAttrs["hidden"] = ""
	} else {
		committedAttrs["hidden"] = ""
	}

	idleSpan := render.Tag("span", idleAttrs, idleChildren...)
	committedSpan := render.Tag("span", committedAttrs, committedChildren...)

	return toggleActionStyle.WrapHTML(render.Tag("button", attrs, idleSpan, committedSpan))
}

var toggleActionStyle = registry.RegisterStyle("ui-toggle-action", func(_ style.Theme) string {
	return `[data-fui-comp="ui-toggle-action"] {
  /* Inherits .ui-button base; override only what the toggle flip needs. */
  position: relative;
  transition: background-color 120ms ease, color 120ms ease;
}
/* Committed state — success tone signals "active". */
[data-fui-comp="ui-toggle-action"][data-state="committed"] {
  background: var(--color-success, #16A34A);
  color: var(--color-primary-fg, #FFFFFF);
  border-color: var(--color-success, #16A34A);
}
/* Pending — same look as committed (optimistic) plus a busy cursor
   while the RPC is in flight. The runtime also sets aria-busy +
   disabled during this window. */
[data-fui-comp="ui-toggle-action"][data-state="pending"] {
  cursor: progress;
  background: var(--color-success, #16A34A);
  color: var(--color-primary-fg, #FFFFFF);
  border-color: var(--color-success, #16A34A);
}
`
})
