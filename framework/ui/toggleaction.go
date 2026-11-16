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

// ToggleActionConfig configures a ToggleAction button.
type ToggleActionConfig struct {
	// Endpoint is the URL hit on commit. Required.
	Endpoint string
	// Method is "POST" (default), "DELETE", "PATCH" or "PUT", for commit and untoggle.
	Method string
	// IdleLabel is the text and the accessible name in both states. Required.
	IdleLabel string
	// CommittedLabel is the visible text while committed; keep IdleLabel's words in it. Required.
	CommittedLabel string
	// IdleIcon renders beside IdleLabel.
	IdleIcon render.HTML
	// CommittedIcon renders beside CommittedLabel.
	CommittedIcon render.HTML
	// Committed is the server-rendered state.
	Committed bool
	// Group joins a client-side mutex: committing one reverts its committed sibling.
	Group string
	// AllowUntoggle lets a click on a committed button revert it.
	AllowUntoggle bool
	// UntoggleEndpoint is the URL hit on revert; it implies AllowUntoggle.
	UntoggleEndpoint string
	// Variant is the Button variant. Default primary.
	Variant ButtonVariant
	// Size is the Button size.
	Size  ButtonSize
	ID    string
	Class string
	// ExtraAttrs land on the button; class, id, data-cui-*, type, data-state and aria-pressed are dropped.
	ExtraAttrs html.Attrs
	// FailedText is the sentence announced on a failure. Empty takes the Strings default.
	FailedText string
	// Ctx resolves the default words. Nil uses English.
	Ctx context.Context
	// Disabled is the state at render time.
	Disabled bool
}

// ToggleAction renders a toggle button that flips between IdleLabel
// and CommittedLabel and mirrors the state onto aria-pressed.
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

var toggleActionClasses = headless.Classes{
	headless.PartRoot: "fui-button",
}

var toggleActionStyle = registry.RegisterStyle("ui-toggle-action", func(_ style.Theme) string {
	return `[data-cui-comp="ui-toggle-action"] {
  /* Inherits .fui-button base; override only what the toggle flip needs. */
  position: relative;
  transition: background-color var(--duration-fast, 150ms) var(--easing-ease-in-out, ease), color var(--duration-fast, 150ms) var(--easing-ease-in-out, ease);
}
/* Committed state — the soft secondary surface marks "on" without a
   status color: success is for outcomes, not for a pressed toggle. */
[data-cui-comp="ui-toggle-action"][data-state="committed"] {
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text, #09090B);
  border-color: var(--color-border, #E4E4E7);
}
/* Pending — same look as committed (optimistic) plus a busy cursor
   while the RPC is in flight. The runtime also sets aria-busy +
   disabled during this window. */
[data-cui-comp="ui-toggle-action"][data-state="pending"] {
  cursor: progress;
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text, #09090B);
  border-color: var(--color-border, #E4E4E7);
}
`
})
