package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/render"

	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

func confirmDialogTemplate(ctx context.Context) render.HTML {
	confirm := i18nui.DialogConfirm.Text(ctx)
	button := func(label string, v ButtonVariant, part string) render.HTML {
		return Button(ButtonConfig{
			Ctx:        ctx,
			Label:      label,
			Variant:    v,
			ExtraAttrs: html.Attrs{"data-cui-confirm-part": part},
		})
	}
	return confirmDialogStyle.WrapHTML(render.Tag("dialog", map[string]string{
		"class":            "fui-confirm-dialog",
		"role":             "alertdialog",
		"aria-modal":       "true",
		"aria-labelledby":  "fui-confirm-dialog-title",
		"aria-describedby": "fui-confirm-dialog-message",
	},
		html.Heading(html.HeadingConfig{
			Level: 2, ID: "fui-confirm-dialog-title", Class: "fui-confirm-dialog__title",
			ExtraAttrs: html.Attrs{"data-cui-confirm-part": "title", "data-cui-internal": ""},
		}, render.Text(i18nui.DialogConfirmTitle.Text(ctx))),
		html.Paragraph(html.TextConfig{
			ID: "fui-confirm-dialog-message", Class: "fui-confirm-dialog__message",
			ExtraAttrs: html.Attrs{"data-cui-confirm-part": "message", "data-cui-internal": ""},
		}),
		html.Div(html.DivConfig{
			Class:      "fui-confirm-dialog__actions",
			ExtraAttrs: html.Attrs{"data-cui-internal": ""},
		},
			button(i18nui.DialogCancel.Text(ctx), ButtonSecondary, "cancel"),
			button(confirm, ButtonPrimary, "accept"),
			button(confirm, ButtonDanger, "accept-danger"),
		),
	))
}

var _ = registry.RegisterTemplate(preset.ConfirmTemplate, confirmDialogTemplate)

var confirmDialogStyle = registry.RegisterStyle("ui-confirm-dialog", confirmDialogCSS)

func confirmDialogCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-confirm-dialog"] {
  box-sizing: border-box;
  inline-size: min(var(--ui-confirm-dialog-max-width, 28rem), calc(100vw - 2 * var(--spacing-lg, 16px)));
  max-inline-size: none;
  margin: auto;
  padding: var(--spacing-lg, 16px);
  background: var(--color-surface);
  color: var(--color-text);
  border: var(--stroke-thin, 1px) solid var(--color-border);
  border-radius: var(--radii-lg, 10px);
  box-shadow: var(--shadow-xl);
}
[data-cui-comp="ui-confirm-dialog"][open] {
  animation: fui-confirm-dialog-in var(--duration-overlay-enter) var(--easing-ease-out);
}
[data-cui-comp="ui-confirm-dialog"]::backdrop {
  background: var(--ui-scrim, rgba(0,0,0,0.45));
}
@keyframes fui-confirm-dialog-in {
  from { opacity: 0; transform: scale(0.96); }
}
@media (prefers-reduced-motion: reduce) {
  [data-cui-comp="ui-confirm-dialog"][open] { animation: none; }
}
[data-cui-comp="ui-confirm-dialog"] .fui-confirm-dialog__title {
  margin: 0 0 var(--spacing-sm, 4px) 0;
  font-size: var(--text-lg, 1.125rem);
  font-weight: var(--font-weight-semibold);
}
[data-cui-comp="ui-confirm-dialog"] .fui-confirm-dialog__message {
  margin: 0 0 var(--spacing-lg, 16px) 0;
  color: var(--color-text-muted);
  line-height: var(--leading-normal, 1.5);
}
[data-cui-comp="ui-confirm-dialog"] .fui-confirm-dialog__actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: var(--spacing-sm, 4px);
}
`
}
