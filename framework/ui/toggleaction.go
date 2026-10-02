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
// and the retired toggleaction adapter module (its .js and its
// registration) is deleted with this move.

// ─── ToggleAction ───────────────────────────────────────────────────
//
// The three-state cousin of OptimisticAction. OptimisticAction commits
// once and stays committed; ToggleAction cycles idle → pending →
// committed and supports two additional patterns:
//
//  1. Mutex groups (Group): buttons sharing the same group key form a
//     mutex: committing one optimistically reverts any sibling that
//     was committed (no extra RPC; the server stays the source of
//     truth and a later navigation refreshes from server state).
//
//  2. Untoggle (AllowUntoggle / UntoggleEndpoint): clicking an
//     already-committed button reverts it to idle. If UntoggleEndpoint
//     is set the runtime POSTs it; otherwise the state just flips
//     locally. Without either, the button is sticky once committed,
//     matching OptimisticAction's behaviour.
//
// Use for "Follow / Following", plan pickers, watch/unwatch: binary
// server-backed state the user flips in place. For one-shot commits
// prefer OptimisticAction; for destructive actions pair with
// ConfirmAction instead.
//
// SSR shape (state ships server-rendered; the runtime only flips it):
//
//	<button data-fui-comp="ui-toggle-action"
//	        data-hui-action="" data-hui-action-endpoint="/follow"
//	        data-hui-action-untoggle="/unfollow"
//	        data-hui-action-group="follows"
//	        data-hui-action-failed="…"
//	        data-state="idle" aria-pressed="false"
//	        class="fui-button fui-button--primary fui-toggle-action">
//	    <span data-hui-action-idle>Follow</span>
//	    <span data-hui-action-done hidden>Following ✓</span>
//	</button>
//
// The headless module binds the data-hui-action* hooks through the
// kernel's action primitive (the retired toggleaction.js is deleted)
// and mirrors the committed state onto aria-pressed.

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
	// previously-committed sibling. Maps to data-hui-action-group.
	Group string

	// AllowUntoggle lets a click on a committed button revert it to
	// idle. Maps to the data-hui-action-untoggle hook (empty for a
	// local flip, the untoggle endpoint's URL when one is set).
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

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers) to the button's root <button> element. Keys
	// the component owns are dropped: class and id (use Class / ID),
	// data-fui-* (the toggle runtime wiring), type, data-state, and
	// aria-pressed (mirrored from Committed by the runtime).
	ExtraAttrs html.Attrs

	// FailedText is what the status span announces on a failure.
	// Empty takes the Strings default.
	FailedText string
	// Ctx carries the per-request context used to resolve the
	// failure sentence. When nil, English fallbacks apply.
	Ctx context.Context
	// Disabled is the state at render time.
	Disabled bool
}

// ToggleAction renders the button. The marker fetches this sheet; the
// clicks are bound through the data-hui-action* hooks the primitive
// renders.
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
	tv := cfg.Variant
	if tv == "" {
		tv = ButtonPrimary
	}
	checkButtonVariant("ToggleAction", tv)
	checkButtonSize("ToggleAction", cfg.Size)

	parts := headless.Parts{}
	if extra := strings.TrimSpace(buttonClassTokens(tv, cfg.Size) + " fui-toggle-action " + cfg.Class); extra != "" {
		parts.Attrs = headless.PartAttrs{headless.PartRoot: {"class": strings.TrimSpace(extra)}}
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return toggleActionStyle.WrapHTML(headless.ToggleAction(headless.ToggleActionProps{
		Endpoint:         cfg.Endpoint,
		Method:           cfg.Method,
		IdleLabel:        cfg.IdleLabel,
		CommittedLabel:   cfg.CommittedLabel,
		IdleIcon:         cfg.IdleIcon,
		DoneIcon:         cfg.CommittedIcon,
		Committed:        cfg.Committed,
		Group:            cfg.Group,
		AllowUntoggle:    cfg.AllowUntoggle,
		UntoggleEndpoint: cfg.UntoggleEndpoint,
		Variant:          string(cfg.Variant),
		Size:             string(cfg.Size),
		Disabled:         cfg.Disabled,
		FailedText:       cfg.FailedText,
		ID:               cfg.ID,
		ExtraAttrs:       headless.Safe(cfg.ExtraAttrs, "type", "data-state", "aria-pressed"),
		Parts:            parts,
		Strings:          StringsFor(ctx),
	}, toggleActionClasses))
}

// toggleActionClasses dresses the primitive's parts in the button
// family's vocabulary.
var toggleActionClasses = headless.Classes{
	headless.PartRoot: "fui-button",
}

var toggleActionStyle = registry.RegisterStyle("ui-toggle-action", func(_ style.Theme) string {
	return `[data-fui-comp="ui-toggle-action"] {
  /* Inherits .fui-button base; override only what the toggle flip needs. */
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
