package ui

import (
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
//     box — no custom JS for navigation.
//   - Form submissions submit the selected value as the radio group's
//     value, no client-side bookkeeping.
//   - Screen readers announce "radio group", current option, position
//     in set, all via native ARIA.
//
// The visual sliding indicator is CSS-only (uses :has() — supported
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

	// RPCPath, when set, attaches data-fui-rpc to each radio so a
	// change submits to the server. Method is POST.
	RPCPath string

	// RPCSignal, when set, broadcasts the response as the given
	// signal name (data-fui-rpc-signal).
	RPCSignal string

	ID    string
	Class string
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

	cls := "ui-segmented"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	wrapAttrs := html.Attrs{
		"class":      cls,
		"role":       "radiogroup",
		"data-count": itoaSmall(len(cfg.Options)),
	}
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
			"class": "ui-segmented__input",
			"id":    cfg.Name + "--" + slug(o.Value),
		}
		if o.Value == selected {
			inputAttrs["checked"] = ""
		}
		if o.Disabled {
			inputAttrs["disabled"] = ""
		}
		if cfg.RPCPath != "" {
			inputAttrs["data-fui-rpc"] = cfg.RPCPath
			inputAttrs["data-fui-rpc-method"] = "POST"
			if cfg.RPCSignal != "" {
				inputAttrs["data-fui-rpc-signal"] = cfg.RPCSignal
			}
		}
		input := render.Tag("input", flattenAttrs(inputAttrs))
		labelHTML := html.Span(html.TextConfig{Class: "ui-segmented__label"}, render.Text(o.Label))
		// Position index for sliding indicator CSS.
		labelAttrs := html.Attrs{
			"class":         "ui-segmented__option",
			"for":           cfg.Name + "--" + slug(o.Value),
			"data-position": itoaSmall(i),
		}
		items = append(items, render.Tag("label", flattenAttrs(labelAttrs), input, labelHTML))
	}
	// Indicator (CSS-positioned via :has() / data-position siblings).
	items = append(items, html.Span(html.TextConfig{
		Class: "ui-segmented__indicator",
		ExtraAttrs: html.Attrs{"aria-hidden": "true"},
	}))

	return segmentedStyle.WrapHTML(render.Tag("div", flattenAttrs(wrapAttrs), items...))
}

// itoaSmall converts a non-negative int to its decimal string —
// used for small bounded values (Options index, count). Avoids
// importing strconv for one int.
func itoaSmall(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// slug is local to the package; toggle.go already defines one.

var segmentedStyle = registry.RegisterStyle("ui-segmented", segmentedCSS)

func segmentedCSS(_ style.Theme) string {
	// Equal-width columns via CSS Grid (grid-auto-columns: 1fr). The
	// sliding indicator is sized to one column via the data-count
	// attribute on the wrapper, then translated by translateX(100% *
	// position) — math works because every column is the same width.
	return `[data-fui-comp="ui-segmented"] {
  position: relative;
  display: inline-grid;
  grid-auto-flow: column;
  grid-auto-columns: 1fr;
  padding: 4px;
  gap: 0;
  border-radius: var(--radii-md, 8px);
  background: var(--color-muted, #f1f1f3);
  border: 1px solid var(--color-border, #e5e7eb);
  font-size: 0.9rem;
  vertical-align: middle;
  isolation: isolate;
}
[data-fui-comp="ui-segmented"][data-count="2"] { min-inline-size: 16rem; }
[data-fui-comp="ui-segmented"][data-count="3"] { min-inline-size: 22rem; }
[data-fui-comp="ui-segmented"][data-count="4"] { min-inline-size: 26rem; }
[data-fui-comp="ui-segmented"][data-count="5"] { min-inline-size: 30rem; }
[data-fui-comp="ui-segmented"][data-count="6"] { min-inline-size: 34rem; }

[data-fui-comp="ui-segmented"] .ui-segmented__option {
  position: relative;
  z-index: 1;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-height: var(--spacing-touch-target, 40px);
  padding: 0 var(--spacing-md, 12px);
  border-radius: calc(var(--radii-md, 8px) - 4px);
  cursor: pointer;
  color: var(--color-text-muted, #6b7280);
  transition: color var(--duration-fast, 150ms) var(--easing-standard, ease);
  user-select: none;
  text-align: center;
  white-space: nowrap;
  margin: 0;
}
[data-fui-comp="ui-segmented"] .ui-segmented__option:hover {
  color: var(--color-text, #111);
}
[data-fui-comp="ui-segmented"] .ui-segmented__input {
  position: absolute;
  opacity: 0;
  pointer-events: none;
  inline-size: 0;
  block-size: 0;
  margin: 0;
}
[data-fui-comp="ui-segmented"] .ui-segmented__option:has(.ui-segmented__input:checked) {
  color: var(--color-text, #111);
  font-weight: 600;
}
[data-fui-comp="ui-segmented"] .ui-segmented__option:has(.ui-segmented__input:focus-visible) {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}
[data-fui-comp="ui-segmented"] .ui-segmented__option:has(.ui-segmented__input:disabled) {
  cursor: not-allowed;
  opacity: 0.45;
}

/* Sliding pill indicator. Sized to one column width via the data-count
   attribute on the wrapper; translated by (position × 100%) which is
   exact because every column is exactly 1fr wide. */
[data-fui-comp="ui-segmented"] .ui-segmented__indicator {
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
  transition: transform var(--duration-medium, 200ms) var(--easing-standard, cubic-bezier(0.4, 0, 0.2, 1));
  pointer-events: none;
}
[data-fui-comp="ui-segmented"][data-count="2"] .ui-segmented__indicator { inline-size: calc((100% - 8px) / 2); }
[data-fui-comp="ui-segmented"][data-count="3"] .ui-segmented__indicator { inline-size: calc((100% - 8px) / 3); }
[data-fui-comp="ui-segmented"][data-count="4"] .ui-segmented__indicator { inline-size: calc((100% - 8px) / 4); }
[data-fui-comp="ui-segmented"][data-count="5"] .ui-segmented__indicator { inline-size: calc((100% - 8px) / 5); }
[data-fui-comp="ui-segmented"][data-count="6"] .ui-segmented__indicator { inline-size: calc((100% - 8px) / 6); }

[data-fui-comp="ui-segmented"]:has(.ui-segmented__option[data-position="0"] .ui-segmented__input:checked) .ui-segmented__indicator { transform: translateX(0); }
[data-fui-comp="ui-segmented"]:has(.ui-segmented__option[data-position="1"] .ui-segmented__input:checked) .ui-segmented__indicator { transform: translateX(100%); }
[data-fui-comp="ui-segmented"]:has(.ui-segmented__option[data-position="2"] .ui-segmented__input:checked) .ui-segmented__indicator { transform: translateX(200%); }
[data-fui-comp="ui-segmented"]:has(.ui-segmented__option[data-position="3"] .ui-segmented__input:checked) .ui-segmented__indicator { transform: translateX(300%); }
[data-fui-comp="ui-segmented"]:has(.ui-segmented__option[data-position="4"] .ui-segmented__input:checked) .ui-segmented__indicator { transform: translateX(400%); }
[data-fui-comp="ui-segmented"]:has(.ui-segmented__option[data-position="5"] .ui-segmented__input:checked) .ui-segmented__indicator { transform: translateX(500%); }

@media (prefers-reduced-motion: reduce) {
  [data-fui-comp="ui-segmented"] .ui-segmented__indicator { transition: none; }
}
`
}
