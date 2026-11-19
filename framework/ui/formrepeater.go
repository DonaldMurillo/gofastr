package ui

import (
	"context"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// FormRepeaterConfig configures a dynamic repeating field group.
type FormRepeaterConfig struct {
	// ID is the caller's; empty derives one from Name within Ctx's scope.
	ID string
	// Name prefixes the add and remove submit names. Required.
	Name string

	// Items holds each row's rendered fields.
	Items [][]render.HTML

	// MinItems is the row count below which Remove is disabled.
	MinItems int

	// MaxItems is the row count at which Add is disabled; zero is unlimited.
	MaxItems    int
	AddLabel    string
	RemoveLabel string
	Class       string
	ExtraAttrs  html.Attrs

	// Ctx resolves the default labels; nil reads English.
	Ctx context.Context
}

var formRepeaterClasses = headless.Classes{
	headless.PartRoot:           "fui-form-repeater",
	headless.PartLabel:          "fui-visually-hidden",
	headless.PartRepeaterItems:  "fui-form-repeater__items",
	headless.PartRepeaterItem:   "fui-form-repeater__item",
	headless.PartRepeaterFields: "fui-form-repeater__item-fields",
	headless.PartActions:        "fui-form-repeater__item-actions",
	headless.PartDismiss:        buttonClassTokens(ButtonDanger, ButtonSizeDefault) + " fui-form-repeater__remove",
	headless.PartRepeaterAdd:    buttonClassTokens(ButtonSecondary, ButtonSizeDefault) + " fui-form-repeater__add",
	headless.PartStatus:         "fui-visually-hidden",
}

// FormRepeater renders repeating field groups whose Add submits
// <Name>_add=1 and whose Remove submits <Name>_remove=<index>.
func FormRepeater(cfg FormRepeaterConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: FormRepeater requires Name")
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg.MaxItems > 0 && cfg.MinItems > cfg.MaxItems {
		panic("ui: FormRepeater MinItems (" + strconv.Itoa(cfg.MinItems) +
			") must not exceed MaxItems (" + strconv.Itoa(cfg.MaxItems) + ")")
	}

	items := make([]headless.RepeaterItem, len(cfg.Items))
	for i, fields := range cfg.Items {
		items[i] = headless.RepeaterItem{Fields: fields}
	}

	parts := headless.Parts{}
	if cfg.Class != "" {
		parts.Attrs = headless.PartAttrs{headless.PartRoot: {"class": cfg.Class}}
	}

	return formRepeaterStyle.WrapHTML(headless.Repeater(headless.RepeaterProps{
		Name:        cfg.Name,
		Items:       items,
		MinItems:    cfg.MinItems,
		MaxItems:    cfg.MaxItems,
		AddLabel:    cfg.AddLabel,
		RemoveLabel: cfg.RemoveLabel,
		AddName:     cfg.Name + "_add",
		AddValue:    "1",
		RemoveName:  cfg.Name + "_remove",
		ID:          scopedControlID(cfg.Ctx, cfg.ID, cfg.Name, "-items"),
		ExtraAttrs:  headless.Safe(cfg.ExtraAttrs, "class", "id", "role", "aria-label", "aria-live"),
		Parts:       parts,
		Strings:     StringsFor(ctx),
	}, formRepeaterClasses))
}

func formRepeaterCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-form-repeater"] {
  display: grid;
  gap: var(--spacing-md);
}
[data-cui-comp="ui-form-repeater"] .fui-form-repeater__items {
  display: grid;
  gap: var(--spacing-md);
  list-style: none;
  margin: 0;
  padding: 0;
}
[data-cui-comp="ui-form-repeater"] .fui-form-repeater__item {
  display: grid;
  gap: var(--spacing-sm);
  padding: var(--spacing-md);
  border: var(--stroke-thin, 1px) solid var(--color-border);
  border-radius: var(--radii-md);
  background: var(--color-surface);
}
[data-cui-comp="ui-form-repeater"] .fui-form-repeater__item-fields {
  display: grid;
  gap: var(--spacing-sm);
}
[data-cui-comp="ui-form-repeater"] .fui-form-repeater__item-actions {
  display: flex;
  justify-content: flex-end;
}
[data-cui-comp="ui-form-repeater"] .fui-form-repeater__add {
  justify-self: start;
}
/* Scoped copy of the visually-hidden recipe: the group label and the
   status live region must not be seen on a page that loads only this
   sheet. */
[data-cui-comp="ui-form-repeater"] .fui-visually-hidden {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
}
`
}
