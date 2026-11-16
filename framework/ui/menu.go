package ui

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// MenuPosition is the trigger corner the panel anchors to; empty is MenuBottomStart.
type MenuPosition string

const (
	MenuBottomStart MenuPosition = "bottom-start"
	MenuBottomEnd   MenuPosition = "bottom-end"
	MenuTopStart    MenuPosition = "top-start"
	MenuTopEnd      MenuPosition = "top-end"
)

// MenuItem is one row in a Menu; Href wins over RPC, and the other row kinds panic when mixed.
type MenuItem struct {
	// Label is the row's text. Required unless Separator.
	Label string
	// Href makes the row a link.
	Href string
	// RPC and RPCMethod make the row call a server handler.
	RPC, RPCMethod string
	// Confirm asks before the RPC fires; inert without RPC.
	Confirm string
	// Icon is drawn before Label.
	Icon render.HTML
	// Danger tints a destructive row.
	Danger bool
	// Disabled greys the row and takes it out of keyboard navigation.
	Disabled bool
	// Separator renders a divider; every other field is ignored.
	Separator bool
	// ID is the row's id; the caller keeps it unique.
	ID string
	// Radio makes the row a menuitemradio in the menu-wide group of that name.
	Radio string
	// Check makes the row a menuitemcheckbox whose state is the server's.
	Check bool
	// Checked sets aria-checked on a Radio or Check row.
	Checked bool
	// Action makes the row a form submission (see MenuAction).
	Action *MenuAction
	// Children nest a submenu behind the row, which is then only a disclosure.
	Children []MenuItem
	// Do makes the row a button carrying a built RPC with its effects and toasts.
	Do *interactive.Action
	// Copy makes the row copy another element's text, as CopyButton does.
	Copy *MenuCopy
	// Class appends to the row's class list.
	Class string
	// ExtraAttrs land on the row; class, id, data-cui-* and the menuitem contract are dropped.
	ExtraAttrs map[string]string
}

// MenuCopy is MenuItem's copy row: the Target element's id and an optional Toast title.
type MenuCopy = headless.MenuCopy

// MenuAction is MenuItem's form row; with no Fields it panics unless Unsafe, CSRF being the caller's.
type MenuAction struct {
	// Path is the form's action URL.
	Path string
	// Method defaults to POST.
	Method string
	// Fields are the hidden inputs, the CSRF token first.
	Fields map[string]string
	// Unsafe accepts an Action with no Fields.
	Unsafe bool
}

// MenuConfig describes a dropdown menu.
type MenuConfig struct {
	// ID pairs the trigger with the panel; empty derives one.
	ID string
	// Label is the trigger's text, or its accessible name.
	Label string
	// TriggerHTML replaces Label with inline HTML.
	TriggerHTML render.HTML
	// Icon names a registered icon drawn before Label.
	Icon string
	// IconOnly draws the trigger as the "more" glyph named by Label, unless another trigger is set.
	IconOnly bool
	// Avatar draws the trigger as that avatar alone, named by Label.
	Avatar *AvatarConfig
	// TriggerElement is the caller's own button the runtime makes the controller; it never navigates.
	TriggerElement render.HTML
	// Items are the rows. Required.
	Items []MenuItem
	// Position anchors the panel relative to the trigger.
	Position MenuPosition
	// LazyPanel keeps the rows out of the live DOM until the first open (empty without script).
	LazyPanel bool
	// TriggerClass and PanelClass append to those elements' classes.
	TriggerClass string
	PanelClass   string
	// ExtraAttrs land on the root; class, id and the menu wiring are dropped.
	ExtraAttrs html.Attrs
}

var menuStyle = registry.RegisterStyle("ui-menu", menuCSS)

// Menu renders a trigger and its role=menu panel through headless.Menu.
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
	if cfg.Icon != "" {
		if trigger != "" || cfg.TriggerElement != "" || cfg.IconOnly || cfg.Avatar != nil {
			panic("ui: Menu Icon dresses the Label trigger; drop TriggerHTML, TriggerElement, IconOnly and Avatar")
		}
		if !IconRegistered(cfg.Icon) {
			panic("ui: Menu Icon " + strconv.Quote(cfg.Icon) + " is not a registered icon")
		}
		trigger = headless.Own(render.Join(
			Icon(cfg.Icon, IconConfig{Size: "16"}),
			html.Span(html.TextConfig{}, render.Text(cfg.Label)),
		))
	}
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
  color: var(--color-text, #09090B);
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
/* Icon: the glyph before the label, quieter than the text. */
[data-cui-comp="ui-menu"] > summary.fui-menu__trigger > svg.fui-icon {
  flex: none;
  color: var(--color-text-muted, #52525B);
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
  color: var(--color-text, #09090B);
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
  /* headless-disclosure shifts a panel that would leave the viewport. */
  translate: var(--hui-panel-shift, 0);
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
[data-cui-comp="ui-menu"] .fui-menu__item--danger { color: var(--color-danger, #B91C1C); }
[data-cui-comp="ui-menu"] .fui-menu__item--danger:hover,
[data-cui-comp="ui-menu"] .fui-menu__item--danger:focus-visible {
  background: color-mix(in srgb, var(--color-danger, #B91C1C) 10%, transparent);
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
