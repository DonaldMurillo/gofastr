package interactive

// The phone half of a navigation surface, shared by SectionMenu and any
// component that folds into a bottom sheet on a phone (ui.TableOfContents
// with PhoneSheet):
//
//   SheetPill:  the pill floating at the bottom of the viewport. It names
//     where the reader is and opens the sheet.
//   PhoneSheet: a pill plus a sheet the page renders inline. Below the
//     swap width the sheet is a native popover rising from the bottom;
//     at and above it the sheet's body sits in normal flow and the pill
//     and the sheet's chrome are gone. Content that changes per page (a
//     contents list) goes here. A menu that is the same on every page
//     can use a mounted widget sheet instead, as SectionMenuDrawer does.
//   sheetHead:  the sheet's title row and close button, drawn the same
//     in both sheets.
//
// The phonesheet behaviour (phone_sheet.js) keeps a PhoneSheet's pill on
// the entry its body marks current, closes the sheet when a link in it
// is followed, and closes it when the viewport grows past the swap.

import (
	_ "embed"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

//go:embed phone_sheet.js
var phoneSheetJS string

var _ = registry.RegisterBehavior("phonesheet", phoneSheetJS,
	registry.Markers(`[data-cui-comp="cui-phone-sheet"]`))

// SwapWidth picks the viewport width where a desktop surface (a
// SectionMenu's rail, a PhoneSheet's inline body) gives way to the
// phone pill and its sheet.
type SwapWidth string

const (
	// SwapDefault swaps below 900px: a surface in normal flow, beside a
	// column the page lays out itself.
	SwapDefault SwapWidth = ""
	// SwapMD swaps below 48rem, where a ui.ContentRow with the default
	// Breakpoint stacks its columns.
	SwapMD SwapWidth = "md"
	// SwapLG swaps below 64rem, where a ui.ContentRow with
	// Breakpoint: ui.StackBelowLG stacks.
	SwapLG SwapWidth = "lg"
)

// swapWidths are the widths either side of each swap, in the order the
// generated stylesheets list them.
var swapWidths = []struct {
	w          SwapWidth
	maxw, minw string
}{
	{SwapDefault, "899.98px", "900px"},
	{SwapMD, "47.99rem", "48rem"},
	{SwapLG, "63.99rem", "64rem"},
}

// swapClass is the modifier a component adds for a swap width, or ""
// for the default. An unknown width panics, naming the component.
func swapClass(component, block string, w SwapWidth) string {
	switch w {
	case SwapDefault:
		return ""
	case SwapMD, SwapLG:
		return " " + block + "--swap-" + string(w)
	}
	panic("interactive: " + component + " unknown SwapAt " + string(w) +
		`. Pick one of: "" (900px), md (48rem), lg (64rem)`)
}

// swapScope is the :where() scope selecting a component root at one swap
// width: the default matches a root with neither modifier.
func swapScope(root, block string, w SwapWidth) string {
	if w == SwapDefault {
		return root + ":where(:not(." + block + "--swap-md, ." + block + "--swap-lg))"
	}
	return root + ":where(." + block + "--swap-" + string(w) + ")"
}

// ─── SheetPill ──────────────────────────────────────────────────────

// SheetPillConfig configures a SheetPill.
type SheetPillConfig struct {
	// Name is what the pill opens ("Components", "On this page"). It
	// starts the pill's accessible name, so a screen reader hears what
	// the control opens before where the reader is, and it is the
	// visible label while nothing is current. Required.
	Name string
	// Label names the current entry. Empty shows Name.
	Label string
	// Detail is a muted second part after the label, such as the
	// current entry's group. Optional.
	Detail string
	// Opens names the mounted widget the pill opens (data-cui-open).
	Opens string
	// Popover is the id of the inline popover the pill opens
	// (popovertarget). Set exactly one of Opens and Popover.
	Popover string
	// Inline keeps the pill in flow where it sits. By default it floats
	// at the bottom of the viewport, centred, and the page ends one
	// pill's height lower so the pill never covers its last line.
	Inline bool
	// SwapAt is the width at and above which the pill hides: the
	// desktop surface it stands in for takes over there.
	SwapAt SwapWidth
}

// SheetPill renders the phone pill: an icon, the current entry and its
// detail, and a chevron. A behaviour that tracks the current entry
// rewrites the label and detail spans in place.
func SheetPill(cfg SheetPillConfig) render.HTML {
	if cfg.Name == "" {
		panic("interactive: SheetPill requires Name: it starts the pill's accessible name")
	}
	if (cfg.Opens == "") == (cfg.Popover == "") {
		panic("interactive: SheetPill needs exactly one of Opens (a mounted widget) and Popover (an inline popover's id)")
	}
	cls := "cui-sheet-pill" + swapClass("SheetPill", "cui-sheet-pill", cfg.SwapAt)
	if cfg.Inline {
		cls += " cui-sheet-pill--inline"
	}
	attrs := map[string]string{"class": cls, "type": "button", "aria-haspopup": "dialog"}
	if cfg.Opens != "" {
		attrs["data-cui-open"] = cfg.Opens
	} else {
		attrs["popovertarget"] = cfg.Popover
	}
	label := cfg.Label
	if label == "" {
		label = cfg.Name
	}
	return sheetPillStyle.WrapHTML(render.Tag("button", attrs,
		render.Raw(`<svg class="cui-sheet-pill__icon" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><line x1="9" y1="6" x2="20" y2="6"/><line x1="9" y1="12" x2="20" y2="12"/><line x1="9" y1="18" x2="20" y2="18"/><circle cx="4.5" cy="6" r="1"/><circle cx="4.5" cy="12" r="1"/><circle cx="4.5" cy="18" r="1"/></svg>`),
		html.Span(html.TextConfig{Class: "cui-sheet-pill__sr"}, render.Text(cfg.Name+": ")),
		html.Span(html.TextConfig{Class: "cui-sheet-pill__label"}, render.Text(label)),
		html.Span(html.TextConfig{Class: "cui-sheet-pill__detail"}, render.Text(cfg.Detail)),
		render.Raw(`<svg class="cui-sheet-pill__chevron" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="6 15 12 9 18 15"/></svg>`),
	))
}

var sheetPillStyle = registry.RegisterStyle("cui-sheet-pill", sheetPillCSS)

// Knobs:
//   - --ui-sheet-pill-bottom (16px; set it on :root, where the page's
//     end padding reads it too): the floating pill's gap above the
//     viewport's bottom edge (the safe-area inset is added to it).
func sheetPillCSS(_ style.Theme) string {
	var b strings.Builder
	b.WriteString(`[data-cui-comp="cui-sheet-pill"] {
  position: fixed;
  z-index: var(--z-sticky, 200);
  inset-inline-start: 50%;
  inset-block-end: calc(var(--ui-sheet-pill-bottom, 16px) + env(safe-area-inset-bottom, 0px));
  translate: -50% 0;
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-md, 8px);
  max-inline-size: calc(100vw - 2 * var(--spacing-lg, 16px));
  box-sizing: border-box;
  cursor: pointer;
  padding: calc(var(--spacing-sm, 4px) * 2.25) calc(var(--spacing-sm, 4px) * 3.5) calc(var(--spacing-sm, 4px) * 2.25) calc(var(--spacing-sm, 4px) * 3);
  border: var(--stroke-thin, 1px) solid var(--color-border-strong, #D4D4D8);
  border-radius: var(--radii-full, 9999px);
  background: var(--color-surface, #fff);
  box-shadow: var(--shadow-lg, 0 10px 30px rgba(0,0,0,0.15));
  color: var(--color-text, currentColor);
  font: inherit;
  font-size: var(--text-sm, 0.875rem);
  white-space: nowrap;
}
[data-cui-comp="cui-sheet-pill"]:not(.cui-sheet-pill--inline):dir(rtl) { translate: 50% 0; }
:where([data-cui-comp="cui-sheet-pill"]).cui-sheet-pill--inline {
  position: static;
  translate: none;
  box-shadow: none;
  max-inline-size: 100%;
}
[data-cui-comp="cui-sheet-pill"] .cui-sheet-pill__icon,
[data-cui-comp="cui-sheet-pill"] .cui-sheet-pill__chevron { flex: 0 0 auto; color: var(--color-text-muted, #52525B); }
[data-cui-comp="cui-sheet-pill"] .cui-sheet-pill__label {
  font-weight: var(--font-weight-semibold, 600);
  overflow: hidden;
  text-overflow: ellipsis;
}
[data-cui-comp="cui-sheet-pill"] .cui-sheet-pill__detail {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-md, 8px);
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--color-text-muted, #52525B);
}
/* A painted dot, not a text bullet: the separator stays out of the
   button's accessible name. */
[data-cui-comp="cui-sheet-pill"] .cui-sheet-pill__detail::before {
  content: "";
  flex: 0 0 auto;
  inline-size: calc(var(--stroke-thin, 1px) * 3);
  block-size: calc(var(--stroke-thin, 1px) * 3);
  border-radius: var(--radii-full, 9999px);
  background: currentColor;
}
[data-cui-comp="cui-sheet-pill"] .cui-sheet-pill__detail:empty { display: none; }
[data-cui-comp="cui-sheet-pill"] .cui-sheet-pill__sr {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  margin: -1px;
  padding: 0;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
  border: 0;
}
`)
	for _, s := range swapWidths {
		scope := swapScope(`[data-cui-comp="cui-sheet-pill"]`, "cui-sheet-pill", s.w)
		b.WriteString(strings.NewReplacer("SCOPE", scope, "MAXW", s.maxw, "MINW", s.minw).Replace(`@media (min-width: MINW) {
  SCOPE { display: none; }
}
/* The floating pill would cover the end of the page (a footer's last
   line): the page ends one pill's height plus its gap lower. */
@media (max-width: MAXW) {
  body:has(SCOPE:not(.cui-sheet-pill--inline)) {
    padding-block-end: calc(var(--ui-sheet-pill-bottom, 16px) + env(safe-area-inset-bottom, 0px) + 3.5rem);
  }
}
`))
	}
	return b.String()
}

// ─── Sheet head ─────────────────────────────────────────────────────

// sheetHead is a sheet's title row: the title, which labels the sheet's
// dialog through id, and a close button whose attrs say what it closes
// (a widget's data-cui-action, a popover's popovertarget). The dialog's
// name says what "Close" closes.
func sheetHead(title, id string, closeAttrs map[string]string) render.HTML {
	attrs := map[string]string{"class": "cui-sheet-head__close", "type": "button", "aria-label": "Close"}
	for k, v := range closeAttrs {
		attrs[k] = v
	}
	return sheetHeadStyle.WrapHTML(html.Div(html.DivConfig{Class: "cui-sheet-head"},
		html.Span(html.TextConfig{Class: "cui-sheet-head__title", ID: id}, render.Text(title)),
		render.Tag("button", attrs,
			render.Raw(`<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><line x1="6" y1="6" x2="18" y2="18"/><line x1="18" y1="6" x2="6" y2="18"/></svg>`)),
	))
}

var sheetHeadStyle = registry.RegisterStyle("cui-sheet-head", sheetHeadCSS)

// Knobs:
//   - --ui-sheet-close-size (36px, both axes of the close button, and
//     the row's height).
//   - --ui-sheet-head-inset (0): the gap before the title, so a sheet
//     lines its title up with its own content.
func sheetHeadCSS(_ style.Theme) string {
	return `[data-cui-comp="cui-sheet-head"] {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--spacing-md, 8px);
  block-size: var(--ui-sheet-close-size, 36px);
  padding-inline-start: var(--ui-sheet-head-inset, 0px);
}
[data-cui-comp="cui-sheet-head"] .cui-sheet-head__title { font-weight: var(--font-weight-semibold, 600); }
[data-cui-comp="cui-sheet-head"] .cui-sheet-head__close {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  inline-size: var(--ui-sheet-close-size, 36px);
  block-size: var(--ui-sheet-close-size, 36px);
  padding: 0;
  border: var(--stroke-thin, 1px) solid transparent;
  border-radius: var(--radii-md, 8px);
  background: transparent;
  color: var(--color-text-muted, currentColor);
  cursor: pointer;
}
[data-cui-comp="cui-sheet-head"] .cui-sheet-head__close:hover {
  border-color: var(--color-border, rgba(0,0,0,0.12));
  color: var(--color-text, currentColor);
}`
}

// ─── PhoneSheet ─────────────────────────────────────────────────────

// PhoneSheetConfig configures a PhoneSheet.
type PhoneSheetConfig struct {
	// ID is the sheet's element id, which the pill names to open it.
	// Unique on the page. Required.
	ID string
	// Title names the sheet: its heading, its dialog's name and the
	// pill's Name. Required.
	Title string
	// SwapAt is the width below which the body folds into the sheet.
	// In a ui.ContentRow column, match the row's Breakpoint.
	SwapAt SwapWidth
}

// PhoneSheet renders body in normal flow at and above the swap width,
// and below it as a bottom sheet a floating SheetPill opens. The sheet
// is a native popover: it opens with no script, closes on Escape and on
// a tap outside it, and holds the page still while it is open. The
// pill names the entry the body marks aria-current, so a list that
// marks where the reader is (a table of contents, a menu) shows it on
// the pill. Following a link in the sheet closes it.
func PhoneSheet(cfg PhoneSheetConfig, body render.HTML) render.HTML {
	if cfg.ID == "" {
		panic("interactive: PhoneSheet requires ID: the pill opens the sheet by its id")
	}
	if cfg.Title == "" {
		panic("interactive: PhoneSheet requires Title: it names the sheet and its pill")
	}
	cls := "cui-phone-sheet" + swapClass("PhoneSheet", "cui-phone-sheet", cfg.SwapAt)
	titleID := cfg.ID + "-title"
	return phoneSheetStyle.WrapHTML(html.Div(html.DivConfig{Class: cls},
		SheetPill(SheetPillConfig{Name: cfg.Title, Popover: cfg.ID, SwapAt: cfg.SwapAt}),
		render.Tag("div", map[string]string{
			"id":              cfg.ID,
			"class":           "cui-phone-sheet__sheet",
			"popover":         "auto",
			"role":            "dialog",
			"aria-labelledby": titleID,
		},
			html.Div(html.DivConfig{Class: "cui-phone-sheet__top"},
				sheetHead(cfg.Title, titleID, map[string]string{
					"popovertarget":       cfg.ID,
					"popovertargetaction": "hide",
				})),
			html.Div(html.DivConfig{Class: "cui-phone-sheet__body"}, body),
		),
	))
}

var phoneSheetStyle = registry.RegisterStyle("cui-phone-sheet", phoneSheetCSS)

// The root, and above the swap the sheet and its body, are
// display: contents: the body lays out as if it sat in the sheet's
// parent, so a sticky list inside it sticks to that column as it would
// with no sheet around it.
//
// Knobs:
//   - --ui-sheet-max-height (75vh): how far the sheet rises.
//   - --ui-scrim (rgba(0,0,0,0.45)): the dim behind the open sheet,
//     shared with the widget sheets.
func phoneSheetCSS(_ style.Theme) string {
	var b strings.Builder
	b.WriteString(`[data-cui-comp="cui-phone-sheet"] { display: contents; }
[data-cui-comp="cui-phone-sheet"] .cui-phone-sheet__sheet {
  position: fixed;
  inset: auto 0 0 0;
  inline-size: auto;
  max-inline-size: none;
  max-block-size: var(--ui-sheet-max-height, 75vh);
  margin: 0;
  box-sizing: border-box;
  overflow-y: auto;
  overscroll-behavior: contain;
  padding: 0 var(--spacing-lg, 16px) calc(var(--spacing-lg, 16px) + env(safe-area-inset-bottom, 0px));
  border: 0;
  border-radius: var(--radii-lg, 10px) var(--radii-lg, 10px) 0 0;
  background: var(--color-surface, #fff);
  box-shadow: var(--shadow-xl, 0 20px 40px rgba(0,0,0,0.2));
  color: var(--color-text, currentColor);
  transition: translate var(--duration-normal, 250ms) var(--easing-ease-out, ease-out),
    overlay var(--duration-normal, 250ms) allow-discrete,
    display var(--duration-normal, 250ms) allow-discrete;
}
[data-cui-comp="cui-phone-sheet"] .cui-phone-sheet__sheet:not(:popover-open) { translate: 0 100%; }
@starting-style {
  [data-cui-comp="cui-phone-sheet"] .cui-phone-sheet__sheet:popover-open { translate: 0 100%; }
}
@media (prefers-reduced-motion: reduce) {
  [data-cui-comp="cui-phone-sheet"] .cui-phone-sheet__sheet { transition: none; }
}
[data-cui-comp="cui-phone-sheet"] .cui-phone-sheet__sheet::backdrop { background: var(--ui-scrim, rgba(0,0,0,0.45)); }
html:has([data-cui-comp="cui-phone-sheet"] .cui-phone-sheet__sheet:popover-open) { overflow: hidden; }
/* The title row stays put while the list scrolls under it. */
[data-cui-comp="cui-phone-sheet"] .cui-phone-sheet__top {
  position: sticky;
  inset-block-start: 0;
  z-index: 1;
  padding-block: var(--spacing-md, 8px);
  background: var(--color-surface, #fff);
}
`)
	for _, s := range swapWidths {
		scope := swapScope(`[data-cui-comp="cui-phone-sheet"]`, "cui-phone-sheet", s.w)
		b.WriteString(strings.NewReplacer("SCOPE", scope, "MINW", s.minw).Replace(`@media (min-width: MINW) {
  SCOPE .cui-phone-sheet__sheet,
  SCOPE .cui-phone-sheet__body { display: contents; }
  SCOPE .cui-phone-sheet__top { display: none; }
}
`))
	}
	return b.String()
}
