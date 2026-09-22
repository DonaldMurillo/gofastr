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

// ─── Repeater ───────────────────────────────────────────────────────
//
// A dynamic list of form rows over headless.Repeater: every row is the
// caller's fields, the add and remove controls are named submit
// buttons, and when RPCPath is set the same buttons carry the
// framework's RPC contract with the operation in the endpoint's query,
// so the region swaps in place and the module restores focus to the
// row or the add control.

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
	// RPCPath, when set, makes add/remove a region update: the
	// endpoint the operations POST to and the signal whose region the
	// answer replaces (the items container, "<ID>-items").
	RPCPath string

	// ExtraAttrs forwards additional attributes to the repeater's root
	// element. Keys the component owns are dropped: class, id,
	// data-fui-*, and every data-hui-* hook.
	ExtraAttrs html.Attrs

	// Ctx carries the per-request context used to resolve i18n strings
	// (AddLabel, RemoveLabel defaults). When nil, context.Background()
	// is used and English fallbacks are returned.
	Ctx context.Context
}

// repeaterClasses dresses headless.Repeater's parts in this package's
// own vocabulary — the names the registered ui-repeater sheet matches.
var repeaterClasses = headless.Classes{
	headless.PartRoot:           "fui-repeater",
	headless.PartLabel:          "fui-repeater__label",
	headless.PartRepeaterItems:  "fui-repeater__items",
	headless.PartRepeaterItem:   "fui-repeater__item",
	headless.PartRepeaterFields: "fui-repeater__item-fields",
	headless.PartActions:        "fui-repeater__item-actions",
	headless.PartDismiss:        "fui-repeater__remove",
	headless.PartRepeaterAdd:    "fui-repeater__add",
	headless.PartStatus:         "fui-visually-hidden",
}

// Repeater renders a dynamic list of form fields with add/remove controls.
func Repeater(cfg RepeaterConfig) render.HTML {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	id := cfg.ID
	if id == "" {
		id = autoID("rep")
	}

	items := make([]headless.RepeaterItem, 0, len(cfg.Items)+max(cfg.MinItems, 1))
	for _, item := range cfg.Items {
		items = append(items, headless.RepeaterItem{Fields: []render.HTML{item}})
	}
	// A Template with no seeded items seeds the minimum (one row when
	// no minimum): a repeater that renders no row and no way to add
	// one is a list that cannot start.
	if len(items) == 0 && cfg.Template != nil {
		count := cfg.MinItems
		if count == 0 {
			count = 1
		}
		for i := range count {
			items = append(items, headless.RepeaterItem{
				Fields: []render.HTML{cfg.Template(i)},
			})
		}
	}

	var island headless.Island
	if cfg.RPCPath != "" {
		island = headless.Island{Endpoint: cfg.RPCPath, Signal: id + "-items"}
	}
	if cfg.AddLabel == "" {
		cfg.AddLabel = i18nui.T(ctx, i18nui.KeyRepeaterAdd)
	}
	if cfg.RemoveLabel == "" {
		cfg.RemoveLabel = i18nui.T(ctx, i18nui.KeyRepeaterRemove)
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
	return `[data-fui-comp="ui-repeater"] { display: flex; flex-direction: column; gap: var(--spacing-sm); }
[data-fui-comp="ui-repeater"] .fui-repeater__items { display: flex; flex-direction: column; gap: var(--spacing-md); }
[data-fui-comp="ui-repeater"] .fui-repeater__item { display: flex; gap: var(--spacing-sm); align-items: flex-start; padding: var(--spacing-sm); border: 1px solid var(--color-border); border-radius: var(--radii-md); }
[data-fui-comp="ui-repeater"] .fui-repeater__item-fields { flex: 1; display: grid; gap: var(--spacing-sm); }
[data-fui-comp="ui-repeater"] .fui-repeater__add { align-self: flex-start; }
/* Scoped copy of the visually-hidden recipe: the status live region
   must not be seen on a page that loads only this sheet. */
[data-fui-comp="ui-repeater"] .fui-visually-hidden {
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
