package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// InlineEditConfig configures an inline-editable text field.
type InlineEditConfig struct {
	Name        string
	Value       string
	ID          string
	Placeholder string
	RPCPath     string
	Type        string
	Required    bool
	EmptyText   string
	ARIALabel   string
}

// InlineEdit renders a span that becomes an input on click.
func InlineEdit(cfg InlineEditConfig) render.HTML {
	if cfg.ID == "" {
		cfg.ID = autoID("iedit")
	}
	if cfg.Type == "" {
		cfg.Type = "text"
	}
	if cfg.EmptyText == "" {
		cfg.EmptyText = "Click to edit"
	}
	if cfg.ARIALabel == "" {
		cfg.ARIALabel = "Edit " + cfg.Name
	}

	displayValue := cfg.Value
	if displayValue == "" {
		displayValue = cfg.EmptyText
	}

	displayAttrs := map[string]string{
		"class":                "ui-inline-edit-display",
		"data-fui-inline-edit": cfg.ID,
		"role":                 "button",
		"tabindex":             "0",
		"aria-label":           cfg.ARIALabel,
	}
	if cfg.Value == "" {
		displayAttrs["data-empty"] = ""
	}

	return inlineEditStyle.WrapHTML(html.Div(html.DivConfig{Class: "ui-inline-edit"},
		html.Span(html.TextConfig{Attrs: displayAttrs}, render.HTML(displayValue)),
		html.Input(html.InputConfig{
			Type:  cfg.Type,
			Name:  cfg.Name,
			ID:    cfg.ID,
			Class: "ui-inline-edit-input",
			Attrs: func() map[string]string {
				a := map[string]string{
					"data-fui-rpc":        cfg.RPCPath,
					"data-fui-rpc-method": "POST",
					"hidden":              "",
				}
				if cfg.Value != "" {
					a["value"] = cfg.Value
				}
				if cfg.Placeholder != "" {
					a["placeholder"] = cfg.Placeholder
				}
				if cfg.Required {
					a["required"] = ""
				}
				return a
			}(),
		}),
		html.Input(html.InputConfig{
			Type:  "hidden",
			Name:  cfg.Name + "_original",
			ID:    cfg.ID + "-original",
			Class: "ui-inline-edit-original",
			Attrs: func() map[string]string {
				a := map[string]string{}
				if cfg.Value != "" {
					a["value"] = cfg.Value
				}
				return a
			}(),
		}),
	))
}

var inlineEditStyle = registry.RegisterStyle("ui-inline-edit", inlineEditCSS)

func inlineEditCSS(t style.Theme) string {
	return `.ui-inline-edit { display: inline-flex; align-items: center; gap: var(--spacing-xs); }
.ui-inline-edit-display { cursor: pointer; padding: var(--spacing-xs) var(--spacing-sm); border-radius: var(--radius-sm); border: 1px solid transparent; transition: border-color var(--duration-fast); }
.ui-inline-edit-display:hover { border-color: var(--color-border); }
.ui-inline-edit-display[data-empty] { color: var(--color-text-muted); font-style: italic; }
.ui-inline-edit-input { display: none; }
.ui-inline-edit.editing .ui-inline-edit-display { display: none; }
.ui-inline-edit.editing .ui-inline-edit-input { display: inline-block; }`
}
