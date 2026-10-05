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
// The visual sliding indicator is CSS-only (uses :has(), supported
// in all evergreen browsers as of 2024). On older engines the
// indicator stays static; the control remains fully functional.
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

	// Selected is the initially selected Value. When empty or not
	// matching any option, defaults to Options[0].Value.
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
	found := false
	for _, o := range cfg.Options {
		if o.Value == selected {
			found = true
			break
		}
	}
	if !found {
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
			"id":    cfg.Name + "--" + slug(o.Value),
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
		labelHTML := html.Span(html.TextConfig{Class: "fui-segmented__label"}, render.Text(o.Label))
		// Position index for sliding indicator CSS. Every option comes
		// from SegmentedControlConfig's Options (label/value strings),
		// never from caller markup, so each label is a topmost internal
		// subtree; its input/labelHTML children need no mark of their own.
		labelAttrs := html.Attrs{
			"class":             "fui-segmented__option",
			"for":               cfg.Name + "--" + slug(o.Value),
			"data-position":     strconv.Itoa(i),
			"data-cui-internal": "",
		}
		items = append(items, render.Tag("label", flattenAttrs(labelAttrs), input, labelHTML))
	}
	// Indicator (CSS-positioned via :has() / data-position siblings) —
	// also this component's own, not caller content.
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
	// sliding indicator is sized to one column via the data-count
	// attribute on the wrapper, then translated by translateX(100% *
	// position). Math works because every column is the same width.
	return `[data-cui-comp="ui-segmented"] {
  position: relative;
  display: inline-grid;
  grid-auto-flow: column;
  grid-auto-columns: 1fr;
  padding: var(--spacing-sm, 4px);
  gap: 0;
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface-soft, #f1f1f3);
  border: 1px solid var(--color-border, #e5e7eb);
  font-size: var(--text-sm, 0.875rem);
  vertical-align: middle;
  isolation: isolate;
}
:where([data-cui-comp="ui-segmented"])[data-count="2"] { min-inline-size: 16rem; }
:where([data-cui-comp="ui-segmented"])[data-count="3"] { min-inline-size: 22rem; }
:where([data-cui-comp="ui-segmented"])[data-count="4"] { min-inline-size: 26rem; }
:where([data-cui-comp="ui-segmented"])[data-count="5"] { min-inline-size: 30rem; }
:where([data-cui-comp="ui-segmented"])[data-count="6"] { min-inline-size: 34rem; }

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
  color: var(--color-text-muted, #6b7280);
  transition: color var(--duration-fast, 150ms) var(--easing-ease-in-out, ease);
  user-select: none;
  text-align: center;
  white-space: nowrap;
  margin: 0;
}
[data-cui-comp="ui-segmented"] .fui-segmented__option:hover {
  color: var(--color-text, #111);
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
  color: var(--color-text, #111);
  font-weight: var(--font-weight-semibold);
}
[data-cui-comp="ui-segmented"] .fui-segmented__option:has(.fui-segmented__input:focus-visible) {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}
[data-cui-comp="ui-segmented"] .fui-segmented__option:has(.fui-segmented__input:disabled) {
  cursor: not-allowed;
  opacity: 0.45;
}

/* Sliding pill indicator. Sized to one column width via the data-count
   attribute on the wrapper; translated by (position × 100%) which is
   exact because every column is exactly 1fr wide. */
[data-cui-comp="ui-segmented"] .fui-segmented__indicator {
  position: absolute;
  z-index: 0;
  top: 4px;
  bottom: 4px;
  left: 4px;
  inline-size: calc((100% - 8px) / 2);
  border-radius: calc(var(--radii-md, 8px) - 4px);
  background: var(--color-surface, #fff);
  box-shadow: 0 1px 2px rgba(0,0,0,0.08),
              0 0 0 1px rgba(0,0,0,0.05);
  transition: transform var(--duration-normal, 250ms) var(--easing-ease-in-out, cubic-bezier(0.4, 0, 0.2, 1));
  pointer-events: none;
}
[data-cui-comp="ui-segmented"][data-count="2"] .fui-segmented__indicator { inline-size: calc((100% - 8px) / 2); }
[data-cui-comp="ui-segmented"][data-count="3"] .fui-segmented__indicator { inline-size: calc((100% - 8px) / 3); }
[data-cui-comp="ui-segmented"][data-count="4"] .fui-segmented__indicator { inline-size: calc((100% - 8px) / 4); }
[data-cui-comp="ui-segmented"][data-count="5"] .fui-segmented__indicator { inline-size: calc((100% - 8px) / 5); }
[data-cui-comp="ui-segmented"][data-count="6"] .fui-segmented__indicator { inline-size: calc((100% - 8px) / 6); }

[data-cui-comp="ui-segmented"]:has(.fui-segmented__option[data-position="0"] .fui-segmented__input:checked) .fui-segmented__indicator { transform: translateX(0); }
[data-cui-comp="ui-segmented"]:has(.fui-segmented__option[data-position="1"] .fui-segmented__input:checked) .fui-segmented__indicator { transform: translateX(100%); }
[data-cui-comp="ui-segmented"]:has(.fui-segmented__option[data-position="2"] .fui-segmented__input:checked) .fui-segmented__indicator { transform: translateX(200%); }
[data-cui-comp="ui-segmented"]:has(.fui-segmented__option[data-position="3"] .fui-segmented__input:checked) .fui-segmented__indicator { transform: translateX(300%); }
[data-cui-comp="ui-segmented"]:has(.fui-segmented__option[data-position="4"] .fui-segmented__input:checked) .fui-segmented__indicator { transform: translateX(400%); }
[data-cui-comp="ui-segmented"]:has(.fui-segmented__option[data-position="5"] .fui-segmented__input:checked) .fui-segmented__indicator { transform: translateX(500%); }

@media (prefers-reduced-motion: reduce) {
  [data-cui-comp="ui-segmented"] .fui-segmented__indicator { transition: none; }
}
`
}
