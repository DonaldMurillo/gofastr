package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// RepeaterConfig configures a dynamic form repeater.
type RepeaterConfig struct {
	Name        string
	Label       string
	ID          string
	MinItems    int
	MaxItems    int
	AddLabel    string
	RemoveLabel string
	Template    func(index int) render.HTML
	Items       []render.HTML
	// RPCPath makes add and remove an island update of "<ID>-items".
	RPCPath    string
	ExtraAttrs html.Attrs

	// Ctx resolves the default labels; nil reads English.
	Ctx context.Context
}

var repeaterClasses = headless.Classes{
	headless.PartRoot:           "fui-repeater",
	headless.PartLabel:          "fui-repeater__label",
	headless.PartRepeaterItems:  "fui-repeater__items",
	headless.PartRepeaterItem:   "fui-repeater__item",
	headless.PartRepeaterFields: "fui-repeater__item-fields",
	headless.PartActions:        "fui-repeater__item-actions",
	headless.PartDismiss:        buttonClassTokens(ButtonGhost, ButtonSizeDefault) + " fui-repeater__remove",
	headless.PartRepeaterAdd:    buttonClassTokens(ButtonSecondary, ButtonSizeDefault) + " fui-repeater__add",
	headless.PartStatus:         "fui-visually-hidden",
}

func repeaterFields(item render.HTML) []render.HTML {
	if item == "" {
		return nil
	}
	return []render.HTML{item}
}

// Repeater renders a dynamic list of form fields with add/remove controls.
func Repeater(cfg RepeaterConfig) render.HTML {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	id := cfg.ID
	if id == "" {
		id = scopedID(cfg.Ctx, "rep", "-items")
	} else {
		render.ReserveID(cfg.Ctx, id, "-items")
	}

	items := make([]headless.RepeaterItem, 0, len(cfg.Items)+max(cfg.MinItems, 1))
	for _, item := range cfg.Items {
		items = append(items, headless.RepeaterItem{Fields: repeaterFields(item)})
	}
	if len(items) == 0 && cfg.Template != nil {
		count := cfg.MinItems
		if count == 0 {
			count = 1
		}
		for i := range count {
			items = append(items, headless.RepeaterItem{
				Fields: repeaterFields(cfg.Template(i)),
			})
		}
	}

	var island headless.Island
	if cfg.RPCPath != "" {
		island = headless.Island{Endpoint: cfg.RPCPath, Signal: id + "-items"}
	}
	if cfg.AddLabel == "" {
		cfg.AddLabel = i18nui.RepeaterAdd.Text(ctx)
	}
	if cfg.RemoveLabel == "" {
		cfg.RemoveLabel = i18nui.RepeaterRemove.Text(ctx)
	}

	parts := headless.Parts{}
	return repeaterStyle.WrapHTML(headless.Repeater(headless.RepeaterProps{
		Name:        cfg.Name,
		Label:       cfg.Label,
		Items:       items,
		MinItems:    cfg.MinItems,
		MaxItems:    cfg.MaxItems,
		AddLabel:    cfg.AddLabel,
		RemoveLabel: cfg.RemoveLabel,
		Action:      cfg.RPCPath,
		Island:      island,
		ID:          id,
		ExtraAttrs:  headless.Safe(cfg.ExtraAttrs, "class", "id", "role", "aria-label"),
		Parts:       parts,
		Strings:     StringsFor(ctx),
	}, repeaterClasses))
}

var repeaterStyle = registry.RegisterStyle("ui-repeater", repeaterCSS)

func repeaterCSS(t style.Theme) string {
	return `[data-cui-comp="ui-repeater"] { display: flex; flex-direction: column; gap: var(--spacing-sm); }
[data-cui-comp="ui-repeater"] .fui-repeater__label { font-size: var(--text-sm); font-weight: var(--font-weight-medium); color: var(--color-text); }
[data-cui-comp="ui-repeater"] .fui-repeater__items { display: flex; flex-direction: column; gap: var(--spacing-md); list-style: none; margin: 0; padding: 0; }
[data-cui-comp="ui-repeater"] .fui-repeater__item { display: flex; gap: var(--spacing-sm); align-items: flex-start; padding: var(--spacing-sm); border: var(--stroke-thin, 1px) solid var(--color-border); border-radius: var(--radii-md); }
[data-cui-comp="ui-repeater"] .fui-repeater__item-fields { flex: 1; display: grid; gap: var(--spacing-sm); }
[data-cui-comp="ui-repeater"] .fui-repeater__add { align-self: flex-start; }
/* Scoped copy of the visually-hidden recipe: the status live region
   must not be seen on a page that loads only this sheet. */
[data-cui-comp="ui-repeater"] .fui-visually-hidden {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
}`
}
