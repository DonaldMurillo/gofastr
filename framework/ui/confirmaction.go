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

// ConfirmActionConfig configures the confirmation flow.
type ConfirmActionConfig struct {
	// Name identifies the modal widget, usually per row. Required.
	Name string
	// TriggerLabel is the destructive button's text. Required.
	TriggerLabel string
	// TriggerVariant is the trigger's button variant. Default "danger".
	TriggerVariant string
	// Title is the alertdialog heading. Required.
	Title string
	// Body is the alertdialog paragraph. Required.
	Body string
	// ConfirmLabel defaults to "Confirm" in the reader's locale.
	ConfirmLabel string
	// CancelLabel defaults to "Cancel" in the reader's locale.
	CancelLabel string
	// RPCPath is the endpoint Confirm posts to. Required.
	RPCPath string
	// RPCMethod defaults to "POST".
	RPCMethod string
	// SuccessSignal names the signal a 2xx response body is written to; it must match ^[A-Za-z0-9_-]+$.
	SuccessSignal string
	// AutofocusConfirm moves initial focus from Cancel to Confirm.
	AutofocusConfirm bool
	// ExtraAttrs land on the dialog panel; class, id and data-cui-* are dropped.
	ExtraAttrs html.Attrs
}

// ConfirmAction returns the trigger button and the alertdialog preset
// to mount once. It panics on a missing required field.
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
		panic("ui: ConfirmAction SuccessSignal must match ^[A-Za-z0-9_-]+$ and not be __proto__, constructor or prototype, got " + cfg.SuccessSignal)
	}
	variant := cfg.TriggerVariant
	if variant == "" {
		variant = "danger"
	}
	method := cfg.RPCMethod
	if method == "" {
		method = "POST"
	}
	trigger := buildConfirmTrigger(cfg.Name, cfg.TriggerLabel, variant)
	modal := buildConfirmModal(cfg.Name, cfg.Title, cfg.Body, cfg.ConfirmLabel, cfg.CancelLabel, method, cfg.RPCPath, cfg.SuccessSignal, cfg.AutofocusConfirm, html.SafeExtraAttrs(cfg.ExtraAttrs))
	return trigger, modal
}

func buildConfirmTrigger(name, label, variant string) render.HTML {
	return Button(ButtonConfig{
		Label:   label,
		Variant: parseButtonVariant(variant),
		ExtraAttrs: html.Attrs{
			"data-cui-open": name,
		},
	})
}

func parseButtonVariant(v string) ButtonVariant {
	switch ButtonVariant(v) {
	case ButtonPrimary, ButtonSecondary, ButtonDanger, ButtonGhost:
		return ButtonVariant(v)
	}
	return ButtonDanger
}

func validSuccessSignalName(name string) bool {
	if name == "__proto__" || name == "constructor" || name == "prototype" {
		return false
	}
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

func buildConfirmModal(name, title, body, confirmLabel, cancelLabel, method, rpcPath, successSignal string, autofocusConfirm bool, extraAttrs html.Attrs) *widget.Builder {
	titleID := name + "-title"
	bodyID := name + "-body"
	slot := requestSlot{build: func(ctx context.Context) component.Component {
		confirm, cancel := confirmLabel, cancelLabel
		if confirm == "" {
			confirm = i18nui.DialogConfirm.Text(ctx)
		}
		if cancel == "" {
			cancel = i18nui.DialogCancel.Text(ctx)
		}
		return &confirmDialogSlot{
			titleID:          titleID,
			bodyID:           bodyID,
			title:            title,
			body:             body,
			confirmLabel:     confirm,
			cancelLabel:      cancel,
			rpcMethod:        method,
			rpcPath:          rpcPath,
			successSignal:    successSignal,
			autofocusConfirm: autofocusConfirm,
			extraAttrs:       extraAttrs,
		}
	}}
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
	extraAttrs                        html.Attrs
}

func (s *confirmDialogSlot) Render() render.HTML {
	cancelAttrs := html.Attrs{
		"data-cui-rpc-close": "",
	}
	confirmAttrs := html.Attrs{
		"data-cui-rpc-close": "",
	}
	if s.rpcPath != "" {
		confirmAttrs["data-cui-rpc"] = s.rpcPath
		confirmAttrs["data-cui-rpc-method"] = s.rpcMethod
	}
	if s.successSignal != "" {
		confirmAttrs["data-cui-rpc-signal"] = s.successSignal
	}
	if s.autofocusConfirm {
		confirmAttrs["autofocus"] = ""
	}

	return confirmActionStyle.WrapHTML(html.Div(html.DivConfig{Class: "fui-confirm-action", ExtraAttrs: s.extraAttrs},
		html.Heading(html.HeadingConfig{Level: 2, Class: "fui-confirm-action__title", ID: s.titleID},
			render.Text(s.title)),
		html.Paragraph(html.TextConfig{Class: "fui-confirm-action__body", ID: s.bodyID},
			render.Text(s.body)),
		html.Div(html.DivConfig{Class: "fui-confirm-action__actions"},
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
	return `[data-cui-comp="ui-confirm-action"] {
  display: block;
  max-inline-size: var(--ui-confirm-action-max-width, 28rem);
}
[data-cui-comp="ui-confirm-action"] .fui-confirm-action__title {
  margin: 0 0 var(--spacing-sm, 4px) 0;
  font-size: var(--text-lg, 1.125rem);
  font-weight: var(--font-weight-semibold);
  color: var(--color-text, #09090B);
}
[data-cui-comp="ui-confirm-action"] .fui-confirm-action__body {
  margin: 0 0 var(--spacing-lg, 16px) 0;
  color: var(--color-text-muted, #52525B);
  line-height: var(--leading-normal, 1.5);
}
[data-cui-comp="ui-confirm-action"] .fui-confirm-action__actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--spacing-sm, 4px);
}
`
}
