package ui

import (
	"context"
	"fmt"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

var signalToggleStyle = registry.RegisterStyle("fui-toggle", signalToggleCSS)

func signalToggleCSS(_ style.Theme) string {
	return `[data-cui-comp="fui-toggle"]{display:inline-flex;align-items:center;gap:var(--spacing-md, .5rem);background:none;border:none;cursor:pointer;padding:0;color:var(--fui-foreground, var(--color-text, #09090B));font:inherit}` +
		`[data-cui-comp="fui-toggle"] .fui-toggle__track{position:relative;display:inline-block;width:var(--ui-toggle-track-width, 2.5rem);height:calc(var(--ui-toggle-thumb-size, 1.125rem) + var(--ui-toggle-thumb-inset, 2px) * 2);border-radius:var(--radii-full, 9999px);background:var(--fui-border, var(--color-border, #E4E4E7));transition:background var(--duration-fast, 150ms)}` +
		`[data-cui-comp="fui-toggle"] .fui-toggle__thumb{position:absolute;top:var(--ui-toggle-thumb-inset, 2px);inset-inline-start:var(--ui-toggle-thumb-inset, 2px);width:var(--ui-toggle-thumb-size, 1.125rem);height:var(--ui-toggle-thumb-size, 1.125rem);border-radius:50%;background:var(--ui-toggle-thumb-bg, #fff);box-shadow:var(--ui-toggle-thumb-shadow, 0 1px 2px rgba(0,0,0,.2));transition:transform var(--duration-fast, 150ms)}` +
		`[data-cui-comp="fui-toggle"][aria-checked="true"] .fui-toggle__track{background:var(--fui-primary, var(--color-primary, #18181B))}` +
		`[data-cui-comp="fui-toggle"][aria-checked="true"] .fui-toggle__thumb{transform:translateX(calc(var(--fui-toggle-dir, 1) * (var(--ui-toggle-track-width, 2.5rem) - var(--ui-toggle-thumb-size, 1.125rem) - var(--ui-toggle-thumb-inset, 2px) * 2)));background:var(--color-primary-fg, #fff)}` +
		`[data-cui-comp="fui-toggle"]:dir(rtl){--fui-toggle-dir:-1}` +
		`[data-cui-comp="fui-toggle"]:focus-visible{outline:var(--stroke-focus, 2px) solid var(--color-text-subtle);outline-offset:var(--stroke-focus-offset, 2px);border-radius:var(--radii-sm, 6px)}`
}

// SignalToggleConfig configures a switch that flips a client signal.
type SignalToggleConfig struct {
	// SignalName is the signal flipped. Required unless Slice is set.
	SignalName string
	// Slice supplies the signal name and first-paint value; it wins over SignalName.
	Slice *store.Slice[bool]
	// Label is the aria-label. Default: the signal name.
	Label string
	// Class adds classes.
	Class string
	// Ctx is the render context a Slice's first paint is read from.
	Ctx context.Context
	// ExtraAttrs land on the button; class, data-cui-*, type, role, aria-checked and aria-label are dropped.
	ExtraAttrs html.Attrs
}

// SignalToggle renders a <button role="switch"> bound to a boolean signal.
func SignalToggle(cfg SignalToggleConfig) render.HTML {
	name := cfg.SignalName
	initial := false
	if cfg.Slice != nil {
		name = cfg.Slice.Name()
		initial = cfg.Slice.Value(cfg.Ctx)
	}
	if name == "" {
		panic("ui: SignalToggle requires SignalName or Slice")
	}
	label := cfg.Label
	if label == "" {
		label = name
	}
	cls := "fui-toggle"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	initStr := strconv.FormatBool(initial)

	escName := render.Escape(name)
	escLabel := render.Escape(label)
	escCls := render.Escape(cls)

	extraAttrs := serializeExtraAttrs(html.SafeExtraAttrs(cfg.ExtraAttrs,
		"type", "role", "aria-checked", "aria-label"))

	inner := fmt.Sprintf(
		`<span class="fui-toggle__track" data-cui-internal=""><span class="fui-toggle__thumb"></span></span>`+
			`<span class="fui-toggle__label" data-cui-internal="" data-cui-signal="%s">%s</span>`,
		escName, initStr,
	)

	//gofastr:allow(GOFASTR1403) every operand is render.Escape output, render.Attr output or the fixed inner markup
	return render.HTML(fmt.Sprintf(
		`<button type="button" class="%s" data-cui-comp="fui-toggle"`+
			` data-cui-signal-toggle="%s"`+
			` data-cui-signal="%s" data-cui-signal-mode="attr" data-cui-signal-attr="aria-checked"`+
			` role="switch" aria-checked="%s" aria-label="%s"%s>%s</button>`,
		escCls, escName, escName, initStr, escLabel, extraAttrs, inner,
	))
}
