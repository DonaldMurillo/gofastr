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

// InlineEditConfig configures an InlineEdit.
type InlineEditConfig struct {
	// Display is the value as the page shows it. Required.
	Display render.HTML
	// Label names the trigger and the field ("Edit Status of INV-1").
	// Required.
	Label string
	// Control is the one input the form submits. Required.
	Control render.HTML
	// Action is the write, sent as Method "PUT" (default) or "PATCH";
	// Return is where a saved edit navigates. Both are required
	// same-origin paths.
	Action, Method, Return string
	// SaveLabel and Saved name the button and the toast.
	SaveLabel, Saved string
	ID               string
	// ExtraAttrs forwards attributes to the root; keys the component
	// owns are dropped.
	ExtraAttrs html.Attrs
	Ctx        context.Context
}

// InlineEdit renders a value whose trigger opens an editor over it;
// a save toasts Saved and navigates to Return, and without script the
// form posts like any other.
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
		save = i18nui.InlineEditSave.Text(ctx)
	}
	if saved == "" {
		saved = i18nui.InlineEditSaved.Text(ctx)
	}
	rpc := interactive.Put(cfg.Action)
	if strings.EqualFold(cfg.Method, "PATCH") {
		rpc = interactive.Patch(cfg.Action)
	}
	attrs := rpc.OnSuccessToast(saved).OnSuccess(interactive.Navigate(cfg.Return)).Attrs()
	form := Form(FormConfig{
		Action:     cfg.Action,
		Method:     "POST",
		HideSubmit: true,
		Ctx:        ctx,
		ExtraAttrs: attrs,
	}, Cluster(ClusterConfig{Gap: GapSM, Align: AlignCenter, NoWrap: true},
		cfg.Control,
		headless.Own(Button(ButtonConfig{Ctx: ctx, Label: save, Type: "submit", Variant: ButtonPrimary, Size: ButtonSizeSmall})),
	))
	summary := render.Join(
		html.Span(html.TextConfig{Class: "fui-inline-edit__value"}, cfg.Display),
		html.Span(html.TextConfig{Class: "fui-visually-hidden", ExtraAttrs: html.Attrs{"data-cui-internal": ""}}, render.Text(cfg.Label)),
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
[data-cui-comp="ui-inline-edit"] > .fui-inline-edit__trigger:hover {
  background: var(--color-surface-soft, var(--color-surface));
}
[data-cui-comp="ui-inline-edit"] > .fui-inline-edit__trigger:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
[data-cui-comp="ui-inline-edit"] > .fui-inline-edit__panel {
  position: absolute;
  inset-block-start: 50%;
  inset-inline-start: calc(-1 * var(--spacing-md, 8px));
  translate: 0 -50%;
  z-index: var(--z-dropdown, 100);
  inline-size: max-content;
  min-inline-size: var(--ui-inline-edit-min-inline-size, 12rem);
  max-inline-size: min(var(--ui-inline-edit-max-inline-size, 26rem), 90vw);
  padding: var(--spacing-xs, 2px);
  background: var(--color-surface);
  border: var(--stroke-thin, 1px) solid var(--color-border-strong, var(--color-border));
  border-radius: var(--radii-md, 8px);
  box-shadow: var(--shadow-lg);
  white-space: normal;
  text-align: start;
}`
}
