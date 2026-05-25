package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// repeaterAppendQuery appends a query string to a URL using `?` when the
// URL has no existing query string, or `&` otherwise.
func repeaterAppendQuery(base, q string) string {
	if strings.Contains(base, "?") {
		return base + "&" + q
	}
	return base + "?" + q
}

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
	RPCPath     string
}

// Repeater renders a dynamic list of form fields with add/remove controls.
func Repeater(cfg RepeaterConfig) render.HTML {
	if cfg.ID == "" {
		cfg.ID = autoID("rep")
	}
	if cfg.AddLabel == "" {
		cfg.AddLabel = i18nui.T(context.Background(), i18nui.KeyRepeaterAdd)
	}
	if cfg.RemoveLabel == "" {
		cfg.RemoveLabel = i18nui.T(context.Background(), i18nui.KeyRepeaterRemove)
	}

	var children []render.HTML

	if cfg.Label != "" {
		children = append(children, html.Label(html.LabelConfig{
			For:  cfg.ID + "-items",
			Text: cfg.Label,
		}))
	}

	var itemHTML []render.HTML
	for i, item := range cfg.Items {
		itemHTML = append(itemHTML, html.Div(html.DivConfig{
			Class: "ui-repeater-item",
			ExtraAttrs: map[string]string{"data-index": strconv.Itoa(i)},
		}, item, repeaterRemoveBtn(cfg, i)))
	}

	if len(itemHTML) == 0 && cfg.Template != nil {
		count := cfg.MinItems
		if count == 0 {
			count = 1
		}
		for i := 0; i < count; i++ {
			itemHTML = append(itemHTML, html.Div(html.DivConfig{
				Class: "ui-repeater-item",
				ExtraAttrs: map[string]string{"data-index": strconv.Itoa(i)},
			}, cfg.Template(i), repeaterRemoveBtn(cfg, i)))
		}
	}

	children = append(children, html.Div(html.DivConfig{
		Class: "ui-repeater-items",
		ExtraAttrs: map[string]string{
			"id":             cfg.ID + "-items",
			"data-repeater":  cfg.Name,
			"data-min-items": strconv.Itoa(cfg.MinItems),
			"data-max-items": strconv.Itoa(cfg.MaxItems),
			// Announce add/remove to AT users without forcing
			// per-callsite ARIA wiring.
			"role":      "region",
			"aria-live": "polite",
		},
	}, itemHTML...))

	addAttrs := map[string]string{"type": "button", "class": "ui-repeater-add"}
	if cfg.RPCPath != "" {
		addAttrs["data-fui-rpc"] = repeaterAppendQuery(cfg.RPCPath, "action=add")
		addAttrs["data-fui-rpc-method"] = "POST"
		addAttrs["data-fui-rpc-signal"] = cfg.ID + "-items"
	}
	children = append(children, html.Button(html.ButtonConfig{
		Label: cfg.AddLabel,
		ExtraAttrs: addAttrs,
	}))

	return repeaterStyle.WrapHTML(html.Div(html.DivConfig{Class: "ui-repeater"}, children...))
}

func repeaterRemoveBtn(cfg RepeaterConfig, index int) render.HTML {
	attrs := map[string]string{
		"class":     "ui-repeater-remove",
		"aria-label": fmt.Sprintf("Remove item %d", index+1),
	}
	if cfg.RPCPath != "" {
		attrs["data-fui-rpc"] = repeaterAppendQuery(cfg.RPCPath, fmt.Sprintf("action=remove&index=%d", index))
		attrs["data-fui-rpc-method"] = "POST"
		attrs["data-fui-rpc-signal"] = cfg.ID + "-items"
	}
	if cfg.MinItems > 0 && index < cfg.MinItems {
		attrs["hidden"] = ""
	}
	return html.Button(html.ButtonConfig{
		Label: cfg.RemoveLabel,
		ExtraAttrs: attrs,
	})
}

var repeaterStyle = registry.RegisterStyle("ui-repeater", repeaterCSS)

func repeaterCSS(t style.Theme) string {
	return `.ui-repeater { display: flex; flex-direction: column; gap: var(--spacing-sm); }
.ui-repeater-items { display: flex; flex-direction: column; gap: var(--spacing-md); }
.ui-repeater-item { display: flex; gap: var(--spacing-sm); align-items: flex-start; padding: var(--spacing-sm); border: 1px solid var(--color-border); border-radius: var(--radius-md); }
.ui-repeater-add { align-self: flex-start; }`
}
