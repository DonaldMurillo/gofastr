package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/render"
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

	// AutofocusConfirm flips the initial focus from Cancel (the
	// default, safer choice for destructive flows where accidental
	// Enter must not fire the action) to Confirm. Set to true for
	// non-destructive confirmations ("Apply changes?", "Continue?").
	AutofocusConfirm bool
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
	variant := cfg.TriggerVariant
	if variant == "" {
		variant = "danger"
	}
	confirm := cfg.ConfirmLabel
	if confirm == "" {
		confirm = "Confirm"
	}
	cancel := cfg.CancelLabel
	if cancel == "" {
		cancel = "Cancel"
	}
	method := cfg.RPCMethod
	if method == "" {
		method = "POST"
	}
	trigger := buildConfirmTrigger(cfg.Name, cfg.TriggerLabel, variant)
	modal := buildConfirmModal(cfg.Name, cfg.Title, cfg.Body, confirm, cancel, method, cfg.RPCPath, cfg.AutofocusConfirm)
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

func buildConfirmModal(name, title, body, confirmLabel, cancelLabel, method, rpcPath string, autofocusConfirm bool) *widget.Builder {
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
	titleID, bodyID           string
	title, body               string
	confirmLabel, cancelLabel string
	rpcMethod, rpcPath        string
	autofocusConfirm          bool
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
	return `[data-fui-comp="ui-confirm-action"] {
  display: block;
  padding: var(--spacing-lg, 16px);
  max-inline-size: 28rem;
  background: var(--color-surface, #fff);
  border-radius: var(--radii-md, 6px);
}
[data-fui-comp="ui-confirm-action"] .ui-confirm-action__title {
  margin: 0 0 var(--spacing-sm, 8px) 0;
  font-size: 1.1rem;
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
