package ui

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── DynamicFormRepeater ────────────────────────────────────────────
//
// Add/remove repeating field groups. Server-driven: the "Add" and
// "Remove" buttons submit the form with specific name/value pairs.
// The server reads these to know which action to take and re-renders
// with the updated Items list.

// FormRepeaterConfig configures a dynamic repeating field group.
type FormRepeaterConfig struct {
	// Name is the repeater group name (used as prefix for field
	// indexing). Required.
	Name string

	// Items is the current list of rendered item groups.
	// Each item is a slice of render.HTML representing one row's fields.
	Items [][]render.HTML

	// MinItems prevents removal below this count. Default 0.
	MinItems int

	// MaxItems prevents addition above this count. Default 0 = unlimited.
	MaxItems int

	// AddLabel is the "Add" button text. Default "Add item".
	AddLabel string

	// RemoveLabel is the "Remove" button text. Default "Remove".
	RemoveLabel string

	Class string
}

// FormRepeater renders a dynamic list of repeating field groups with
// add/remove controls.
//
// Server-driven: clicking "Add" submits name="<Name>_add" value="1",
// and clicking "Remove" submits name="<Name>_remove" value="<index>".
// The server processes these and re-renders with the updated Items.
func FormRepeater(cfg FormRepeaterConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: FormRepeater requires Name")
	}

	addLabel := cfg.AddLabel
	if addLabel == "" {
		addLabel = "Add item"
	}
	removeLabel := cfg.RemoveLabel
	if removeLabel == "" {
		removeLabel = "Remove"
	}

	// D-2: Reject impossible constraint: MinItems > MaxItems.
	if cfg.MaxItems > 0 && cfg.MinItems > cfg.MaxItems {
		panic(fmt.Sprintf("ui: FormRepeater MinItems (%d) must not exceed MaxItems (%d)", cfg.MinItems, cfg.MaxItems))
	}

	cls := "ui-form-repeater"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	children := []render.HTML{}

	// Render each item row.
	for i, item := range cfg.Items {
		itemChildren := []render.HTML{}

		// Item fields.
		itemChildren = append(itemChildren, render.Tag("div", map[string]string{
			"class": "ui-form-repeater__item-fields",
		}, item...))

		// Remove button.
		removeDisabled := len(cfg.Items) <= cfg.MinItems
		removeAttrs := html.Attrs{
			"name":  cfg.Name + "_remove",
			"value": fmt.Sprintf("%d", i),
		}
		if removeDisabled {
			removeAttrs["disabled"] = ""
		}
		removeBtn := html.Button(html.ButtonConfig{
			Label: removeLabel,
			Type:  "submit",
			Class: "ui-button ui-button--danger ui-button--small",
			ExtraAttrs: removeAttrs,
		})
		itemChildren = append(itemChildren, render.Tag("div", map[string]string{
			"class": "ui-form-repeater__item-actions",
		}, removeBtn))

		itemDiv := render.Tag("div", map[string]string{
			"class": "ui-form-repeater__item",
			"data-index": fmt.Sprintf("%d", i),
		}, itemChildren...)
		children = append(children, itemDiv)
	}

	// Add button.
	addDisabled := cfg.MaxItems > 0 && len(cfg.Items) >= cfg.MaxItems
	addAttrs := html.Attrs{
		"name":  cfg.Name + "_add",
		"value": "1",
	}
	if addDisabled {
		addAttrs["disabled"] = ""
	}
	addBtn := html.Button(html.ButtonConfig{
		Label: addLabel,
		Type:  "submit",
		Class: "ui-button ui-button--secondary",
		ExtraAttrs: addAttrs,
	})
	children = append(children, render.Tag("div", map[string]string{
		"class": "ui-form-repeater__add",
	}, addBtn))

	return formRepeaterStyle.WrapHTML(render.Tag("div", map[string]string{
		"data-fui-comp": "ui-form-repeater",
		"class":         cls,
		"aria-label":    cfg.Name + " items",
		"aria-live":     "polite",
	}, children...))
}

// formRepeaterStyle is registered in styles_components.go

func formRepeaterCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-form-repeater"] {
  display: grid;
  gap: var(--spacing-md, 8px);
}
[data-fui-comp="ui-form-repeater"] .ui-form-repeater__item {
  display: grid;
  gap: var(--spacing-sm, 4px);
  padding: var(--spacing-md, 8px);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
}
[data-fui-comp="ui-form-repeater"] .ui-form-repeater__item-fields {
  display: grid;
  gap: var(--spacing-sm, 4px);
}
[data-fui-comp="ui-form-repeater"] .ui-form-repeater__item-actions {
  display: flex;
  justify-content: flex-end;
}
[data-fui-comp="ui-form-repeater"] .ui-form-repeater__add {
  display: flex;
  justify-content: flex-start;
}
`
}
