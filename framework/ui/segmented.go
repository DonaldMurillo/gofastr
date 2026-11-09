package ui

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── SegmentedControl ───────────────────────────────────────────────
//
// A radiogroup styled as a horizontal pill toggle bar with a sliding
// background indicator. Built on native <input type="radio"> so:
//
//   - Keyboard nav (Tab, Arrow keys, Space/Enter) works out of the
//     box. No custom JS for navigation.
//   - Form submissions submit the selected value as the radio group's
//     value, no client-side bookkeeping.
//   - Screen readers announce "radio group", current option, position
//     in set, all via native ARIA.
//
// The sliding indicator is CSS-only: the checked option is a CSS
// anchor and the indicator is positioned on it, so it fits any number
// of options. A browser without anchor positioning draws the pill on
// the checked option itself, without the slide.
//
// Optional RPCPath fires a POST to the server on change so apps can
// pre-cache or re-render dependent islands; pair with RPCSignal for
// signal-driven downstream updates.

// SegmentedOption is one selectable segment.
type SegmentedOption struct {
	// Label is the visible text. Required.
	Label string
	// Value is the submit value and the option's stable identifier.
	// Required and unique within the control.
	Value string
	// Disabled marks the segment as non-selectable.
	Disabled bool
}

// SegmentedControlConfig configures a segmented radiogroup.
type SegmentedControlConfig struct {
	// Name is the form-submit name shared by all radios. Required.
	Name string

	// Options must contain at least two segments. Required.
	Options []SegmentedOption

	// Selected is the initially selected Value. Empty defaults to
	// Options[0].Value. A value matching no option checks no segment,
	// so the group submits nothing rather than a value nobody chose.
	Selected string

	// Label is the aria-label on the radiogroup wrapper. Required
	// when the surrounding context doesn't already label it (e.g.
	// the SegmentedControl is not inside a <label> or FormField).
	Label string

	// RPCPath, when set, attaches data-cui-rpc to each radio so a change
	// POSTs to the server carrying the selected segment's name=value. The
	// runtime serializes the form the radio belongs to (node.form), so place
	// the SegmentedControl inside a <form> for the selection to round-trip:
	// a radio with no enclosing form posts an empty body and the handler
	// cannot see which segment was chosen. An explicit data-cui-rpc-body on a
	// radio still wins over form serialization. Method is POST.
	RPCPath string

	// RPCSignal, when set, broadcasts the response as the given
	// signal name (data-cui-rpc-signal).
	RPCSignal string

	ID    string
	Class string
	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the control's root
	// element. Keys the component owns are dropped: class and id
	// (use Class / ID), data-cui-*, role, aria-label, and data-count.
	ExtraAttrs html.Attrs
}

// SegmentedControl renders the radiogroup with a sliding indicator.
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
	// An empty Selected starts on the first segment. A value no segment
	// lists checks none: falling back to the first submitted a value
	// nobody chose, and no segment can draw the one that was given.
	selected := cfg.Selected
	if selected == "" {
		selected = cfg.Options[0].Value
	}

	cls := "fui-segmented"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	// Sanitized extras first, owned keys on top so they win.
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
		// data-label feeds the hidden bold copy that reserves the
		// checked weight's width (see segmentedCSS).
		labelHTML := html.Span(html.TextConfig{Class: "fui-segmented__label", ExtraAttrs: html.Attrs{"data-label": o.Label}}, render.Text(o.Label))
		// Every option comes from SegmentedControlConfig's Options
		// (label/value strings), never from caller markup, so each label
		// is a topmost internal subtree; its input/labelHTML children
		// need no mark of their own.
		labelAttrs := html.Attrs{
			"class":             "fui-segmented__option",
			"for":               cfg.Name + "--" + strconv.Itoa(i),
			"data-cui-internal": "",
		}
		items = append(items, render.Tag("label", flattenAttrs(labelAttrs), input, labelHTML))
	}
	// Indicator (anchored to the checked option in CSS), also this
	// component's own, not caller content.
	items = append(items, html.Span(html.TextConfig{
		Class:      "fui-segmented__indicator",
		ExtraAttrs: html.Attrs{"aria-hidden": "true", "data-cui-internal": ""},
	}))

	return segmentedStyle.WrapHTML(render.Tag("div", flattenAttrs(wrapAttrs), items...))
}

// slug is local to the package; toggle.go already defines one.

var segmentedStyle = registry.RegisterStyle("ui-segmented", segmentedCSS)

func segmentedCSS(_ style.Theme) string {
	// Equal-width columns via CSS Grid (grid-auto-columns: 1fr). The
	// sliding indicator is anchored to the checked option (CSS anchor
	// positioning, scoped to the control), so it fits any option count.
	//
	// Knobs: --ui-segmented-min-width-2…-6 (16/22/26/30/34rem, the
	// minimum widths for two to six options; seven or more size to
	// their content), --ui-segmented-indicator-shadow (the sliding
	// pill's two-layer shadow).
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
