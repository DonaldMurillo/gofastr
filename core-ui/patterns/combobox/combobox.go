package combobox

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Style is the registered stylesheet handle. CSS auto-loads on first
// appearance via the runtime's data-fui-comp scanner. Apps override
// the visual defaults via theme tokens.
var Style = registry.RegisterStyle("combobox", styleFn)

// Render renders the combobox shell: label, input with combobox
// semantics, and the empty listbox the search RPC populates.
func Render(cfg Config) render.HTML {
	if cfg.ID == "" {
		panic("combobox: Render requires ID")
	}
	if cfg.Name == "" {
		panic("combobox: Render requires Name")
	}
	if cfg.Label == "" {
		panic("combobox: Render requires Label")
	}
	if cfg.RPCPath == "" {
		panic("combobox: Render requires RPCPath")
	}
	if cfg.SignalName == "" {
		panic("combobox: Render requires SignalName")
	}
	debounce := cfg.DebounceMs
	if debounce <= 0 {
		debounce = 250
	}

	listboxID := cfg.ID + "-listbox"

	wrapClass := "combobox"
	if cfg.Class != "" {
		wrapClass += " " + cfg.Class
	}

	labelClass := "combobox__label"
	if cfg.LabelHidden {
		labelClass += " ui-visually-hidden"
	}
	label := render.Tag("label", map[string]string{
		"class": labelClass,
		"for":   cfg.ID,
	}, render.Text(cfg.Label))

	// Input carries the ARIA + binding affordances; the FORM carries
	// the RPC trigger because the runtime listens for input events at
	// document level on form[data-fui-rpc][data-fui-rpc-trigger="input"].
	inputAttrs := map[string]string{
		"type":                  "text",
		"id":                    cfg.ID,
		"name":                  cfg.Name,
		"class":                 "combobox__input",
		"role":                  "combobox",
		"aria-autocomplete":     "list",
		"aria-controls":         listboxID,
		"aria-expanded":         "false",
		"aria-activedescendant": "",
		"autocomplete":          "off",
		"spellcheck":            "false",
	}
	if cfg.Placeholder != "" {
		inputAttrs["placeholder"] = cfg.Placeholder
	}

	form := render.Tag("form", map[string]string{
		"class":                    "combobox__form",
		"data-fui-rpc":             cfg.RPCPath,
		"data-fui-rpc-method":      "POST",
		"data-fui-rpc-trigger":     "input",
		"data-fui-rpc-debounce-ms": strconv.Itoa(debounce),
		"data-fui-rpc-signal":      cfg.SignalName,
	}, render.Tag("input", inputAttrs))

	listboxAttrs := map[string]string{
		"id":                    listboxID,
		"role":                  "listbox",
		"aria-label":            cfg.Label + " suggestions",
		"class":                 "combobox__listbox",
		"data-fui-signal":       cfg.SignalName,
		"data-fui-signal-mode":  "html",
	}
	if cfg.EmptyHTML == "" {
		listboxAttrs["hidden"] = ""
	}
	listbox := render.Tag("ul", listboxAttrs, render.Raw(cfg.EmptyHTML))

	return Style.WrapHTML(render.Tag("div", map[string]string{
		"class": wrapClass,
	}, label, form, listbox))
}
