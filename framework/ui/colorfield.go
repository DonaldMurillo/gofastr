package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── ColorField ─────────────────────────────────────────────────────
//
// A colour swatch beside a text input holding the same value, as one control.
//
// [ColorPicker] pairs a swatch with a LABEL, which is the right shape for a
// form asking "pick a colour". It is the wrong shape when the exact value
// matters and has to be readable, typed and pasted: a theme token, a brand
// hex, a chart series colour. Those need both affordances on one row, and the
// text input is the source of truth: a swatch alone cannot express "inherit",
// "transparent", or a var() reference, and cannot be pasted from a brand guide.
//
// The caller owns the wiring between the two inputs (they are its inputs, via
// SwatchAttrs/TextAttrs); this component owns the row.

// ColorFieldConfig configures a ColorField.
type ColorFieldConfig struct {
	// Value is the authoritative value, placed in the text input verbatim.
	Value string
	// SwatchValue is what the native colour input shows. It differs from
	// Value whenever Value is not a plain hex: the native control cannot
	// represent "transparent" or "var(--x)" and silently falls back to black,
	// so the caller resolves a display colour itself.
	SwatchValue string
	// TextID is the id of the text input, for a label's `for`.
	TextID string
	// SwatchLabel is the accessible name for the swatch, which has no visible
	// label of its own. Required. An unlabelled colour input is announced as
	// nothing.
	SwatchLabel string
	// TextLabel is the accessible name for the text input. Defaults to
	// SwatchLabel.
	//
	// BOTH inputs need a name. The text input is the one carrying the value, so
	// leaving it unnamed is the worse of the two omissions, and it is the easy
	// one to miss, because a caller that wraps this in its own <label for=…>
	// sees a labelled control while a caller that does not ships a critical axe
	// violation. Set aria-label or aria-labelledby in TextAttrs to take over.
	TextLabel string
	// SwatchAttrs and TextAttrs are merged onto the respective inputs, for
	// the data-* attributes a caller's own JS binds to.
	SwatchAttrs html.Attrs
	TextAttrs   html.Attrs

	Class string
}

// ColorField renders the swatch + text-input row.
func ColorField(cfg ColorFieldConfig) render.HTML {
	if cfg.SwatchLabel == "" {
		panic("ui: ColorField requires SwatchLabel. A colour input with no accessible name is announced as nothing")
	}
	swatch := html.Attrs{
		"type":       "color",
		"class":      "ui-color-field__swatch",
		"aria-label": cfg.SwatchLabel,
	}
	if cfg.SwatchValue != "" {
		swatch["value"] = cfg.SwatchValue
	}
	for k, v := range cfg.SwatchAttrs {
		swatch[k] = v
	}

	text := html.Attrs{
		"type":  "text",
		"class": "ui-color-field__text",
		"value": cfg.Value,
	}
	if cfg.TextID != "" {
		text["id"] = cfg.TextID
	}
	for k, v := range cfg.TextAttrs {
		text[k] = v
	}
	// Name the text input unless the caller already did. Applied after the
	// merge so an explicit aria-label/aria-labelledby wins, and so a caller
	// pointing at its own visible label is not overridden.
	if text["aria-label"] == "" && text["aria-labelledby"] == "" {
		name := cfg.TextLabel
		if name == "" {
			name = cfg.SwatchLabel
		}
		text["aria-label"] = name
	}

	cls := "ui-color-field"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	return colorFieldStyle.WrapHTML(render.Tag("div", html.Attrs{"class": cls},
		render.VoidTag("input", swatch),
		render.VoidTag("input", text),
	))
}

var colorFieldStyle = registry.RegisterStyle("ui-color-field", colorFieldCSS)

func colorFieldCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-color-field"] {
  display: flex;
  align-items: center;
  gap: var(--spacing-sm, 8px);
  inline-size: 100%;
}

[data-fui-comp="ui-color-field"] .ui-color-field__swatch {
  flex: 0 0 auto;
  /* Square, and at least a comfortable tap target. */
  inline-size: var(--spacing-touch-target, 44px);
  block-size: var(--spacing-touch-target, 44px);
  padding: 2px;
  border: 1px solid var(--color-border, #e4e4e7);
  border-radius: var(--radii-sm, 4px);
  background-color: var(--color-surface, #fff);
  cursor: pointer;
}

[data-fui-comp="ui-color-field"] .ui-color-field__text {
  /* The value is the point, so the text input takes the room. min-inline-size
     resets the intrinsic width an <input> otherwise insists on, which would
     push the row wider than its container. */
  flex: 1 1 auto;
  min-inline-size: 0;
  font-family: var(--font-mono, ui-monospace, monospace);
}`
}
