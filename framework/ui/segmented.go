package ui

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// SegmentedOption is one selectable segment.
type SegmentedOption struct {
	// Label and Value are required; Value is unique within the control.
	Label    string
	Value    string
	Disabled bool
}

// SegmentedControlConfig configures a segmented radiogroup.
type SegmentedControlConfig struct {
	// Name is shared by every radio. Required.
	Name string
	// Options holds at least two segments.
	Options []SegmentedOption
	// Selected is the checked Value: empty checks the first segment,
	// and a value no option lists checks none.
	Selected string
	// Label names the radiogroup when nothing around it does.
	Label string
	// RPCPath POSTs the radio's form on change; outside a form the
	// body carries no selection.
	RPCPath string
	// RPCSignal broadcasts the RPC response under this signal name.
	RPCSignal string

	ID    string
	Class string
	// ExtraAttrs forwards attributes to the root; keys the component
	// owns are dropped.
	ExtraAttrs html.Attrs
}

// SegmentedControl renders native radios as a pill toggle bar whose
// indicator slides to the checked segment in CSS alone.
func SegmentedControl(cfg SegmentedControlConfig) render.HTML {
	if cfg.RPCPath != "" {
		checkRPCPath("SegmentedControl", "RPCPath", cfg.RPCPath)
	}
	if cfg.Name == "" {
		panic("ui: SegmentedControl requires Name")
	}
	if len(cfg.Options) < 2 {
		panic("ui: SegmentedControl requires at least 2 Options")
	}
	for _, o := range cfg.Options {
		if o.Label == "" || o.Value == "" {
			panic("ui: SegmentedControl option requires Label and Value")
		}
	}
	selected := cfg.Selected
	if selected == "" {
		selected = cfg.Options[0].Value
	}

	cls := "fui-segmented"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	wrapAttrs := html.SafeExtraAttrs(cfg.ExtraAttrs, "role", "data-count", "aria-label")
	if wrapAttrs == nil {
		wrapAttrs = html.Attrs{}
	}
	wrapAttrs["class"] = cls
	wrapAttrs["role"] = "radiogroup"
	wrapAttrs["data-count"] = strconv.Itoa(len(cfg.Options))
	if cfg.ID != "" {
		wrapAttrs["id"] = cfg.ID
	}
	if cfg.Label != "" {
		wrapAttrs["aria-label"] = cfg.Label
	}

	items := make([]render.HTML, 0, len(cfg.Options)+1)
	for i, o := range cfg.Options {
		inputAttrs := html.Attrs{
			"type":  "radio",
			"name":  cfg.Name,
			"value": o.Value,
			"class": "fui-segmented__input",
			"id":    cfg.Name + "--" + strconv.Itoa(i),
		}
		if o.Value == selected {
			inputAttrs["checked"] = ""
		}
		if o.Disabled {
			inputAttrs["disabled"] = ""
		}
		if cfg.RPCPath != "" {
			inputAttrs["data-cui-rpc"] = cfg.RPCPath
			inputAttrs["data-cui-rpc-method"] = "POST"
			if cfg.RPCSignal != "" {
				inputAttrs["data-cui-rpc-signal"] = cfg.RPCSignal
			}
		}
		input := render.Tag("input", flattenAttrs(inputAttrs))
		labelHTML := html.Span(html.TextConfig{Class: "fui-segmented__label", ExtraAttrs: html.Attrs{"data-label": o.Label}}, render.Text(o.Label))
		labelAttrs := html.Attrs{
			"class":             "fui-segmented__option",
			"for":               cfg.Name + "--" + strconv.Itoa(i),
			"data-cui-internal": "",
		}
		items = append(items, render.Tag("label", flattenAttrs(labelAttrs), input, labelHTML))
	}
	items = append(items, html.Span(html.TextConfig{
		Class:      "fui-segmented__indicator",
		ExtraAttrs: html.Attrs{"aria-hidden": "true", "data-cui-internal": ""},
	}))

	return segmentedStyle.WrapHTML(render.Tag("div", flattenAttrs(wrapAttrs), items...))
}

var segmentedStyle = registry.RegisterStyle("ui-segmented", segmentedCSS)

func segmentedCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-segmented"] {
  position: relative;
  anchor-scope: --fui-segmented-checked;
  display: inline-grid;
  grid-auto-flow: column;
  grid-auto-columns: 1fr;
  padding: var(--spacing-sm, 4px);
  gap: 0;
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface-soft, #F4F4F5);
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  font-size: var(--text-sm, 0.875rem);
  vertical-align: middle;
  isolation: isolate;
  /* Inline-grid alone does not hold: a stack or section body stretches
     its items, and a billing toggle spanned the column. A definite
     width is never stretched; each count's minimum gives way to a
     narrower column, since a minimum beats the 100% cap. */
  inline-size: fit-content;
  max-inline-size: 100%;
}
:where([data-cui-comp="ui-segmented"])[data-count="2"] { min-inline-size: min(var(--ui-segmented-min-width-2, 16rem), 100%); }
:where([data-cui-comp="ui-segmented"])[data-count="3"] { min-inline-size: min(var(--ui-segmented-min-width-3, 22rem), 100%); }
:where([data-cui-comp="ui-segmented"])[data-count="4"] { min-inline-size: min(var(--ui-segmented-min-width-4, 26rem), 100%); }
:where([data-cui-comp="ui-segmented"])[data-count="5"] { min-inline-size: min(var(--ui-segmented-min-width-5, 30rem), 100%); }
:where([data-cui-comp="ui-segmented"])[data-count="6"] { min-inline-size: min(var(--ui-segmented-min-width-6, 34rem), 100%); }

[data-cui-comp="ui-segmented"] .fui-segmented__option {
  position: relative;
  z-index: 1;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-height: var(--spacing-touch-target, 44px);
  padding: 0 var(--spacing-md, 8px);
  border-radius: calc(var(--radii-md, 8px) - 4px);
  cursor: pointer;
  color: var(--color-text-muted, #52525B);
  transition: color var(--duration-fast, 150ms) var(--easing-ease-out, ease);
  user-select: none;
  text-align: center;
  white-space: nowrap;
  margin: 0;
}
[data-cui-comp="ui-segmented"] .fui-segmented__option:hover {
  color: var(--color-text, #09090B);
}
[data-cui-comp="ui-segmented"] .fui-segmented__input {
  position: absolute;
  opacity: 0;
  pointer-events: none;
  inline-size: 0;
  block-size: 0;
  margin: 0;
}
[data-cui-comp="ui-segmented"] .fui-segmented__option:has(.fui-segmented__input:checked) {
  color: var(--color-text, #09090B);
  font-weight: var(--font-weight-semibold);
}
/* The checked option sets its label in the semibold weight, and every
   column is as wide as the widest label, so without a reservation the
   control grows and shrinks as the selection moves (a heavy theme
   weight makes it plain). A hidden, zero-height semibold copy of the
   label shares the label's one grid column and holds it at the checked
   width in every state. visibility: hidden keeps the copy out of the
   accessibility tree. */
[data-cui-comp="ui-segmented"] .fui-segmented__label {
  display: inline-grid;
  justify-items: center;
}
[data-cui-comp="ui-segmented"] .fui-segmented__label::after {
  content: attr(data-label);
  font-weight: var(--font-weight-semibold);
  visibility: hidden;
  block-size: 0;
  overflow: hidden;
  user-select: none;
  pointer-events: none;
}
[data-cui-comp="ui-segmented"] .fui-segmented__option:has(.fui-segmented__input:focus-visible) {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
[data-cui-comp="ui-segmented"] .fui-segmented__option:has(.fui-segmented__input:disabled) {
  cursor: not-allowed;
  opacity: var(--opacity-disabled, 0.5);
}

/* Sliding pill indicator. The checked option is the anchor and the
   control scopes the anchor name, so each control's pill finds its own
   checked segment and takes its box: any option count, either
   direction, no per-count or per-position rule. When the checked
   option changes, the pill's insets change and the transition slides
   it there. */
[data-cui-comp="ui-segmented"] .fui-segmented__option:has(.fui-segmented__input:checked) {
  anchor-name: --fui-segmented-checked;
}
[data-cui-comp="ui-segmented"] .fui-segmented__indicator {
  position: absolute;
  z-index: 0;
  position-anchor: --fui-segmented-checked;
  top: anchor(top);
  bottom: anchor(bottom);
  inset-inline-start: anchor(start);
  inset-inline-end: anchor(end);
  border-radius: calc(var(--radii-md, 8px) - 4px);
  background: var(--color-surface, #fff);
  box-shadow: var(--ui-segmented-indicator-shadow, 0 1px 2px rgba(0,0,0,0.08),
              0 0 0 var(--stroke-thin, 1px) rgba(0,0,0,0.05));
  transition: inset-inline-start var(--duration-normal, 250ms) var(--easing-ease-in-out, cubic-bezier(0.4, 0, 0.2, 1)),
              inset-inline-end var(--duration-normal, 250ms) var(--easing-ease-in-out, cubic-bezier(0.4, 0, 0.2, 1));
  pointer-events: none;
}
/* With no segment checked there is no anchor, and a pill would claim a
   choice the group does not hold. */
[data-cui-comp="ui-segmented"]:not(:has(.fui-segmented__input:checked)) .fui-segmented__indicator { visibility: hidden; }
/* Without anchor positioning the checked option draws its own pill:
   in the right place, without the slide. */
@supports not (anchor-name: --x) {
  [data-cui-comp="ui-segmented"] .fui-segmented__indicator { display: none; }
  [data-cui-comp="ui-segmented"] .fui-segmented__option:has(.fui-segmented__input:checked) {
    background: var(--color-surface, #fff);
    box-shadow: var(--ui-segmented-indicator-shadow, 0 1px 2px rgba(0,0,0,0.08),
                0 0 0 var(--stroke-thin, 1px) rgba(0,0,0,0.05));
  }
}

@media (prefers-reduced-motion: reduce) {
  [data-cui-comp="ui-segmented"] .fui-segmented__indicator { transition: none; }
}
`
}
