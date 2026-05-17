package ui

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
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

// MenuItem is one row in a Menu — either an actionable item (Label
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

	// Icon is rendered to the left of Label. Inline HTML; caller
	// supplies an <svg>, character, or render.Text("⚙").
	Icon render.HTML

	// Variant tints destructive items (red) — purely a visual hint;
	// the actual confirm step belongs on the RPC via data-fui-confirm.
	Danger bool

	// Disabled greys the item out and removes it from keyboard
	// navigation.
	Disabled bool

	// Separator renders a horizontal divider instead of an item.
	// Label and other fields are ignored when true.
	Separator bool

	// Class appends to the rendered item's class list (rare; mainly
	// for testing or one-off hooks).
	Class string

	// Attrs sprinkles extra attributes onto the rendered element.
	Attrs map[string]string
}

// MenuConfig describes a dropdown menu — a trigger that, when
// activated, reveals a list of MenuItems with proper roles, keyboard
// navigation, and theming.
type MenuConfig struct {
	// ID becomes the dropdown's stable identifier. Used to pair the
	// trigger with the panel for aria-controls + analytics. Optional
	// — auto-generated when empty.
	ID string

	// Label is the trigger's visible text. Mutually exclusive with
	// TriggerHTML.
	Label string

	// TriggerHTML overrides Label with custom inline HTML. Use for
	// avatar buttons, icon-only triggers, etc.
	TriggerHTML render.HTML

	// Items is the menu's contents. Required (empty menus panic at
	// render time — they signal a bug, not a runtime state).
	Items []MenuItem

	// Position anchors the panel relative to the trigger.
	Position MenuPosition

	// TriggerClass / PanelClass append to the rendered element class
	// lists (rare).
	TriggerClass string
	PanelClass   string
}

var menuStyle = registry.RegisterStyle("ui-menu", menuCSS)

// Menu renders a dropdown. The trigger toggles the panel; the panel
// is a `role=menu` list with `role=menuitem` rows. Built on the
// runtime's `data-fui-disclosure` machinery (Esc closes, SPA nav
// closes, aria-expanded mirroring), augmented with arrow / type-ahead
// keyboard navigation that the runtime applies to any `[role=menu]`
// inside an open disclosure.
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
	cls := "ui-menu ui-menu--" + string(pos)
	if cfg.PanelClass != "" {
		cls += " " + cfg.PanelClass
	}
	b.WriteString(`<details class="` + cls + `" data-fui-disclosure data-fui-menu="` + escAttr(id) + `">`)

	// Summary = trigger. We bolt aria-haspopup="menu" so SR users know
	// the activation type; the runtime mirrors aria-expanded.
	tcls := "ui-menu__trigger"
	if cfg.TriggerClass != "" {
		tcls += " " + cfg.TriggerClass
	}
	b.WriteString(`<summary class="` + tcls + `" aria-haspopup="menu" aria-controls="` + escAttr(panelID) + `">`)
	if cfg.TriggerHTML != "" {
		b.WriteString(string(cfg.TriggerHTML))
	} else {
		b.WriteString(escText(cfg.Label))
		// A subtle caret nudges that this is a menu.
		b.WriteString(`<span class="ui-menu__caret" aria-hidden="true">▾</span>`)
	}
	b.WriteString(`</summary>`)

	b.WriteString(`<div class="ui-menu__panel" id="` + escAttr(panelID) + `" role="menu" data-fui-menu-panel>`)
	for _, it := range cfg.Items {
		writeMenuItem(&b, it)
	}
	b.WriteString(`</div></details>`)

	return menuStyle.WrapHTML(render.HTML(b.String()))
}

func writeMenuItem(b *strings.Builder, it MenuItem) {
	if it.Separator {
		b.WriteString(`<hr class="ui-menu__sep" role="separator">`)
		return
	}
	if it.Label == "" {
		panic("ui: MenuItem requires Label (or Separator: true)")
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
		openExtra = `href="` + escAttr(it.Href) + `"`
	}
	tabindex := "-1" // managed by runtime via roving focus
	disabledAttr := ""
	if it.Disabled {
		disabledAttr = `aria-disabled="true"`
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
		rpcAttr = ` data-fui-rpc="` + escAttr(it.RPC) + `" data-fui-rpc-method="` + escAttr(method) + `"`
	}
	extra := ""
	for k, v := range it.Attrs {
		extra += ` ` + escAttr(k) + `="` + escAttr(v) + `"`
	}
	b.WriteString(`<` + tag + ` class="` + cls + `" ` + openExtra +
		` role="menuitem" tabindex="` + tabindex + `"` + disabledAttr + rpcAttr + extra + `>`)
	if it.Icon != "" {
		b.WriteString(`<span class="ui-menu__icon" aria-hidden="true">` + string(it.Icon) + `</span>`)
	}
	b.WriteString(`<span class="ui-menu__label">` + escText(it.Label) + `</span></` + tag + `>`)
}

// escText is a minimal HTML text-content escaper. Sufficient for
// trusted labels generated by app code; the framework never renders
// user-supplied HTML through here.
func escText(s string) string {
	r := strings.NewReplacer(`&`, `&amp;`, `<`, `&lt;`, `>`, `&gt;`)
	return r.Replace(s)
}

func escAttr(s string) string {
	r := strings.NewReplacer(`&`, `&amp;`, `"`, `&quot;`, `<`, `&lt;`, `>`, `&gt;`)
	return r.Replace(s)
}

// shortHash is a tiny FNV-style stable hash used only to derive a
// unique fallback ID when the caller doesn't supply one. Collisions
// are visually acceptable — two menus sharing the same ID just both
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

func positionsForHash(items []MenuItem) string {
	var b strings.Builder
	for _, it := range items {
		b.WriteString(it.Label)
		b.WriteString("|")
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
  gap: 2px;
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
@media (prefers-reduced-motion: reduce) {
  [data-fui-comp="ui-menu"] .ui-menu__panel { animation: none; }
}`
}
