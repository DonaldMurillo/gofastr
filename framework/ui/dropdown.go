package ui

import (
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── Dropdown ───────────────────────────────────────────────────────
//
// A button that opens a panel floating under it: the Menu's sibling for
// content that is not a list of commands. A menu panel is role=menu
// and holds only rows; a dropdown holds a small form or a set of
// controls (a list's filters, a "save this view" name field). It is a
// native <details> (headless.Disclosure with Dismiss), so it opens with
// no script, and the disclosure module closes it on a click outside,
// on Escape and on a client-side navigation.
//
// Controls inside a closed dropdown still belong to their form: a
// dropdown inside a GET form holds fields the form submits whether the
// panel is open or not.

// DropdownAlign picks which edge of the trigger the panel lines up with.
type DropdownAlign string

const (
	// DropdownStart lines the panel up with the trigger's start edge
	// (the default).
	DropdownStart DropdownAlign = "start"
	// DropdownEnd lines it up with the trigger's end edge, for a
	// trigger at the end of a row.
	DropdownEnd DropdownAlign = "end"
)

// DropdownConfig configures a Dropdown.
type DropdownConfig struct {
	// Label is the trigger's text. Required.
	Label string
	// Icon, when set, names a registered ui.Icon drawn before the label.
	Icon string
	// Count, when above zero, draws a count badge after the label: the
	// filters applied, the rows chosen.
	Count int
	// Content is the panel. Required.
	Content render.HTML
	// Align picks the panel's edge; empty is DropdownStart.
	Align DropdownAlign
	// Open renders the panel open.
	Open bool

	ID    string
	Class string
	// ExtraAttrs land on the root <details>; open and class are the
	// component's (use Open and Class).
	ExtraAttrs html.Attrs
}

// Dropdown renders a trigger button and its floating panel.
func Dropdown(cfg DropdownConfig) render.HTML {
	if strings.TrimSpace(cfg.Label) == "" {
		panic("ui: Dropdown requires Label — the trigger is a button and needs a name")
	}
	if strings.TrimSpace(string(cfg.Content)) == "" {
		panic("ui: Dropdown requires Content — an empty panel opens onto nothing")
	}
	align := cfg.Align
	switch align {
	case "":
		align = DropdownStart
	case DropdownStart, DropdownEnd:
	default:
		panic("ui: Dropdown Align must be DropdownStart or DropdownEnd, got " + strconv.Quote(string(align)))
	}
	trigger := []render.HTML{}
	if cfg.Icon != "" {
		if !IconRegistered(cfg.Icon) {
			panic("ui: Dropdown Icon " + strconv.Quote(cfg.Icon) + " is not a registered icon")
		}
		trigger = append(trigger, Icon(cfg.Icon, IconConfig{Size: "16"}))
	}
	trigger = append(trigger, html.Span(html.TextConfig{Class: "fui-dropmenu__label"}, render.Text(cfg.Label)))
	if cfg.Count > 0 {
		trigger = append(trigger, html.Span(html.TextConfig{Class: "fui-dropmenu__count"},
			render.Text(strconv.Itoa(cfg.Count))))
	}
	out := headless.Disclosure(headless.DisclosureProps{
		// The trigger is built from strings and a registered icon, so
		// it is the component's own.
		Summary:    headless.Own(render.Join(trigger...)),
		Content:    cfg.Content,
		Open:       cfg.Open,
		Dismiss:    true,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "open", "class"),
	}, headless.Classes{
		headless.PartRoot:    cls("fui-dropmenu fui-dropmenu--"+string(align), cfg.Class),
		headless.PartSummary: "fui-dropmenu__trigger",
		headless.PartPanel:   "fui-dropmenu__panel",
	})
	return dropdownStyle.WrapHTML(out)
}

var dropdownStyle = registry.RegisterStyle("ui-dropdown", dropdownCSS)

// dropdownCSS draws the trigger as the menu trigger does (a bordered
// surface button) and the panel as the menu panel (surface, thin
// border, large radius, the lg shadow), so the two read as one family.
// Knobs: --ui-dropdown-min-width (14rem) and --ui-dropdown-max-width
// (28rem) bound the panel; the viewport always wins.
// --ui-dropdown-count-size (1.25rem) sizes the count badge.
func dropdownCSS(_ style.Theme) string {
	return `:where([data-cui-comp="ui-dropdown"]).fui-dropmenu {
  position: relative;
  display: inline-block;
}
[data-cui-comp="ui-dropdown"] > summary.fui-dropmenu__trigger {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
  cursor: pointer;
  list-style: none;
  user-select: none;
  white-space: nowrap;
  padding: 0 var(--spacing-lg, 16px);
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFF);
  color: var(--color-text, #18181B);
  box-shadow: var(--shadow-xs);
  font: inherit;
  font-size: var(--text-sm, 0.875rem);
  font-weight: var(--font-weight-medium);
  min-height: var(--spacing-touch-target, 44px);
  transition: background var(--duration-fast, 150ms) var(--easing-ease-in-out, ease);
}
[data-cui-comp="ui-dropdown"] > summary.fui-dropmenu__trigger::-webkit-details-marker { display: none; }
[data-cui-comp="ui-dropdown"] > summary.fui-dropmenu__trigger:hover,
[data-cui-comp="ui-dropdown"][open] > summary.fui-dropmenu__trigger {
  background: var(--color-surface-soft, #F4F4F5);
}
[data-cui-comp="ui-dropdown"] > summary.fui-dropmenu__trigger:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
[data-cui-comp="ui-dropdown"] .fui-dropmenu__trigger svg { flex: none; color: var(--color-text-muted); }
[data-cui-comp="ui-dropdown"] .fui-dropmenu__count {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-inline-size: var(--ui-dropdown-count-size, 1.25rem);
  padding: 0 var(--spacing-xs, 2px);
  border-radius: var(--radii-full, 9999px);
  background: var(--color-primary, #18181B);
  color: var(--color-primary-fg, #FFFFFF);
  font-size: var(--text-xs, 0.75rem);
  line-height: var(--ui-dropdown-count-size, 1.25rem);
  font-variant-numeric: tabular-nums;
}
[data-cui-comp="ui-dropdown"] > .fui-dropmenu__panel {
  /* headless-disclosure shifts a panel that would leave the viewport. */
  translate: var(--hui-panel-shift, 0);
  position: absolute;
  z-index: var(--z-dropdown, 100);
  top: calc(100% + var(--spacing-sm, 4px));
  box-sizing: border-box;
  min-inline-size: min(var(--ui-dropdown-min-width, 14rem), calc(100vw - var(--spacing-2xl, 32px)));
  max-inline-size: min(var(--ui-dropdown-max-width, 28rem), calc(100vw - var(--spacing-2xl, 32px)));
  padding: var(--spacing-md, 8px);
  background: var(--color-surface, #FFF);
  color: var(--color-text, #18181B);
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-lg, 10px);
  box-shadow: var(--shadow-lg, 0 10px 15px -3px rgba(0,0,0,.10));
  animation: fui-dropmenu-in var(--duration-dropdown-enter, 120ms) var(--easing-ease-out, cubic-bezier(0.16, 1, 0.3, 1));
}
[data-cui-comp="ui-dropdown"].fui-dropmenu--start > .fui-dropmenu__panel { inset-inline-start: 0; }
[data-cui-comp="ui-dropdown"].fui-dropmenu--end > .fui-dropmenu__panel { inset-inline-end: 0; }
@keyframes fui-dropmenu-in {
  from { opacity: 0; transform: translateY(-4px) scale(0.98); }
}
@media (prefers-reduced-motion: reduce) {
  [data-cui-comp="ui-dropdown"] > .fui-dropmenu__panel { animation: none; }
}
`
}
