package ui

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── InlineEdit ────────────────────────────────────────────────────
//
// A value edited where it is shown: a table cell's value is the trigger
// of a small popup holding a one-field form. Saving sends the field to
// Action over the runtime's form RPC (PUT by default) and, on success,
// says Saved and navigates to Return, so the server redraws the page
// with the stored value. Escape or a press outside closes it unsaved.
// Without script the trigger still opens the popup (it is a <details>)
// and the form posts like any other.

// InlineEditConfig configures an InlineEdit.
type InlineEditConfig struct {
	// Display is the value as the page shows it. Required.
	Display render.HTML
	// Label names the edit ("Edit Status of INV-1"): the trigger's
	// accessible name and the field's label. Required.
	Label string
	// Control is the one input the form submits, named for the field.
	// Required.
	Control render.HTML
	// Action is the write the form sends; Method is "PUT" (default) or
	// "PATCH". Return is where a saved edit navigates. Required: Action
	// and Return, both same-origin paths.
	Action, Method, Return string
	// SaveLabel and Saved name the button and the toast (defaults "Save"
	// and "Saved").
	SaveLabel, Saved string
	ID               string
	// ExtraAttrs forwards attributes to the root; keys the component
	// owns are dropped.
	ExtraAttrs html.Attrs
	Ctx        context.Context
}

// InlineEdit renders the editable value.
func InlineEdit(cfg InlineEditConfig) render.HTML {
	if cfg.Display == "" || cfg.Label == "" || cfg.Control == "" {
		panic("ui: InlineEdit requires Display, Label and Control")
	}
	for _, p := range []string{cfg.Action, cfg.Return} {
		if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.ContainsRune(p, '\\') {
			panic("ui: InlineEdit Action and Return must be same-origin paths, not " + p)
		}
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	save, saved := cfg.SaveLabel, cfg.Saved
	if save == "" {
		save = i18nui.T(ctx, i18nui.KeyInlineEditSave)
	}
	if saved == "" {
		saved = i18nui.T(ctx, i18nui.KeyInlineEditSaved)
	}
	rpc := interactive.Put(cfg.Action)
	if strings.EqualFold(cfg.Method, "PATCH") {
		rpc = interactive.Patch(cfg.Action)
	}
	attrs := rpc.OnSuccessToast(saved).OnSuccess(interactive.Navigate(cfg.Return)).Attrs()
	form := Form(FormConfig{
		Action:      cfg.Action,
		Method:      "POST",
		SubmitLabel: save,
		Ctx:         ctx,
		ExtraAttrs:  attrs,
	}, cfg.Control)
	// The value is the caller's; the name and the pencil are ours.
	internal := html.Attrs{"data-cui-internal": ""}
	summary := render.Join(
		html.Span(html.TextConfig{Class: "fui-inline-edit__value"}, cfg.Display),
		html.Span(html.TextConfig{Class: "fui-visually-hidden", ExtraAttrs: internal}, render.Text(cfg.Label)),
		html.Span(html.TextConfig{Class: "fui-inline-edit__icon", ExtraAttrs: html.Attrs{"data-cui-internal": "", "aria-hidden": "true"}},
			Icon("pencil", IconConfig{Size: "14"})),
	)
	out := headless.Disclosure(headless.DisclosureProps{
		Summary:    summary,
		Content:    form,
		Dismiss:    true,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "open", "class"),
	}, headless.Classes{
		headless.PartRoot:    "fui-inline-edit",
		headless.PartSummary: "fui-inline-edit__trigger",
		headless.PartPanel:   "fui-inline-edit__panel",
	})
	return inlineEditStyle.WrapHTML(out)
}

var inlineEditStyle = registry.RegisterStyle("ui-inline-edit", inlineEditCSS)

// inlineEditCSS draws the trigger as the value itself, a pencil showing
// on hover and focus, and the panel as the dropdown's floating surface.
func inlineEditCSS(_ style.Theme) string {
	return `:where([data-cui-comp="ui-inline-edit"]).fui-inline-edit {
  position: relative;
  display: inline;
}
[data-cui-comp="ui-inline-edit"] > .fui-inline-edit__trigger {
  display: inline;
  padding: var(--spacing-xs, 2px) var(--spacing-sm, 4px);
  margin-inline: calc(-1 * var(--spacing-sm, 4px));
  border-radius: var(--radii-sm, 6px);
  cursor: text;
  list-style: none;
}
[data-cui-comp="ui-inline-edit"] > .fui-inline-edit__trigger::-webkit-details-marker { display: none; }
[data-cui-comp="ui-inline-edit"] > .fui-inline-edit__trigger:hover,
[data-cui-comp="ui-inline-edit"][open] > .fui-inline-edit__trigger {
  box-shadow: inset 0 0 0 var(--stroke-thin, 1px) var(--color-border-strong, var(--color-border));
}
[data-cui-comp="ui-inline-edit"] .fui-inline-edit__icon {
  margin-inline-start: var(--spacing-xs, 2px);
  vertical-align: middle;
  color: var(--color-text-muted);
  opacity: 0;
}
[data-cui-comp="ui-inline-edit"] > .fui-inline-edit__trigger:hover .fui-inline-edit__icon,
[data-cui-comp="ui-inline-edit"] > .fui-inline-edit__trigger:focus-visible .fui-inline-edit__icon {
  opacity: 1;
}
@media (pointer: coarse) {
  [data-cui-comp="ui-inline-edit"] .fui-inline-edit__icon { opacity: 1; }
}
[data-cui-comp="ui-inline-edit"] > .fui-inline-edit__panel {
  position: absolute;
  inset-block-start: calc(100% + var(--spacing-xs, 2px));
  inset-inline-start: 0;
  z-index: var(--z-dropdown, 100);
  min-inline-size: 16rem;
  max-inline-size: min(22rem, 90vw);
  padding: var(--spacing-md, 8px);
  background: var(--color-surface);
  border: var(--stroke-thin, 1px) solid var(--color-border);
  border-radius: var(--radii-lg, 10px);
  box-shadow: var(--shadow-lg);
  white-space: normal;
  text-align: start;
}`
}
