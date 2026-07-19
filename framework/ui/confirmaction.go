package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/render"

	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── ConfirmAction ──────────────────────────────────────────────────
//
// A declarative pair: a destructive-action trigger button + a modal
// alertdialog wired to confirm/cancel. Eliminates the per-button
// boilerplate of building a hidden Modal preset by hand.
//
// Usage:
//
//	trigger, modal := ui.ConfirmAction(ui.ConfirmActionConfig{
//	    Name:         "delete-user-42",
//	    TriggerLabel: "Delete",
//	    Title:        "Delete user?",
//	    Body:         "This permanently removes the user and their data.",
//	    RPCPath:      "/users/42/delete",
//	})
//	def := modal.Build()
//	widget.Mount(r, &def) // once at app startup
//	// then render `trigger` wherever the destructive button belongs

// ConfirmActionConfig configures the confirmation flow.
type ConfirmActionConfig struct {
	// Name uniquely identifies the modal widget. Required.
	// Usually qualified per row, e.g. "delete-user-42".
	Name string

	// TriggerLabel is the visible text on the destructive button.
	// Required.
	TriggerLabel string

	// TriggerVariant maps to one of the framework button variants.
	// Defaults to "danger". The trigger always renders as
	// .ui-btn--<TriggerVariant>.
	TriggerVariant string

	// Title is the alertdialog title (h2). Required.
	Title string

	// Body is the alertdialog body paragraph. Required — the body
	// gives the user the information they need to confirm safely.
	Body string

	// ConfirmLabel defaults to "Confirm".
	ConfirmLabel string

	// CancelLabel defaults to "Cancel".
	CancelLabel string

	// RPCPath is the endpoint the Confirm button posts to. Required.
	RPCPath string

	// RPCMethod defaults to "POST".
	RPCMethod string

	// SuccessSignal, when set, emits data-fui-rpc-signal="<name>" on
	// the Confirm button. On a 2xx response the runtime broadcasts
	// the response body (typically the fresh authoritative list HTML)
	// into the named signal — pair it with a
	// data-fui-signal="<name>" data-fui-signal-mode="html" region to
	// swap in that HTML (e.g. the shorter list after a delete). On a
	// non-2xx response html-mode regions are left unchanged (the
	// optimistic-UI invariant: a failed delete leaves the row/list
	// intact), while text-mode regions render a human-readable
	// "Error: …" string. Empty (the default) leaves the response
	// unused, which is correct for fire-and-forget confirms.
	//
	// The name MUST match ^[A-Za-z0-9_-]+$ — ConfirmAction panics
	// otherwise. The runtime interpolates the value into a CSS
	// attribute selector (querySelectorAll '[data-fui-signal="…"]'),
	// so any other shape is either an invalid selector (silently
	// drops the broadcast) or a selector-injection footgun.
	SuccessSignal string

	// AutofocusConfirm flips the initial focus from Cancel (the
	// default, safer choice for destructive flows where accidental
	// Enter must not fire the action) to Confirm. Set to true for
	// non-destructive confirmations ("Apply changes?", "Continue?").
	AutofocusConfirm bool
	// Ctx carries the per-request context used to resolve the
	// Confirm/Cancel button labels. When nil, English fallbacks apply.
	Ctx context.Context
}

// ConfirmAction returns the trigger button and a *widget.Builder for
// the alertdialog. The caller mounts the preset once at startup; the
// trigger renders inline anywhere on the page.
func ConfirmAction(cfg ConfirmActionConfig) (render.HTML, *widget.Builder) {
	if cfg.Name == "" {
		panic("ui: ConfirmAction requires Name")
	}
	if cfg.TriggerLabel == "" {
		panic("ui: ConfirmAction requires TriggerLabel")
	}
	if cfg.Title == "" {
		panic("ui: ConfirmAction requires Title")
	}
	if cfg.Body == "" {
		panic("ui: ConfirmAction requires Body")
	}
	if cfg.RPCPath == "" {
		panic("ui: ConfirmAction requires RPCPath")
	}
	if cfg.SuccessSignal != "" && !validSuccessSignalName(cfg.SuccessSignal) {
		panic("ui: ConfirmAction SuccessSignal must match ^[A-Za-z0-9_-]+$, got " + cfg.SuccessSignal)
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	variant := cfg.TriggerVariant
	if variant == "" {
		variant = "danger"
	}
	confirm := cfg.ConfirmLabel
	if confirm == "" {
		confirm = i18nui.T(ctx, i18nui.KeyDialogConfirm)
	}
	cancel := cfg.CancelLabel
	if cancel == "" {
		cancel = i18nui.T(ctx, i18nui.KeyDialogCancel)
	}
	method := cfg.RPCMethod
	if method == "" {
		method = "POST"
	}
	trigger := buildConfirmTrigger(cfg.Name, cfg.TriggerLabel, variant)
	modal := buildConfirmModal(cfg.Name, cfg.Title, cfg.Body, confirm, cancel, method, cfg.RPCPath, cfg.SuccessSignal, cfg.AutofocusConfirm)
	return trigger, modal
}

func buildConfirmTrigger(name, label, variant string) render.HTML {
	return Button(ButtonConfig{
		Label:   label,
		Variant: parseButtonVariant(variant),
		ExtraAttrs: html.Attrs{
			"data-fui-open": name,
		},
	})
}

// parseButtonVariant maps a free-form variant string to a typed
// ButtonVariant. Unknown values fall back to ButtonDanger because
// ConfirmAction's primary use case is destructive flows.
func parseButtonVariant(v string) ButtonVariant {
	switch ButtonVariant(v) {
	case ButtonPrimary, ButtonSecondary, ButtonDanger, ButtonGhost:
		return ButtonVariant(v)
	}
	return ButtonDanger
}

// validSuccessSignalName returns true iff name is a safe identifier
// for use as a data-fui-rpc-signal / data-fui-signal value. The runtime
// interpolates the name verbatim into the CSS attribute selector
// `[data-fui-signal="<name>"]`; values outside [A-Za-z0-9_-] are either
// invalid selectors (the broadcast silently no-ops) or selector-injection
// (a `"` or `]` breaks out of the attribute matcher). Keep this in sync
// with the same shape used by interactive.SetSignal and the CSS.escape
// contract documented in core-ui/ARCHITECTURE.md.
func validSuccessSignalName(name string) bool {
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '_' || c == '-':
		default:
			return false
		}
	}
	return true
}

func buildConfirmModal(name, title, body, confirmLabel, cancelLabel, method, rpcPath, successSignal string, autofocusConfirm bool) *widget.Builder {
	titleID := name + "-title"
	bodyID := name + "-body"
	slot := &confirmDialogSlot{
		titleID:          titleID,
		bodyID:           bodyID,
		title:            title,
		body:             body,
		confirmLabel:     confirmLabel,
		cancelLabel:      cancelLabel,
		rpcMethod:        method,
		rpcPath:          rpcPath,
		successSignal:    successSignal,
		autofocusConfirm: autofocusConfirm,
	}
	return preset.Modal(name).
		Hidden().
		Role("alertdialog").
		LabelledBy(titleID).
		DescribedBy(bodyID).
		Slot("body", slot)
}

type confirmDialogSlot struct {
	titleID, bodyID                   string
	title, body                       string
	confirmLabel, cancelLabel         string
	rpcMethod, rpcPath, successSignal string
	autofocusConfirm                  bool
}

func (s *confirmDialogSlot) Render() render.HTML {
	cancelAttrs := html.Attrs{
		"data-fui-rpc-close": "",
	}
	confirmAttrs := html.Attrs{
		"data-fui-rpc":        s.rpcPath,
		"data-fui-rpc-method": s.rpcMethod,
		"data-fui-rpc-close":  "",
	}
	if s.successSignal != "" {
		confirmAttrs["data-fui-rpc-signal"] = s.successSignal
	}
	// Only the OPT-IN case carries an autofocus attribute. Cancel is
	// rendered first in DOM order so the Modal preset's "focus the
	// first focusable" pass already lands on it — adding autofocus
	// there would race with the preset's focus() call and emit
	// Chrome's "Autofocus processing was blocked" info message. The
	// preset's focus pass also explicitly prefers any [autofocus]
	// element when one is present, so AutofocusConfirm still wins.
	if s.autofocusConfirm {
		confirmAttrs["autofocus"] = ""
	}

	return confirmActionStyle.WrapHTML(html.Div(html.DivConfig{Class: "ui-confirm-action"},
		html.Heading(html.HeadingConfig{Level: 2, Class: "ui-confirm-action__title", ID: s.titleID},
			render.Text(s.title)),
		html.Paragraph(html.TextConfig{Class: "ui-confirm-action__body", ID: s.bodyID},
			render.Text(s.body)),
		html.Div(html.DivConfig{Class: "ui-confirm-action__actions"},
			Button(ButtonConfig{
				Label:      s.cancelLabel,
				Variant:    ButtonGhost,
				ExtraAttrs: cancelAttrs,
			}),
			Button(ButtonConfig{
				Label:      s.confirmLabel,
				Variant:    ButtonDanger,
				ExtraAttrs: confirmAttrs,
			}),
		),
	))
}

var _ component.Component = (*confirmDialogSlot)(nil)

var confirmActionStyle = registry.RegisterStyle("ui-confirm-action", confirmActionCSS)

func confirmActionCSS(_ style.Theme) string {
	// No background / padding / border-radius here: the widget chrome's
	// centered panel (`.fui-pos-center > .fui-panel`, core-ui/widget)
	// paints the panel surface for every modal body. This component
	// only constrains its own width and lays out its internals —
	// duplicating the panel props would double-pad the dialog.
	return `[data-fui-comp="ui-confirm-action"] {
  display: block;
  max-inline-size: 28rem;
}
[data-fui-comp="ui-confirm-action"] .ui-confirm-action__title {
  margin: 0 0 var(--spacing-sm, 8px) 0;
  font-size: var(--text-lg, 1.1rem);
  font-weight: 600;
  color: var(--color-text, #111);
}
[data-fui-comp="ui-confirm-action"] .ui-confirm-action__body {
  margin: 0 0 var(--spacing-lg, 16px) 0;
  color: var(--color-text-muted, #4b5563);
  line-height: 1.45;
}
[data-fui-comp="ui-confirm-action"] .ui-confirm-action__actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--spacing-sm, 8px);
}
`
}
