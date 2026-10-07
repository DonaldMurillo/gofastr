package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
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
	// data-cui-rpc / data-cui-rpc-method. Use for "Delete this row"
	// menu items.
	RPC, RPCMethod string

	// Confirm asks the user to confirm before the RPC fires. Maps to
	// data-cui-confirm, which the runtime honors on RPC dispatch and on
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
	// row a shortcut targets). Uniqueness is caller-owned within one
	// menu, like any HTML id: the menu refuses duplicates it can see.
	// Ignored on separators, like every other field. Empty emits no
	// id, leaving the output identical to a menu that never set it.
	ID string

	// Radio, when non-empty, renders the row as a radio option:
	// role="menuitemradio" plus aria-checked (see Checked) and
	// data-hui-menu-radio="<Radio>". Every item sharing the same Radio
	// value within one Menu forms a radio group — across submenus too:
	// a picker whose rarer options sit behind a "More" submenu is one
	// group, not two. Exactly one of them should carry Checked (like
	// ID, uniqueness is caller-owned, not enforced here). The runtime's
	// menu module arbitrates the group client-side on activation
	// (click / Enter / Space): the activated row is checked, its
	// same-group siblings — anywhere in the same menu — unchecked, so
	// pure-client menus feel like radios without a round trip; a row
	// carrying RPC or Href still fires it and the server re-render
	// stays authoritative. Mutually exclusive with Children (a radio
	// row is a leaf command, a submenu parent is a disclosure) — both
	// set panics at render time. Empty renders the plain menuitem, so
	// zero-value output is unchanged.
	Radio string

	// Check renders the row as a checkbox option (menuitemcheckbox)
	// with a check mark while Checked. The state is the server's: pair
	// it with Href (a link that toggles the option) or RPC. Mutually
	// exclusive with Radio and Children.
	Check bool

	// Checked sets aria-checked on a Radio or Check row. Inert on a
	// plain menuitem: there is no checked state to render.
	Checked bool

	// Action renders the row as a form submission instead of a link or
	// RPC, for hosts whose command rows hit PRG endpoints (stop
	// impersonation, sign out with server-side sessions) where a plain
	// Href would widen a state-changing endpoint to GET. Nil (the zero
	// value) renders nothing — Action is a pointer exactly so the
	// unconfigured case cannot half-render. See MenuAction for the
	// CSRF contract. Mutually exclusive with Href, RPC, Radio, and
	// Children: incoherent combos panic at render time.
	Action *MenuAction

	// Children nests a submenu behind this row: the row renders as a
	// <summary role="menuitem" aria-haspopup="menu"> whose activation
	// reveals a nested role="menu" panel, reusing the same disclosure
	// machinery the top level uses (Escape, SPA-nav close,
	// aria-expanded mirroring, focus-on-open). Keyboard: the menu
	// module opens it on ArrowRight (ArrowLeft in RTL) and enters,
	// closes it on ArrowLeft (ArrowRight in RTL) and ArrowUp-style
	// roving focus applies inside it; Escape closes one level at a
	// time. The parent row is purely a disclosure: setting Href, RPC,
	// or Radio on it panics at render time. Disabled greys the row out
	// and removes it from keyboard navigation like any other item; the
	// children still render (closed, unreachable) so a server-side
	// state change only needs to flip Disabled.
	Children []MenuItem

	// Do renders the row as a button carrying a built RPC, effects and
	// toasts included: a Delete that confirms, re-fetches the page on
	// success and toasts a refusal. Mutually exclusive with Href, RPC,
	// Action, Copy, Radio and Children.
	Do *interactive.Action

	// Copy renders the row as a copy-to-clipboard command over another
	// element's text, as CopyButton does. Mutually exclusive with Href,
	// RPC, Do, Action, Radio and Children.
	Copy *MenuCopy

	// Class appends to the rendered item's class list (rare; mainly
	// for testing or one-off hooks).
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) onto the rendered item
	// element. Keys the item owns are dropped: class (use Class),
	// id (use ID), the runtime wiring (data-cui-*), and the menuitem
	// contract (type, href, tabindex, role, aria-disabled, disabled,
	// aria-checked).
	ExtraAttrs map[string]string
}

// MenuCopy is MenuItem's copy row: Target is the id of the element
// whose text it copies (a leading "#" is allowed), Toast the title of
// the success toast, empty for none.
type MenuCopy = headless.MenuCopy

// MenuAction is MenuItem's form-POST row. The menuitem is a submit
// button inside <form method action>; Fields supplies the hidden
// inputs.
//
// CSRF contract: the framework never mints or verifies tokens — the
// caller's form middleware owns that exactly as it would for any
// inline form. But a state-changing POST with NO hidden inputs is
// almost certainly a forgotten token, so the default refuses it: an
// Action with no Fields panics unless Unsafe explicitly acknowledges
// the endpoint carries its own protection (same-origin POST + session
// checks, a one-shot token in the path, or a reviewed exception like
// the port's tokenless parity surface). Unsafe is greppable debt, not
// a recommendation.
type MenuAction struct {
	// Path is the form's action URL.
	Path string

	// Method defaults to POST. GET is allowed for completeness but
	// defeats the point; prefer Href for navigations.
	Method string

	// Fields are the hidden inputs, first use: the CSRF token.
	Fields map[string]string

	// Unsafe acknowledges the no-Fields case deliberately.
	Unsafe bool
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
	// avatar buttons and other custom triggers.
	TriggerHTML render.HTML

	// IconOnly draws the trigger as the "more" glyph, a compact square
	// like an icon button, with Label as its accessible name and no
	// caret: a table row's actions, where the row already says what
	// the menu acts on. Ignored with TriggerHTML or TriggerElement.
	IconOnly bool

	// Avatar draws the trigger as that avatar alone: round, borderless,
	// 2rem, with a focus ring (the account menu in an app bar). Label
	// stays the trigger's accessible name, after the avatar's own name.
	// Mutually exclusive with TriggerHTML, TriggerElement and IconOnly.
	Avatar *AvatarConfig

	// TriggerElement replaces the framework-rendered summary with a
	// caller-owned interactive element: inline HTML for a real <button>
	// (or <a>). The menu renders a summary-less disclosure holding the
	// panel, with the element in a presentation wrapper beside it, and
	// the runtime makes the element the disclosure controller: click,
	// Enter, and Space toggle the panel; aria-haspopup /
	// aria-controls / aria-expanded are wired onto the element at
	// hydration (attributes cannot be injected into raw caller HTML
	// server-side); focus lands on the first menuitem on open; Escape
	// closes one level at a time and returns focus to the element; Tab
	// closes the chain. Use for host-styled triggers whose markup and
	// classes the page owns: routing such an element through
	// TriggerHTML nests an interactive control inside the summary
	// control, which axe reports as nested-interactive (SERIOUS).
	// Activation is preventDefaulted — the element opens the menu, it
	// does not navigate or submit; put navigation on menu items.
	// Overrides Label and TriggerHTML. Auto-generated IDs fold the
	// markup in, but two structurally identical trigger menus on one
	// page still need distinct IDs.
	TriggerElement render.HTML

	// Items is the menu's contents. Required (empty menus panic at
	// render time. They signal a bug, not a runtime state).
	Items []MenuItem

	// Position anchors the panel relative to the trigger.
	Position MenuPosition

	// LazyPanel keeps the panel's rows out of the document tree until
	// the menu is first opened: SSR wraps them in an inert
	// <template data-hui-menu-lazy> as the panel's only child, and the
	// menu module moves them into the panel on first open (the panel
	// <div> itself always renders, so aria-controls still resolves
	// while closed). Use it when page-scoped consumers must not see
	// closed-menu rows in the live DOM: host test contracts that pin
	// getByText('Theme') to a visible element or getByLabel to exactly
	// one. The rows are still in the HTML source, so this hides
	// nothing from a crawler that parses the response. The cost: rows
	// are not in the DOM until first open, so host JS that binds rows
	// by id at page load must use delegated listeners instead, and
	// with JavaScript disabled the menu opens empty (only the module
	// mounts the rows). The zero value (false) renders rows inline
	// exactly as before. Applies to the summary path and
	// TriggerElement alike; nested submenus live inside the template
	// and mount with the rest.
	LazyPanel bool

	// TriggerClass / PanelClass append to the rendered element class
	// lists (rare).
	TriggerClass string
	PanelClass   string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the menu's root element
	// (the <details> on the summary path, the wrapper <div> when
	// TriggerElement is set). Keys the component owns are dropped:
	// class, id, and the disclosure/menu wiring.
	ExtraAttrs html.Attrs
}

var menuStyle = registry.RegisterStyle("ui-menu", menuCSS)

// Menu renders a dropdown. The trigger toggles the panel; the panel
// is a `role=menu` list with `role=menuitem` rows — `menuitemradio`
// rows for items with Radio set, and nested `role=menu` submenus for
// items with Children. Renders headless.Menu dressed with the fui-menu
// class map: the disclosure machinery (Escape one level at a time,
// SPA-nav close, aria-expanded mirroring) is headless-disclosure's,
// and the keyboard contract (roving focus, type-ahead, RTL-aware
// submenus, radio arbitration) is headless-menu's. With
// TriggerElement set there is no framework summary: the caller's
// element is the controller (see MenuConfig.TriggerElement).
func Menu(cfg MenuConfig) render.HTML {
	items := make([]headless.MenuItem, len(cfg.Items))
	for i, it := range cfg.Items {
		items[i] = headlessMenuItem(it)
		children := make([]headless.MenuItem, len(it.Children))
		for j, c := range it.Children {
			children[j] = headlessMenuItem(c)
		}
		items[i].Children = children
	}
	classes := headless.Classes{
		headless.PartRoot:        "fui-menu",
		headless.PartSummary:     "fui-menu__trigger",
		headless.PartMenuCaret:   "fui-menu__caret",
		headless.PartPanel:       "fui-menu__panel",
		headless.PartMenuItem:    "fui-menu__item",
		headless.PartIcon:        "fui-menu__icon",
		headless.PartText:        "fui-menu__label",
		headless.PartDividerLine: "fui-menu__sep",
		headless.PartMenuSubmenu: "fui-menu__sub",
		headless.PartMenuForm:    "fui-menu__form",
	}
	for _, pos := range []string{"bottom-start", "bottom-end", "top-start", "top-end"} {
		classes[headless.Part("root--"+pos)] = "fui-menu--" + pos
	}
	for _, v := range []string{"danger", "disabled", "hassub"} {
		classes[headless.Part("menu-item--"+v)] = "fui-menu__item--" + v
	}
	classes[headless.Part("panel--sub")] = "fui-menu__panel--sub"
	if cfg.TriggerClass != "" {
		classes[headless.PartSummary] += " " + cfg.TriggerClass
	}
	if cfg.PanelClass != "" {
		classes[headless.PartPanel] += " " + cfg.PanelClass
	}
	trigger := cfg.TriggerHTML
	if cfg.Avatar != nil {
		if trigger != "" || cfg.TriggerElement != "" || cfg.IconOnly {
			panic("ui: Menu Avatar draws the trigger; drop TriggerHTML, TriggerElement and IconOnly")
		}
		classes[headless.PartSummary] += " fui-menu__trigger--avatar"
		trigger = headless.Own(render.Join(
			Avatar(*cfg.Avatar),
			html.Span(html.TextConfig{Class: "fui-visually-hidden"}, render.Text(cfg.Label)),
		))
	}
	if cfg.IconOnly && trigger == "" && cfg.TriggerElement == "" {
		classes[headless.PartSummary] += " fui-menu__trigger--icon"
		trigger = headless.Own(render.Join(
			Icon("more", IconConfig{Size: "18", ExtraAttrs: html.Attrs{"aria-hidden": "true"}}),
			html.Span(html.TextConfig{Class: "fui-visually-hidden"}, render.Text(cfg.Label)),
		))
	}
	out := headless.Menu(headless.MenuProps{
		ID:             cfg.ID,
		Label:          cfg.Label,
		TriggerHTML:    trigger,
		TriggerElement: cfg.TriggerElement,
		Items:          items,
		Position:       string(cfg.Position),
		LazyPanel:      cfg.LazyPanel,
		ExtraAttrs:     headless.Safe(cfg.ExtraAttrs),
	}, classes)
	return menuStyle.WrapHTML(out)
}

// headlessMenuItem maps one row, without its children, onto the
// headless row.
func headlessMenuItem(it MenuItem) headless.MenuItem {
	extras := headless.Safe(it.ExtraAttrs)
	if it.Class != "" {
		extras["class"] = it.Class
	}
	var action *headless.MenuAction
	if it.Action != nil {
		action = &headless.MenuAction{
			Path:   it.Action.Path,
			Method: it.Action.Method,
			Fields: it.Action.Fields,
			Unsafe: it.Action.Unsafe,
		}
	}
	var rpcAttrs map[string]string
	if it.Do != nil {
		rpcAttrs = it.Do.Attrs()
	}
	return headless.MenuItem{
		Label:      it.Label,
		Href:       it.Href,
		RPC:        it.RPC,
		RPCMethod:  it.RPCMethod,
		RPCAttrs:   rpcAttrs,
		Copy:       it.Copy,
		Confirm:    it.Confirm,
		Icon:       it.Icon,
		Danger:     it.Danger,
		Disabled:   it.Disabled,
		Separator:  it.Separator,
		ID:         it.ID,
		Radio:      it.Radio,
		Check:      it.Check,
		Checked:    it.Checked,
		Action:     action,
		ExtraAttrs: extras,
	}
}

func menuCSS(_ style.Theme) string {
	return `:where([data-cui-comp="ui-menu"]).fui-menu {
  position: relative;
  display: inline-block;
}
[data-cui-comp="ui-menu"] > summary.fui-menu__trigger {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
  cursor: pointer;
  list-style: none;
  user-select: none;
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
  transition: background var(--duration-fast, 150ms) var(--easing-ease-in-out, ease), box-shadow var(--duration-fast, 150ms) var(--easing-ease-in-out, ease), translate var(--duration-fast, 150ms) var(--easing-ease-in-out, ease);
}
[data-cui-comp="ui-menu"] > summary.fui-menu__trigger::-webkit-details-marker { display: none; }
/* Press knobs: --ui-menu-trigger-{hover,active}-{translate,shadow}
   over the shared --ui-press-* knobs, as on a button. */
[data-cui-comp="ui-menu"] > summary.fui-menu__trigger:hover {
  background: var(--color-surface-soft, #F4F4F5);
  box-shadow: var(--ui-menu-trigger-hover-shadow, var(--ui-press-hover-shadow, var(--shadow-xs)));
  translate: var(--ui-menu-trigger-hover-translate, var(--ui-press-hover-translate, none));
}
[data-cui-comp="ui-menu"] > summary.fui-menu__trigger:active {
  box-shadow: var(--ui-menu-trigger-active-shadow, var(--ui-press-active-shadow, var(--ui-menu-trigger-hover-shadow, var(--ui-press-hover-shadow, var(--shadow-xs)))));
  translate: var(--ui-menu-trigger-active-translate, var(--ui-press-active-translate, var(--ui-menu-trigger-hover-translate, var(--ui-press-hover-translate, none))));
}
[data-cui-comp="ui-menu"] > summary.fui-menu__trigger:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
/* IconOnly: a quiet square the touch target's size, the glyph centred,
   for a row's actions where a bordered button per row would shout. It
   takes the surface only on hover. */
[data-cui-comp="ui-menu"] > summary.fui-menu__trigger--icon {
  justify-content: center;
  padding: 0;
  inline-size: var(--spacing-touch-target, 44px);
  border-color: transparent;
  background: transparent;
  box-shadow: none;
  color: var(--color-text-muted, #52525B);
}
[data-cui-comp="ui-menu"] > summary.fui-menu__trigger--icon:hover {
  color: var(--color-text, #18181B);
}
/* Avatar: the avatar is the whole trigger, round, with the ring as the
   only chrome. A coarse pointer keeps the touch target around it. */
[data-cui-comp="ui-menu"] > summary.fui-menu__trigger--avatar {
  justify-content: center;
  padding: 0;
  min-height: 0;
  border: 0;
  border-radius: var(--radii-full, 9999px);
  background: transparent;
  box-shadow: none;
  --ui-avatar-size: 2rem;
}
[data-cui-comp="ui-menu"] > summary.fui-menu__trigger--avatar:hover { background: transparent; box-shadow: none; translate: none; }
@media (pointer: coarse) {
  [data-cui-comp="ui-menu"] > summary.fui-menu__trigger--avatar { min-inline-size: var(--spacing-touch-target, 44px); min-block-size: var(--spacing-touch-target, 44px); }
}
/* Knobs: --ui-menu-caret-size (12px) is the trigger caret's square;
   --ui-menu-min-width (12rem) and --ui-menu-max-width (20rem) bound
   the panel; --ui-menu-submenu-caret-opacity (0.7) fades the submenu
   arrow pseudo-glyph. */
/* The caret glyph stays in the markup for no-CSS readers; the sheet
   draws the stroked chevron the Select uses in its place. */
[data-cui-comp="ui-menu"] .fui-menu__caret {
  inline-size: var(--ui-menu-caret-size, 12px);
  block-size: var(--ui-menu-caret-size, 12px);
  font-size: 0;
  background: currentColor;
  opacity: var(--opacity-muted, 0.6);
  -webkit-mask: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 12 12'%3E%3Cpath d='M3 4.5l3 3 3-3' fill='none' stroke='black' stroke-width='1.5' stroke-linecap='round' stroke-linejoin='round'/%3E%3C/svg%3E") center / contain no-repeat;
  mask: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 12 12'%3E%3Cpath d='M3 4.5l3 3 3-3' fill='none' stroke='black' stroke-width='1.5' stroke-linecap='round' stroke-linejoin='round'/%3E%3C/svg%3E") center / contain no-repeat;
}
[data-cui-comp="ui-menu"] .fui-menu__panel {
  position: absolute;
  z-index: var(--z-dropdown, 100);
  min-width: var(--ui-menu-min-width, 12rem);
  max-width: min(var(--ui-menu-max-width, 20rem), calc(100vw - var(--spacing-2xl, 32px)));
  padding: var(--spacing-sm, 4px);
  background: var(--color-surface, #FFF);
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-lg, 10px);
  box-shadow: var(--shadow-lg, 0 10px 15px -3px rgba(0,0,0,.10));
  display: grid;
  gap: var(--spacing-xs, 2px);
  animation: fui-menu-in var(--duration-dropdown-enter, 120ms)
    var(--easing-ease-out, cubic-bezier(0.16, 1, 0.3, 1));
}
/* The UA sheet hides closed-details children with display:none, but the
   author display:grid above would override it — the panel would render
   visibly while details.open is false, and AT/role engines prune the
   whole menu (the trigger's own activation state lies). Hide explicitly;
   the open state is the UA default. The details type selector is
   load-bearing: on the TriggerElement path the root carrying
   data-cui-comp is a plain div, which never carries [open] — an
   untyped rule matches unconditionally and hides the panel even while
   the summary-less details child is open (#386). */
details[data-cui-comp="ui-menu"]:not([open]) .fui-menu__panel { display: none; }
/* Trigger-element path (MenuConfig.TriggerElement): the root is a div,
   so the rule above cannot key [open] on it — the closed panel is
   hidden through the summary-less <details data-hui-menu> child for
   the same author-display:grid reason. The wrapper generates no box
   (display:contents), so the caller's element is laid out as a direct
   child of the root and host CSS written against the element itself
   (header button.rounded-full) keeps working; role="presentation"
   keeps it out of the accessibility tree. */
[data-cui-comp="ui-menu"] > [data-hui-menu-trigger] { display: contents; }
[data-cui-comp="ui-menu"] > details[data-hui-menu]:not([open]) .fui-menu__panel { display: none; }
[data-cui-comp="ui-menu"].fui-menu--bottom-start .fui-menu__panel { inset-inline-start: 0; top: calc(100% + var(--spacing-sm, 4px)); }
[data-cui-comp="ui-menu"].fui-menu--bottom-end   .fui-menu__panel { inset-inline-end: 0;   top: calc(100% + var(--spacing-sm, 4px)); }
[data-cui-comp="ui-menu"].fui-menu--top-start    .fui-menu__panel { inset-inline-start: 0; bottom: calc(100% + var(--spacing-sm, 4px)); }
[data-cui-comp="ui-menu"].fui-menu--top-end      .fui-menu__panel { inset-inline-end: 0;   bottom: calc(100% + var(--spacing-sm, 4px)); }
@keyframes fui-menu-in {
  from { opacity: 0; transform: translateY(-4px) scale(0.98); }
  to   { opacity: 1; transform: translateY(0)    scale(1);    }
}
[data-cui-comp="ui-menu"] .fui-menu__item {
  display: flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
  width: 100%;
  text-align: start;
  padding: var(--spacing-sm, 4px) var(--spacing-md, 8px);
  background: transparent;
  color: inherit;
  border: 0;
  border-radius: var(--radii-md, 8px);
  cursor: pointer;
  font: inherit;
  font-size: var(--text-sm, 0.875rem);
  text-decoration: none;
  min-height: var(--spacing-touch-target, 44px);
}
[data-cui-comp="ui-menu"] .fui-menu__item:hover,
[data-cui-comp="ui-menu"] .fui-menu__item:focus-visible {
  background: var(--color-surface-soft, #F4F4F5);
  outline: none;
}
[data-cui-comp="ui-menu"] .fui-menu__item--danger { color: var(--color-danger, #DC2626); }
[data-cui-comp="ui-menu"] .fui-menu__item--danger:hover,
[data-cui-comp="ui-menu"] .fui-menu__item--danger:focus-visible {
  background: color-mix(in srgb, var(--color-danger, #DC2626) 10%, transparent);
}
[data-cui-comp="ui-menu"] .fui-menu__item--disabled {
  opacity: var(--opacity-disabled, 0.5);
  cursor: not-allowed;
  pointer-events: none;
}
[data-cui-comp="ui-menu"] .fui-menu__icon { display: inline-flex; width: 1em; justify-content: center; }
[data-cui-comp="ui-menu"] .fui-menu__label { flex: 1; }
[data-cui-comp="ui-menu"] .fui-menu__sep {
  border: 0;
  border-top: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  margin: var(--spacing-xs, 2px) 0;
}
[data-cui-comp="ui-menu"] .fui-menu__form { display: grid; gap: inherit; }
/* Submenus: the nested <details> is one grid child of the parent
   panel; it positions the nested panel, which reuses every panel
   chrome rule above. The position-variant inset rules on the outer
   details would also match the nested panel, so this rule carries a
   higher specificity (element + two classes vs attr + class + class)
   and always wins, at any depth. */
[data-cui-comp="ui-menu"] details.fui-menu__sub { position: relative; display: block; }
[data-cui-comp="ui-menu"] details.fui-menu__sub > summary.fui-menu__item {
  list-style: none;
}
[data-cui-comp="ui-menu"] details.fui-menu__sub > summary.fui-menu__item::-webkit-details-marker { display: none; }
[data-cui-comp="ui-menu"] details.fui-menu__sub > .fui-menu__panel {
  inset-inline-start: 100%;
  top: calc(-1 * var(--spacing-xs, 2px));
}
/* The submenu caret is a pseudo-element so it stays out of the row's
   textContent (type-ahead) and accessible name, unlike the trigger's
   <span> caret. :dir() flips it in RTL; unsupported engines show the
   LTR glyph, a cosmetic-only degradation. */
[data-cui-comp="ui-menu"] .fui-menu__item--hassub::after {
  content: "▸";
  font-size: 0.75em;
  opacity: var(--ui-menu-submenu-caret-opacity, 0.7);
}
:dir(rtl) [data-cui-comp="ui-menu"] .fui-menu__item--hassub::after { content: "◂"; }
/* Radio and checkbox rows: the check indicator is likewise a
   pseudo-element — space is reserved in both states so labels align
   whether checked or not. */
[data-cui-comp="ui-menu"] :is([role="menuitemradio"], [role="menuitemcheckbox"])::before {
  content: "✓";
  display: inline-flex;
  width: 1em;
  flex: none;
  justify-content: center;
  visibility: hidden;
}
[data-cui-comp="ui-menu"] :is([role="menuitemradio"], [role="menuitemcheckbox"])[aria-checked="true"]::before { visibility: visible; }
@media (prefers-reduced-motion: reduce) {
  [data-cui-comp="ui-menu"] .fui-menu__panel { animation: none; }
}`
}
