package ui

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// MenuPosition controls which corner of the trigger the menu panel
// anchors to. Defaults to MenuBottomStart (panel hangs below the
// trigger, aligned to its inline-start edge).
type MenuPosition string

const (
	MenuBottomStart MenuPosition = "bottom-start"
	MenuBottomEnd   MenuPosition = "bottom-end"
	MenuTopStart    MenuPosition = "top-start"
	MenuTopEnd      MenuPosition = "top-end"
)

// MenuItem is one row in a Menu: either an actionable item (Label
// required, Href / OnClickAttr / RPC etc. as supplied) or a
// separator. The framework owns the role attributes; callers only
// describe semantics.
type MenuItem struct {
	// Label is the item's visible text. Required unless Separator.
	Label string

	// Href turns the item into an <a> link. Mutually exclusive with
	// custom action attrs; if both are supplied, Href wins.
	Href string

	// RPC + RPCMethod wire the item to a server-side handler via
	// data-fui-rpc / data-fui-rpc-method. Use for "Delete this row"
	// menu items.
	RPC, RPCMethod string

	// Confirm asks the user to confirm before the RPC fires. Maps to
	// data-fui-confirm, which the runtime honors on RPC dispatch and on
	// any form submit. A menu item is neither unless it carries RPC, so
	// on a plain link item the attribute is inert.
	Confirm string

	// Icon is rendered to the left of Label. Inline HTML; caller
	// supplies an <svg>, character, or render.Text("⚙").
	Icon render.HTML

	// Variant tints destructive items (red), purely a visual hint;
	// the actual confirm step is Confirm above.
	Danger bool

	// Disabled greys the item out and removes it from keyboard
	// navigation.
	Disabled bool

	// Separator renders a horizontal divider instead of an item.
	// Label and other fields are ignored when true.
	Separator bool

	// ID becomes the rendered row's id attribute, so page JS, test
	// suites, or aria wiring elsewhere on the page can address this
	// exact item (a Help Mode toggle a script binds to, an Imports
	// row a shortcut targets). Uniqueness is caller-owned, like any
	// HTML id: duplicates are the caller's bug, not enforced here.
	// Ignored on separators, like every other field. Empty emits no
	// id, leaving the output identical to a menu that never set it.
	ID string

	// Radio, when non-empty, renders the row as a radio option:
	// role="menuitemradio" plus aria-checked (see Checked) and
	// data-fui-menu-radio="<Radio>". Every item sharing the same Radio
	// value inside one menu panel forms a radio group; exactly one of
	// them should carry Checked (like ID, uniqueness is caller-owned,
	// not enforced here). The runtime's menu module arbitrates the
	// group client-side on activation (click / Enter / Space): the
	// activated row is checked, its same-group siblings unchecked, so
	// pure-client menus feel like radios without a round trip; a row
	// carrying RPC or Href still fires it and the server re-render
	// stays authoritative. Mutually exclusive with Children (a radio
	// row is a leaf command, a submenu parent is a disclosure) — both
	// set panics at render time. Empty renders the plain menuitem, so
	// zero-value output is unchanged.
	Radio string

	// Checked sets aria-checked on a Radio row. Inert without Radio
	// (like Confirm without RPC): there is no checked state to render
	// on a plain menuitem.
	Checked bool

	// Children nests a submenu behind this row: the row renders as a
	// <summary role="menuitem" aria-haspopup="menu"> whose activation
	// reveals a nested role="menu" panel, reusing the same
	// data-fui-disclosure machinery the top level uses (Escape, SPA-nav
	// close, aria-expanded mirroring, focus-on-open). Keyboard: the
	// menu module opens it on ArrowRight (ArrowLeft in RTL) and enters,
	// closes it on ArrowLeft (ArrowRight in RTL) and ArrowUp-style
	// roving focus applies inside it; Escape closes one level at a
	// time. The parent row is purely a disclosure: setting Href, RPC,
	// or Radio on it panics at render time. Disabled greys the row out
	// and removes it from keyboard navigation like any other item; the
	// children still render (closed, unreachable) so a server-side
	// state change only needs to flip Disabled.
	Children []MenuItem

	// Class appends to the rendered item's class list (rare; mainly
	// for testing or one-off hooks).
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) onto the rendered item
	// element. Keys the item owns are dropped: class (use Class),
	// id (use ID), data-fui-* (the disclosure / rpc wiring), and the
	// menuitem contract (type, href, tabindex, role, aria-disabled,
	// disabled, aria-checked, and on submenu rows aria-haspopup /
	// aria-controls).
	ExtraAttrs map[string]string
}

// MenuConfig describes a dropdown menu: a trigger that, when
// activated, reveals a list of MenuItems with proper roles, keyboard
// navigation, and theming.
type MenuConfig struct {
	// ID becomes the dropdown's stable identifier. Used to pair the
	// trigger with the panel for aria-controls + analytics. Optional
	// auto-generated when empty.
	ID string

	// Label is the trigger's visible text. Mutually exclusive with
	// TriggerHTML.
	Label string

	// TriggerHTML overrides Label with custom inline HTML. Use for
	// avatar buttons, icon-only triggers, etc.
	TriggerHTML render.HTML

	// Items is the menu's contents. Required (empty menus panic at
	// render time. They signal a bug, not a runtime state).
	Items []MenuItem

	// Position anchors the panel relative to the trigger.
	Position MenuPosition

	// TriggerClass / PanelClass append to the rendered element class
	// lists (rare).
	TriggerClass string
	PanelClass   string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the menu's root <details>
	// element. Keys the component owns are dropped: class, id, and
	// data-fui-* (the disclosure wiring).
	ExtraAttrs html.Attrs
}

var menuStyle = registry.RegisterStyle("ui-menu", menuCSS)

// Menu renders a dropdown. The trigger toggles the panel; the panel
// is a `role=menu` list with `role=menuitem` rows — `menuitemradio`
// rows for items with Radio set, and nested `role=menu` submenus for
// items with Children. Built on the runtime's `data-fui-disclosure`
// machinery (Esc closes one level at a time, SPA nav closes,
// aria-expanded mirroring), augmented with arrow / type-ahead keyboard
// navigation that the runtime applies to any `[role=menu]` inside an
// open disclosure.
func Menu(cfg MenuConfig) render.HTML {
	if len(cfg.Items) == 0 {
		panic("ui: Menu requires at least one Item")
	}
	pos := cfg.Position
	if pos == "" {
		pos = MenuBottomStart
	}

	id := cfg.ID
	if id == "" {
		id = "ui-menu-" + shortHash(cfg.Label+positionsForHash(cfg.Items))
	}
	panelID := id + "-panel"

	var b strings.Builder
	// `<details>` is the toggle. data-fui-disclosure adds Escape close
	// and closes on SPA nav for free.
	//
	// PanelClass / TriggerClass are caller-supplied and pass through
	// render.Escape so a value containing `"` or `'` cannot break out of
	// the class attribute into a sibling attribute context
	// (`onclick=…`, etc.).
	cls := "ui-menu ui-menu--" + string(pos)
	if cfg.PanelClass != "" {
		cls += " " + cfg.PanelClass
	}
	b.WriteString(`<details class="` + render.Escape(cls) + `" data-fui-disclosure data-fui-menu="` + render.Escape(id) + `"`)
	b.WriteString(serializeExtraAttrs(html.SafeExtraAttrs(cfg.ExtraAttrs)))
	b.WriteString(`>`)

	// Summary = trigger. We bolt aria-haspopup="menu" so SR users know
	// the activation type; the runtime mirrors aria-expanded.
	tcls := "ui-menu__trigger"
	if cfg.TriggerClass != "" {
		tcls += " " + cfg.TriggerClass
	}
	b.WriteString(`<summary class="` + render.Escape(tcls) + `" aria-haspopup="menu" aria-controls="` + render.Escape(panelID) + `">`)
	if cfg.TriggerHTML != "" {
		b.WriteString(string(cfg.TriggerHTML))
	} else {
		b.WriteString(render.Escape(cfg.Label))
		// A subtle caret nudges that this is a menu.
		b.WriteString(`<span class="ui-menu__caret" aria-hidden="true">▾</span>`)
	}
	b.WriteString(`</summary>`)

	b.WriteString(`<div class="ui-menu__panel" id="` + render.Escape(panelID) + `" role="menu" data-fui-menu-panel>`)
	writeMenuItems(&b, cfg.Items, panelID)
	b.WriteString(`</div></details>`)

	return menuStyle.WrapHTML(render.HTML(b.String()))
}

// writeMenuItems renders one panel's rows. parentPanelID seeds the
// deterministic id chain for nested submenu panels (parentPanelID +
// "-sub-<index>"), so ids stay unique within a menu without caller
// input.
func writeMenuItems(b *strings.Builder, items []MenuItem, parentPanelID string) {
	for i, it := range items {
		writeMenuItem(b, it, parentPanelID, i)
	}
}

func writeMenuItem(b *strings.Builder, it MenuItem, parentPanelID string, idx int) {
	if it.Separator {
		b.WriteString(`<hr class="ui-menu__sep" role="separator">`)
		return
	}
	if it.Label == "" {
		panic("ui: MenuItem requires Label (or Separator: true)")
	}
	if len(it.Children) > 0 {
		if it.Radio != "" {
			panic("ui: MenuItem with Radio cannot have Children (a radio row is a leaf command, a submenu parent is a disclosure)")
		}
		if it.Href != "" || it.RPC != "" {
			panic("ui: MenuItem with Children cannot also set Href or RPC (a submenu parent is purely a disclosure)")
		}
		writeSubMenu(b, it, parentPanelID, idx)
		return
	}
	cls := "ui-menu__item"
	if it.Danger {
		cls += " ui-menu__item--danger"
	}
	if it.Disabled {
		cls += " ui-menu__item--disabled"
	}
	if it.Class != "" {
		cls += " " + it.Class
	}
	tag := "button"
	openExtra := `type="button"`
	if it.Href != "" {
		tag = "a"
		// safeURL drops javascript:, data:, vbscript:, file:, blob:,
		// protocol-relative //host, and control bytes (see safety.go);
		// a rejected href degrades to "#" like ui.Card / ui.Link.
		href := urlsafe.CleanAnchor(it.Href)
		if href == "" {
			href = "#"
		}
		openExtra = `href="` + render.Escape(href) + `"`
	}
	tabindex := "-1" // managed by runtime via roving focus
	// Radio rows swap the role and carry their group + checked state;
	// the group attr names the runtime's client-side arbitration set.
	radioAttr := ""
	role := "menuitem"
	if it.Radio != "" {
		role = "menuitemradio"
		checked := "false"
		if it.Checked {
			checked = "true"
		}
		radioAttr = ` aria-checked="` + checked + `" data-fui-menu-radio="` + render.Escape(it.Radio) + `"`
	}
	disabledAttr := ""
	if it.Disabled {
		// Leading space: this is the only optional fragment in the
		// row's attribute chain whose neighbours (tabindex above,
		// data-fui-menu-radio on radio rows) do NOT carry one, so
		// omitting it glues two attributes together.
		disabledAttr = ` aria-disabled="true"`
		if tag == "button" {
			disabledAttr += ` disabled`
		}
	}
	rpcAttr := ""
	if it.RPC != "" && it.Href == "" {
		method := it.RPCMethod
		if method == "" {
			method = "POST"
		}
		rpcAttr = ` data-fui-rpc="` + render.Escape(it.RPC) + `" data-fui-rpc-method="` + render.Escape(method) + `"`
		if it.Confirm != "" {
			rpcAttr += ` data-fui-confirm="` + render.Escape(it.Confirm) + `"`
		}
	}
	// ID lands right after class, mirroring the panel div (class,
	// id, role). Empty stays empty so zero-value output is unchanged.
	idAttr := ""
	if it.ID != "" {
		idAttr = ` id="` + render.Escape(it.ID) + `"`
	}
	// ExtraAttrs join the SafeExtraAttrs contract: the item owns
	// type/href/tabindex/role/aria-disabled/disabled/aria-checked plus
	// the rpc data-fui-* wiring (data-fui-menu-radio included — every
	// data-fui-* key is reserved). serializeExtraAttrs sorts the
	// survivors and validates each key via render.Attr (unsafe keys
	// drop).
	extra := serializeExtraAttrs(html.SafeExtraAttrs(it.ExtraAttrs,
		"type", "href", "tabindex", "role", "aria-disabled", "disabled", "aria-checked"))
	b.WriteString(`<` + tag + ` class="` + render.Escape(cls) + `"` + idAttr + ` ` + openExtra +
		` role="` + role + `" tabindex="` + tabindex + `"` + radioAttr + disabledAttr + rpcAttr + extra + `>`)
	if it.Icon != "" {
		b.WriteString(`<span class="ui-menu__icon" aria-hidden="true">` + string(it.Icon) + `</span>`)
	}
	b.WriteString(`<span class="ui-menu__label">` + render.Escape(it.Label) + `</span></` + tag + `>`)
}

// writeSubMenu renders a parent row plus its nested role="menu"
// panel. The wrapper is a <details data-fui-disclosure data-fui-menu>,
// i.e. the exact machinery the top level uses, so Escape, SPA-nav
// close, aria-expanded mirroring, focus-on-open, and the menu
// module's keyboard handling all apply at depth without a second
// mechanism. The summary IS the parent menuitem (role=menuitem,
// aria-haspopup="menu", tabindex=-1 roving like every row); the
// caller-incoherent combos (Radio, Href, RPC) were refused in
// writeMenuItem before we got here.
//
// A Disabled parent renders the same markup with aria-disabled and
// the disabled class: the row drops out of keyboard rotation and
// pointer-events, while the children stay in the DOM (closed,
// unreachable), so re-enabling is a data change, not a rebuild.
func writeSubMenu(b *strings.Builder, it MenuItem, parentPanelID string, idx int) {
	subID := parentPanelID + "-sub-" + itoaSmall(idx)
	subPanelID := subID + "-panel"
	cls := "ui-menu__item ui-menu__item--hassub"
	if it.Danger {
		cls += " ui-menu__item--danger"
	}
	if it.Disabled {
		cls += " ui-menu__item--disabled"
	}
	if it.Class != "" {
		cls += " " + it.Class
	}
	disabledAttr := ""
	if it.Disabled {
		// <summary> has no native disabled attribute; aria + CSS
		// pointer-events carry the state.
		disabledAttr = ` aria-disabled="true"`
	}
	idAttr := ""
	if it.ID != "" {
		idAttr = ` id="` + render.Escape(it.ID) + `"`
	}
	extra := serializeExtraAttrs(html.SafeExtraAttrs(it.ExtraAttrs,
		"type", "href", "tabindex", "role", "aria-disabled", "disabled",
		"aria-haspopup", "aria-controls", "aria-checked"))
	b.WriteString(`<details class="ui-menu__sub" data-fui-disclosure data-fui-menu="` + render.Escape(subID) + `">`)
	b.WriteString(`<summary class="` + render.Escape(cls) + `"` + idAttr +
		` aria-haspopup="menu" aria-controls="` + render.Escape(subPanelID) +
		`" role="menuitem" tabindex="-1"` + disabledAttr + extra + `>`)
	if it.Icon != "" {
		b.WriteString(`<span class="ui-menu__icon" aria-hidden="true">` + string(it.Icon) + `</span>`)
	}
	// The disclosure caret is a CSS ::after (not a span like the
	// trigger's) so it never pollutes the row's textContent —
	// type-ahead matches the label alone.
	b.WriteString(`<span class="ui-menu__label">` + render.Escape(it.Label) + `</span></summary>`)
	b.WriteString(`<div class="ui-menu__panel ui-menu__panel--sub" id="` + render.Escape(subPanelID) + `" role="menu" data-fui-menu-panel>`)
	writeMenuItems(b, it.Children, subPanelID)
	b.WriteString(`</div></details>`)
}

// shortHash is a tiny FNV-style stable hash used only to derive a
// unique fallback ID when the caller doesn't supply one. Collisions
// are visually acceptable. Two menus sharing the same ID just both
// respond to the same Esc; nothing breaks.
func shortHash(s string) string {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	// Render as 8 lowercase hex chars.
	const digits = "0123456789abcdef"
	out := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		out[i] = digits[h&0xF]
		h >>= 4
	}
	return string(out)
}

// positionsForHash folds every item label (recursing into submenus,
// which contribute their rows to the auto-id identity) into the
// shortHash input. Zero-value menus (no Children anywhere) produce
// the exact bytes they did before submenus existed.
func positionsForHash(items []MenuItem) string {
	var b strings.Builder
	for _, it := range items {
		b.WriteString(it.Label)
		b.WriteString("|")
		if len(it.Children) > 0 {
			b.WriteString(positionsForHash(it.Children))
		}
	}
	return b.String()
}

func menuCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-menu"].ui-menu {
  position: relative;
  display: inline-block;
}
[data-fui-comp="ui-menu"] > summary.ui-menu__trigger {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-xs, 4px);
  cursor: pointer;
  list-style: none;
  user-select: none;
  padding: var(--spacing-sm, 6px) var(--spacing-md, 10px);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFF);
  color: var(--color-text, #18181B);
  font: inherit;
  min-height: var(--spacing-touch-target, 44px);
}
[data-fui-comp="ui-menu"] > summary.ui-menu__trigger::-webkit-details-marker { display: none; }
[data-fui-comp="ui-menu"] > summary.ui-menu__trigger:hover  { background: var(--color-surface-soft, #F4F4F5); }
[data-fui-comp="ui-menu"] > summary.ui-menu__trigger:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}
[data-fui-comp="ui-menu"] .ui-menu__caret { font-size: 0.75em; opacity: 0.7; }
[data-fui-comp="ui-menu"] .ui-menu__panel {
  position: absolute;
  z-index: var(--z-dropdown, 100);
  min-width: 12rem;
  max-width: min(20rem, calc(100vw - 2rem));
  padding: var(--spacing-xs, 4px);
  background: var(--color-surface, #FFF);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  box-shadow: var(--shadow-lg, 0 10px 15px -3px rgba(0,0,0,.10));
  display: grid;
  gap: var(--spacing-xs, 2px);
  animation: ui-menu-in var(--duration-dropdown-enter, 120ms)
    var(--easing-ease-out, cubic-bezier(0.16, 1, 0.3, 1));
}
[data-fui-comp="ui-menu"].ui-menu--bottom-start .ui-menu__panel { inset-inline-start: 0; top: calc(100% + 4px); }
[data-fui-comp="ui-menu"].ui-menu--bottom-end   .ui-menu__panel { inset-inline-end: 0;   top: calc(100% + 4px); }
[data-fui-comp="ui-menu"].ui-menu--top-start    .ui-menu__panel { inset-inline-start: 0; bottom: calc(100% + 4px); }
[data-fui-comp="ui-menu"].ui-menu--top-end      .ui-menu__panel { inset-inline-end: 0;   bottom: calc(100% + 4px); }
@keyframes ui-menu-in {
  from { opacity: 0; transform: translateY(-4px) scale(0.98); }
  to   { opacity: 1; transform: translateY(0)    scale(1);    }
}
[data-fui-comp="ui-menu"] .ui-menu__item {
  display: flex;
  align-items: center;
  gap: var(--spacing-sm, 6px);
  width: 100%;
  text-align: start;
  padding: var(--spacing-sm, 8px) var(--spacing-md, 12px);
  background: transparent;
  color: inherit;
  border: 0;
  border-radius: var(--radii-sm, 4px);
  cursor: pointer;
  font: inherit;
  text-decoration: none;
  min-height: var(--spacing-touch-target, 44px);
}
[data-fui-comp="ui-menu"] .ui-menu__item:hover,
[data-fui-comp="ui-menu"] .ui-menu__item:focus-visible {
  background: var(--color-surface-soft, #F4F4F5);
  outline: none;
}
[data-fui-comp="ui-menu"] .ui-menu__item--danger { color: var(--color-danger, #DC2626); }
[data-fui-comp="ui-menu"] .ui-menu__item--danger:hover,
[data-fui-comp="ui-menu"] .ui-menu__item--danger:focus-visible {
  background: color-mix(in srgb, var(--color-danger, #DC2626) 10%, transparent);
}
[data-fui-comp="ui-menu"] .ui-menu__item--disabled {
  opacity: 0.5;
  cursor: not-allowed;
  pointer-events: none;
}
[data-fui-comp="ui-menu"] .ui-menu__icon { display: inline-flex; width: 1em; justify-content: center; }
[data-fui-comp="ui-menu"] .ui-menu__label { flex: 1; }
[data-fui-comp="ui-menu"] .ui-menu__sep {
  border: 0;
  border-top: 1px solid var(--color-border, #E4E4E7);
  margin: var(--spacing-xs, 4px) 0;
}
/* Submenus: the nested <details> is one grid child of the parent
   panel; it positions the nested panel, which reuses every panel
   chrome rule above. The position-variant inset rules on the outer
   details would also match the nested panel, so this rule carries a
   higher specificity (element + two classes vs attr + class + class)
   and always wins, at any depth. */
[data-fui-comp="ui-menu"] details.ui-menu__sub { position: relative; display: block; }
[data-fui-comp="ui-menu"] details.ui-menu__sub > summary.ui-menu__item {
  list-style: none;
}
[data-fui-comp="ui-menu"] details.ui-menu__sub > summary.ui-menu__item::-webkit-details-marker { display: none; }
[data-fui-comp="ui-menu"] details.ui-menu__sub > .ui-menu__panel {
  inset-inline-start: 100%;
  top: calc(-1 * var(--spacing-xs, 4px));
}
/* The submenu caret is a pseudo-element so it stays out of the row's
   textContent (type-ahead) and accessible name, unlike the trigger's
   <span> caret. :dir() flips it in RTL; unsupported engines show the
   LTR glyph, a cosmetic-only degradation. */
[data-fui-comp="ui-menu"] .ui-menu__item--hassub::after {
  content: "▸";
  font-size: 0.75em;
  opacity: 0.7;
}
:dir(rtl) [data-fui-comp="ui-menu"] .ui-menu__item--hassub::after { content: "◂"; }
/* Radio rows: the check indicator is likewise a pseudo-element —
   space is reserved in both states so labels align whether checked
   or not. */
[data-fui-comp="ui-menu"] [role="menuitemradio"]::before {
  content: "✓";
  display: inline-flex;
  width: 1em;
  flex: none;
  justify-content: center;
  visibility: hidden;
}
[data-fui-comp="ui-menu"] [role="menuitemradio"][aria-checked="true"]::before { visibility: visible; }
@media (prefers-reduced-motion: reduce) {
  [data-fui-comp="ui-menu"] .ui-menu__panel { animation: none; }
}`
}
