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

// ColorFieldConfig configures a ColorField.
type ColorFieldConfig struct {
	// Name is the hex text input's form name. Required.
	Name string
	// Value goes into the text input verbatim; one that is not #rgb or
	// #rrggbb leaves the swatch black and marks the shell data-invalid.
	Value string
	// TextID is the text input's id; a Field's id wins over it.
	TextID string
	// SwatchLabel is required, and names the text input when TextLabel
	// is empty; the swatch is named by PickColor plus Name.
	SwatchLabel string
	// TextLabel names the text input. An aria-label or aria-labelledby
	// in TextAttrs takes over.
	TextLabel string
	// SwatchAttrs and TextAttrs add attributes to each input; keys the
	// component owns are dropped.
	SwatchAttrs html.Attrs
	TextAttrs   html.Attrs

	Class string

	// ExtraAttrs forwards attributes to the shell's root; keys the
	// component owns are dropped.
	ExtraAttrs html.Attrs

	// Ctx resolves the swatch's word. Nil means English.
	Ctx context.Context

	// Field is an enclosing FormField's wiring, applied to the text
	// input. Zero means standalone.
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
  border: var(--stroke-thin, 1px) solid var(--ui-control-border, var(--color-text-subtle));
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
  padding: var(--ui-control-padding-y, 10px) var(--spacing-md, 8px);
  border: var(--stroke-thin, 1px) solid var(--ui-control-border, var(--color-text-subtle));
  border-radius: var(--fui-field-radius);
  background: var(--color-surface, #fff);
  color: var(--color-text, #09090B);
  font-size: var(--text-base, 1rem);
}
.fui-color__text:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: calc(var(--stroke-focus-offset, 2px) / 2);
}
.fui-color__text:is(:user-invalid, [aria-invalid="true"]),
.fui-color[data-invalid] .fui-color__text {
  border-color: var(--color-danger, #B91C1C);
  box-shadow: inset 0 0 0 var(--stroke-thin, 1px) var(--color-danger, #B91C1C);
}
.fui-color__swatch:disabled,
.fui-color__text:disabled {
  opacity: var(--opacity-muted, 0.6);
  cursor: not-allowed;
}`
}
