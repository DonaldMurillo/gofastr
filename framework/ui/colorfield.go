package ui

import (
	"context"
	"maps"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── ColorField ─────────────────────────────────────────────────────
//
// The affix-shell colour control, rendered through headless.Color: the
// native picker reduced to a swatch beside the hex text, so the field
// reads as one control-height input. The text input is the source of
// truth and the control that submits; the swatch is a picker bound to
// it (data-hui-affix-swatch, out of the tab order so there is exactly
// one focus target), and the headless behaviour module keeps the two
// one value through data-hui-color.
//
// A value the picker cannot show — "transparent", "var(--x)", a CSS
// colour name — is kept verbatim in the text and the shell is marked
// data-invalid rather than the value being degraded: this component
// edits config, and rewriting an unpickable value to black would
// destroy it.

// ColorFieldConfig configures a ColorField.
type ColorFieldConfig struct {
	// Name is the hex text input's form-field name (required): the
	// text input is the control that submits.
	Name string
	// Value is the authoritative value, placed in the text input
	// verbatim. When it is not #rgb or #rrggbb the swatch falls back
	// to black and the shell is marked data-invalid.
	Value string
	// TextID is the id of the text input, for a label's `for`.
	// A Field's id wins over it when both are set.
	TextID string
	// SwatchLabel is required. The swatch's own accessible name comes
	// from the component's PickColor word plus Name; SwatchLabel
	// names the TEXT input when TextLabel is empty — the input that
	// carries the value is the worse of the two to leave unnamed,
	// and the easy one to miss, because a caller that wraps this in
	// its own <label for=…> sees a labelled control while a caller
	// that does not ships an unnamed critical control.
	SwatchLabel string
	// TextLabel is the accessible name for the text input. Defaults
	// to SwatchLabel. Set aria-label or aria-labelledby in TextAttrs
	// to take over.
	TextLabel string
	// SwatchAttrs and TextAttrs add attributes to the respective
	// inputs, for the data-* attributes a caller's own JS binds to.
	// Keys the component owns are dropped: the swatch's aria-label,
	// type, value and tabindex; the text input's name, value and the
	// field wiring; and every data-fui-* / data-hui-* key.
	SwatchAttrs html.Attrs
	TextAttrs   html.Attrs

	Class string

	// ExtraAttrs forwards additional attributes to the shell's root
	// element. Keys the component owns are dropped: class (use
	// Class), id, and every data-fui-* / data-hui-* key. Per-input
	// attributes belong in SwatchAttrs / TextAttrs.
	ExtraAttrs html.Attrs

	// Ctx carries the per-request context used to resolve the swatch's
	// word (ui.StringsFor: PickColor). When nil, the English default
	// is used.
	Ctx context.Context

	// Field is the wiring an enclosing FormField handed its builder:
	// applied to the text input (id, described-by chain, invalid
	// state), and it wins over TextID. Zero value means standalone.
	Field headless.FieldControl
}

// ColorField renders the swatch + hex-text affix shell.
func ColorField(cfg ColorFieldConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: ColorField requires Name — the hex field is what submits, and without a name it sends nothing")
	}
	if cfg.SwatchLabel == "" {
		panic("ui: ColorField requires SwatchLabel. A colour input with no accessible name is announced as nothing")
	}
	id := cfg.TextID
	if cfg.Field.ID != "" {
		id = cfg.Field.ID
	}

	// Name the text input unless the caller already did: applied on
	// the merged attrs so an explicit aria-label / aria-labelledby
	// wins, and a caller pointing at its own visible label is not
	// overridden.
	hexAttrs := html.Attrs{}
	maps.Copy(hexAttrs, cfg.TextAttrs)
	if hexAttrs["aria-label"] == "" && hexAttrs["aria-labelledby"] == "" {
		name := cfg.TextLabel
		if name == "" {
			name = cfg.SwatchLabel
		}
		hexAttrs["aria-label"] = name
	}

	swatchAttrs := html.Attrs{}
	maps.Copy(swatchAttrs, cfg.SwatchAttrs)

	rootAttrs := html.SafeExtraAttrs(cfg.ExtraAttrs)

	// A nil Ctx resolves through i18nui's English defaults (the
	// bridge pins them byte-for-byte to headless's), preserving the
	// swappable-defaults behaviour the framework's i18n table has.
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	return colorFieldStyle.WrapHTML(headless.Color(headless.ColorProps{
		Name:        cfg.Name,
		Value:       cfg.Value,
		Invalid:     cfg.Field.Invalid,
		ID:          id,
		DescribedBy: cfg.Field.DescribedBy,
		Parts: headless.Parts{
			Attrs: headless.PartAttrs{
				headless.PartRoot:        rootAttrs,
				headless.PartControl:     hexAttrs,
				headless.PartAffixSwatch: swatchAttrs,
			},
		},
		Strings: StringsFor(ctx),
	}, withRootClass(colorClasses, cfg.Class)))
}

var colorFieldStyle = registry.RegisterStyle("ui-color-field", colorFieldCSS)

func colorFieldCSS(_ style.Theme) string {
	return `.fui-color {
  display: flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
  inline-size: 100%;
}

.fui-color__swatch {
  flex: 0 0 auto;
  /* Square, and at least a comfortable tap target. */
  inline-size: var(--spacing-touch-target, 44px);
  block-size: var(--spacing-touch-target, 44px);
  padding: var(--spacing-xs, 2px);
  border: 1px solid var(--color-border, #e4e4e7);
  border-radius: var(--fui-field-radius);
  background-color: var(--color-surface, #fff);
  cursor: pointer;
}

.fui-color__text {
  /* The value is the point, so the text input takes the room. min-inline-size
     resets the intrinsic width an <input> otherwise insists on, which would
     push the row wider than its container. */
  flex: 1 1 auto;
  min-inline-size: 0;
  box-sizing: border-box;
  min-block-size: var(--fui-density-control-h);
  font-family: var(--font-mono, ui-monospace, monospace);
  padding: 10px var(--spacing-md, 8px);
  border: 1px solid var(--color-border, #e4e4e7);
  border-radius: var(--fui-field-radius);
  background: var(--color-surface, #fff);
  color: var(--color-text, #18181b);
  font-size: var(--text-base, 1rem);
}
.fui-color__text:focus-visible {
  outline: 2px solid var(--color-primary, #4f46e5);
  outline-offset: 1px;
}
.fui-color__text[aria-invalid="true"],
.fui-color[data-invalid] .fui-color__text {
  border-color: var(--color-danger, #dc2626);
  box-shadow: inset 0 0 0 1px var(--color-danger, #dc2626);
}
.fui-color__swatch:disabled,
.fui-color__text:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}`
}
