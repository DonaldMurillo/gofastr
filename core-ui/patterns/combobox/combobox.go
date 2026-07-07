package combobox

import (
	"strconv"
	"strings"

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
	if cfg.RPCPath == "" && len(cfg.Options) == 0 {
		panic("combobox: Render requires RPCPath or Options")
	}
	if cfg.RPCPath != "" && cfg.SignalName == "" {
		panic("combobox: Render requires SignalName when RPCPath is set")
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

	// Static options take precedence over the RPC path: render the full
	// list inline (the combobox runtime module filters on input) and emit
	// no data-fui-rpc, so no network round-trip fires. Use for small fixed
	// command sets — e.g. a docs/nav palette on a static export.
	hasStatic := len(cfg.Options) > 0

	// Input carries the ARIA + binding affordances; the FORM carries
	// the RPC trigger because the runtime listens for input events at
	// document level on form[data-fui-rpc][data-fui-rpc-trigger="input"].
	//
	// SSR expanded state must match the listbox: static options render
	// visible, so the input ships aria-expanded="true" — the runtime's
	// Escape / outside-click dismissal is keyed on aria-expanded="true"
	// and would otherwise be unable to close the visibly-open listbox
	// until a keystroke re-synced the state.
	expanded := "false"
	if hasStatic {
		expanded = "true"
	}
	inputAttrs := map[string]string{
		"type":                  "text",
		"id":                    cfg.ID,
		"name":                  cfg.Name,
		"class":                 "combobox__input",
		"role":                  "combobox",
		"aria-autocomplete":     "list",
		"aria-controls":         listboxID,
		"aria-expanded":         expanded,
		"aria-activedescendant": "",
		"autocomplete":          "off",
		"spellcheck":            "false",
	}
	if cfg.Placeholder != "" {
		inputAttrs["placeholder"] = cfg.Placeholder
	}

	formAttrs := map[string]string{"class": "combobox__form"}
	if cfg.RPCPath != "" && !hasStatic {
		formAttrs["data-fui-rpc"] = cfg.RPCPath
		formAttrs["data-fui-rpc-method"] = "POST"
		formAttrs["data-fui-rpc-trigger"] = "input"
		formAttrs["data-fui-rpc-debounce-ms"] = strconv.Itoa(debounce)
		formAttrs["data-fui-rpc-signal"] = cfg.SignalName
	}
	form := render.Tag("form", formAttrs, render.Tag("input", inputAttrs))

	listboxAttrs := map[string]string{
		"id":         listboxID,
		"role":       "listbox",
		"aria-label": cfg.Label + " suggestions",
		"class":      "combobox__listbox",
	}
	if cfg.RPCPath != "" && !hasStatic {
		listboxAttrs["data-fui-signal"] = cfg.SignalName
		listboxAttrs["data-fui-signal-mode"] = "html"
	}
	var listboxBody render.HTML
	if hasStatic {
		listboxAttrs["data-fui-static-options"] = ""
		listboxBody = render.Join(staticOptionRows(listboxID, cfg.Options)...)
	} else {
		if cfg.EmptyHTML == "" {
			listboxAttrs["hidden"] = ""
		}
		listboxBody = render.Raw(cfg.EmptyHTML)
	}
	listbox := render.Tag("ul", listboxAttrs, listboxBody)

	return Style.WrapHTML(render.Tag("div", map[string]string{
		"class": wrapClass,
	}, label, form, listbox))
}

// staticOptionRows renders Options as <li role="option"> rows for a static,
// client-filtered combobox list. Each row gets a stable id (listboxID-opt-N)
// so the combobox runtime module can drive aria-activedescendant highlighting.
func staticOptionRows(listboxID string, opts []Option) []render.HTML {
	rows := make([]render.HTML, 0, len(opts))
	for i, o := range opts {
		val := o.Value
		if val == "" {
			val = o.Label
		}
		attrs := map[string]string{
			"role":       "option",
			"id":         listboxID + "-opt-" + strconv.Itoa(i),
			"data-value": val,
		}
		// The runtime hands data-fui-push-state to the SPA navigator
		// (falling back to location.href), so an unsanitized
		// javascript: Href is DOM XSS on pick. Same allow-list as
		// framework/ui's safeURL; unsafe values drop the nav affordance
		// entirely (the option still fills the input on pick).
		if href := safePushHref(o.Href); href != "" {
			attrs["data-fui-push-state"] = href
		}
		children := []render.HTML{
			render.Tag("span", map[string]string{"class": "combobox__opt-label"}, render.Text(o.Label)),
		}
		if o.Meta != "" {
			children = append(children, render.Tag("span", map[string]string{"class": "combobox__opt-meta"}, render.Text(o.Meta)))
		}
		rows = append(rows, render.Tag("li", attrs, render.Join(children...)))
	}
	return rows
}

// safePushHref returns u if it is safe to hand to the SPA navigator /
// location.href, and "" otherwise. Mirrors framework/ui's safeURL
// allow-list: http(s), mailto, tel, relative paths, fragment/query
// references. javascript:, data:, vbscript:, file:, blob:,
// protocol-relative URLs, control bytes, and encoded CR/LF are dropped.
func safePushHref(u string) string {
	if u == "" {
		return ""
	}
	for i := 0; i < len(u); i++ {
		c := u[i]
		if c < 0x20 || c == 0x7f {
			return ""
		}
	}
	trimmed := strings.TrimLeft(u, " \t")
	low := strings.ToLower(trimmed)
	if strings.Contains(low, "%0d") || strings.Contains(low, "%0a") {
		return ""
	}
	if strings.HasPrefix(trimmed, "//") {
		return ""
	}
	if strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "#") ||
		strings.HasPrefix(trimmed, "?") || strings.HasPrefix(trimmed, "./") ||
		strings.HasPrefix(trimmed, "../") {
		return u
	}
	for i := 0; i < len(trimmed); i++ {
		c := trimmed[i]
		if c == ':' {
			switch strings.ToLower(trimmed[:i]) {
			case "http", "https", "mailto", "tel":
				return u
			default:
				return ""
			}
		}
		if c == '/' || c == '?' || c == '#' {
			return u // no scheme — relative reference
		}
	}
	return u // bare relative reference
}
