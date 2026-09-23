package headless

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The menu: a disclosure whose panel is a list of commands. The
// no-script contract is the details element — the summary opens the
// panel, every row is a real link, button or form submit — and the
// keyboard contract a sighted reader gets from a native list, a
// screen reader user gets from the roles and the roving tabindex the
// rows carry here. headless-menu binds the keyboard (arrows wrap,
// Home and End jump, type-ahead matches, submenus open and close
// RTL-aware, Escape walks back one level, Tab closes the chain);
// headless-disclosure, which it requires, holds the disclosure half
// (the aria mirror, Escape, the close-on-navigate).

// Menu parts. The summary is the trigger on the summary path; the
// trigger-element path renders a wrapper (menu-trigger) beside a
// summary-less details (menu-toggle) that the module pairs by id.
const (
	PartMenuCaret   Part = "menu-caret"
	PartMenuItem    Part = "menu-item"
	PartMenuSubmenu Part = "menu-submenu"
	PartMenuTrigger Part = "menu-trigger"
	PartMenuToggle  Part = "menu-toggle"
	PartMenuForm    Part = "menu-form"
)

// MenuItem is one row: an actionable item (Label required, Href / RPC
// / Action as supplied) or a separator. The framework owns the role
// attributes; callers only describe semantics.
type MenuItem struct {
	// Label is the row's visible text. Required unless Separator.
	Label string

	// Href turns the row into an <a> link. Mutually exclusive with the
	// custom action attrs; if both are supplied, Href wins.
	Href string

	// RPC and RPCMethod wire the row to a server-side handler via the
	// kernel's data-fui-rpc contract. Use for "Delete this row" items.
	RPC, RPCMethod string

	// Confirm asks the reader to confirm before the RPC fires; the
	// runtime honours it on RPC dispatch and on any form submit.
	Confirm string

	// Icon is rendered before the Label. Inline HTML.
	Icon render.HTML

	// Danger tints destructive rows; a visual hint only.
	Danger bool

	// Disabled removes the row from keyboard navigation and pointer
	// interaction while keeping it in the panel.
	Disabled bool

	// Separator renders a horizontal divider instead of a row.
	Separator bool

	// ID becomes the rendered row's id attribute. Uniqueness is
	// caller-owned, like any HTML id — the menu refuses the duplicates
	// it can see.
	ID string

	// Radio, when non-empty, renders the row as a radio option:
	// role="menuitemradio" plus aria-checked (see Checked) and the
	// group attr the module arbitrates client-side. Exactly one row of
	// a group should carry Checked. Mutually exclusive with Children —
	// both set is refused at render.
	Radio string

	// Checked sets aria-checked on a Radio row.
	Checked bool

	// Action renders the row as a form submission instead of a link or
	// RPC, for hosts whose command rows hit PRG endpoints. Nil (the
	// zero value) renders nothing. Mutually exclusive with Href, RPC,
	// Radio and Children: incoherent combos are refused at render.
	Action *MenuAction

	// Children nests a submenu behind this row. The row renders as a
	// disclosure summary; setting Href, RPC, Action or Radio on it is
	// refused at render.
	Children []MenuItem

	// ExtraAttrs forwards additional attributes onto the row. Keys the
	// row owns are dropped, as everywhere in this package.
	ExtraAttrs html.Attrs
}

// MenuAction is the row's form-POST shape.
//
// CSRF contract: the framework never mints or verifies tokens — the
// caller's form middleware owns that. But a state-changing POST with
// no hidden inputs is almost certainly a forgotten token, so the
// default refuses it: an Action with no Fields panics unless Unsafe
// explicitly acknowledges the endpoint carries its own protection.
type MenuAction struct {
	// Path is the form's action URL.
	Path string
	// Method defaults to POST.
	Method string
	// Fields are the hidden inputs, first use: the CSRF token.
	Fields map[string]string
	// Unsafe acknowledges the no-Fields case deliberately.
	Unsafe bool
}

// MenuProps configures a dropdown menu.
type MenuProps struct {
	// ID pairs the trigger with the panel. Empty derives a stable id
	// from the menu's content.
	ID string
	// Label is the trigger's visible text. Mutually exclusive with
	// TriggerHTML and TriggerElement.
	Label string
	// TriggerHTML overrides Label with custom inline HTML.
	TriggerHTML render.HTML
	// TriggerElement replaces the framework summary with a
	// caller-owned interactive element: the menu renders a
	// summary-less disclosure beside it and the module makes the
	// element the controller. Use for host-styled triggers — an
	// interactive element inside a summary is axe nested-interactive.
	TriggerElement render.HTML
	// Items is the menu's contents. Required; an empty menu panics —
	// it signals a bug, not a runtime state.
	Items []MenuItem
	// Position names the panel's anchoring variant (bottom-start, …);
	// the class map resolves it. Empty takes bottom-start.
	Position string
	// LazyPanel ships the panel's rows inside an inert template the
	// module mounts on first open, for page-scoped consumers that must
	// not see closed-menu rows in the live DOM.
	LazyPanel bool

	ExtraAttrs html.Attrs

	// Parts: attrs on the root. Rows own their guarantees.
	Parts Parts
}

// Menu renders the dropdown.
func Menu(p MenuProps, s Classes) render.HTML {
	if p.TriggerElement == "" && p.TriggerHTML == "" {
		checkLabel("Menu", "Label", p.Label)
	}
	if len(p.Items) == 0 {
		panic("headless: Menu requires at least one item — an empty menu is a bug, not a runtime state")
	}
	pos := p.Position
	if pos == "" {
		pos = "bottom-start"
	}
	id := p.ID
	if id == "" {
		hashIn := p.Label
		if p.TriggerElement != "" {
			hashIn += string(p.TriggerElement)
		}
		id = "hui-menu-" + menuShortHash(hashIn+menuPositionsForHash(p.Items))
	}
	panelID := id + "-panel"
	menuItemIDs(p.Items, &[]string{})

	b := p.Parts.Box(s)
	rootAttrs := Merge(Safe(p.ExtraAttrs), nil)
	if v := s.Variant(PartRoot, pos); v != "" {
		rootAttrs["class"] = joinClasses(rootAttrs["class"], v)
	}

	if p.TriggerElement != "" {
		// Caller-owned trigger: no <summary> (an interactive element
		// inside one is axe nested-interactive). The wrapper carries
		// the pairing hook — attributes cannot be injected into raw
		// caller HTML server-side — beside the summary-less details
		// the module resolves it against.
		return b.El("div", PartRoot, rootAttrs,
			b.El("div", PartMenuTrigger, Attrs(map[string]string{
				"data-hui-menu-trigger": id,
				"role":                  "presentation",
			}), p.TriggerElement),
			menuDetails(b, id, panelID, p.Items, p.LazyPanel, PartMenuToggle, "", html.Attrs{}),
		)
	}
	// The summary is the trigger; the caret says the activation opens
	// a list.
	summary := b.El("summary", PartSummary, Attrs(map[string]string{
		"aria-haspopup": "menu",
		"aria-controls": panelID,
	}), menuTriggerContent(b, p))
	return menuDetails(b, id, panelID, p.Items, p.LazyPanel, PartRoot, summary, rootAttrs)
}

// menuTriggerContent builds the summary's inner content: the caller's
// HTML, or the label and a caret glyph.
func menuTriggerContent(b Box, p MenuProps) render.HTML {
	if p.TriggerHTML != "" {
		return p.TriggerHTML
	}
	return render.Join(
		render.Text(p.Label),
		b.El("span", PartMenuCaret, Attrs(map[string]string{"aria-hidden": "true"}), render.Text("▾")),
	)
}

// menuDetails builds the disclosure that carries the panel. summary is
// nil on the trigger-element path; rootAttrs carry the caller's extras
// and the position variant the root wears.
func menuDetails(b Box, id, panelID string, items []MenuItem, lazy bool, root Part, summary render.HTML, rootAttrs html.Attrs) render.HTML {
	// rootAttrs arrives already sanitised (Menu ran Safe) with the
	// position variant folded into its class; re-running Safe here
	// would drop the class the variant lives in.
	own := Merge(rootAttrs, html.Attrs{
		"data-hui-disclosure": "",
		"data-hui-menu":       id,
	})
	rows := menuRows(b, items, panelID)
	if lazy {
		tplAttrs := html.Attrs{}
		Mark(tplAttrs, "data-hui-menu-lazy")
		rows = render.Tag("template", tplAttrs, rows)
	}
	panelAttrs := Attrs(map[string]string{
		"id":   panelID,
		"role": "menu",
	})
	Mark(panelAttrs, "data-hui-menu-panel")
	panel := b.El("div", PartPanel, panelAttrs, rows)
	if summary != "" {
		return b.El("details", root, own, summary, panel)
	}
	return b.El("details", root, own, panel)
}

// menuRows renders one panel's rows.
func menuRows(b Box, items []MenuItem, parentPanelID string) render.HTML {
	var out []render.HTML
	for i, it := range items {
		out = append(out, menuItemEl(b, it, parentPanelID, i))
	}
	if out == nil {
		return ""
	}
	return render.Join(out...)
}

// menuItemEl renders one row (or separator, or submenu).
func menuItemEl(b Box, it MenuItem, parentPanelID string, idx int) render.HTML {
	if it.Separator {
		return b.El("hr", PartDividerLine, Attrs(map[string]string{"role": "separator"}))
	}
	if strings.TrimSpace(it.Label) == "" {
		panic("headless: MenuItem requires Label (or Separator: true) — a row of nothing but whitespace is a command nobody can read")
	}
	checkMenuItemCoherence(it)
	if len(it.Children) > 0 {
		return menuSubmenu(b, it, parentPanelID, idx)
	}

	own := Safe(it.ExtraAttrs, "type", "href", "tabindex", "role",
		"aria-disabled", "disabled", "aria-checked")
	if it.Danger {
		if v := b.Classes.Variant(PartMenuItem, "danger"); v != "" {
			own["class"] = joinClasses(own["class"], v)
		}
	}
	if it.Disabled {
		if v := b.Classes.Variant(PartMenuItem, "disabled"); v != "" {
			own["class"] = joinClasses(own["class"], v)
		}
	}
	if it.ID != "" {
		own["id"] = it.ID
	}
	// The roving tabindex is the module's to manage; every row starts
	// out of the tab order.
	own["tabindex"] = "-1"
	own["role"] = menuRowRole(it)
	if it.Radio != "" {
		checked := "false"
		if it.Checked {
			checked = "true"
		}
		own["aria-checked"] = checked
		own["data-hui-menu-radio"] = it.Radio
	}
	if it.Disabled {
		own["aria-disabled"] = "true"
	}
	var icon render.HTML
	if it.Icon != "" {
		icon = b.El("span", PartIcon, Attrs(map[string]string{"aria-hidden": "true"}), it.Icon)
	}
	kids := []render.HTML{icon, b.El("span", PartText, nil, render.Text(scrubControlBytes(it.Label)))}

	if it.Action != nil {
		method := it.Action.Method
		if method == "" {
			method = "POST"
		}
		// The same anchor policy as every form-action sink: a rejected
		// path degrades to the inert "#".
		action := urlsafe.CleanAnchor(it.Action.Path)
		if action == "" {
			action = "#"
		}
		var hidden []render.HTML
		for _, k := range slices.Sorted(maps.Keys(it.Action.Fields)) {
			hidden = append(hidden, render.VoidTag("input", html.Attrs{
				"type":  "hidden",
				"name":  k,
				"value": it.Action.Fields[k],
			}))
		}
		rowOwn := own
		if it.Confirm != "" {
			// data-fui-confirm rides the submit control: the runtime
			// honours it on any form submit, so a destructive form row
			// asks before it POSTs.
			rowOwn = Merge(own, Attrs(map[string]string{"data-fui-confirm": it.Confirm}))
		}
		// role="none": a form is not one of a menu's required owned
		// elements (menuitem, menuitemcheckbox, menuitemradio, group,
		// separator) — the presentation role takes it out of the AX
		// tree so the row's menuitem is what a menu parent is judged
		// against.
		return b.El("form", PartMenuForm, Attrs(map[string]string{
			"role":   "none",
			"method": method,
			"action": action,
		}), append(hidden,
			b.El("button", PartMenuItem, Merge(rowOwn, Attrs(map[string]string{"type": "submit"})), kids...))...)
	}

	if it.Href != "" {
		// safeURL drops javascript:, data:, and friends; a rejected
		// href degrades to "#" like every link this framework renders.
		href := urlsafe.CleanAnchor(it.Href)
		if href == "" {
			href = "#"
		}
		own["href"] = href
		return b.El("a", PartMenuItem, own, kids...)
	}

	if it.RPC != "" {
		method := it.RPCMethod
		if method == "" {
			method = "POST"
		}
		own["data-fui-rpc"] = it.RPC
		own["data-fui-rpc-method"] = method
		if it.Confirm != "" {
			own["data-fui-confirm"] = it.Confirm
		}
	}
	if it.Disabled {
		// Presence, not value: the button element's own spelling.
		Mark(own, "disabled")
	}
	return b.El("button", PartMenuItem, Merge(own, Attrs(map[string]string{"type": "button"})), kids...)
}

// menuRowRole names the row's ARIA role.
func menuRowRole(it MenuItem) string {
	if it.Radio != "" {
		return "menuitemradio"
	}
	return "menuitem"
}

// menuSubmenu renders a parent row plus its nested panel. The wrapper
// is a disclosure — the exact machinery the top level uses — so
// Escape, the aria mirror and the module's keyboard handling apply at
// depth without a second mechanism. The summary IS the parent row.
func menuSubmenu(b Box, it MenuItem, parentPanelID string, idx int) render.HTML {
	subID := parentPanelID + "-sub-" + strconv.Itoa(idx)
	subPanelID := subID + "-panel"
	rowAttrs := Merge(Safe(it.ExtraAttrs, "type", "href", "tabindex", "role",
		"aria-disabled", "disabled", "aria-haspopup", "aria-controls", "aria-checked"), Attrs(map[string]string{
		"aria-haspopup": "menu",
		"aria-controls": subPanelID,
		"role":          "menuitem",
		"tabindex":      "-1",
	}))
	for _, variant := range []struct {
		on bool
		v  string
	}{{true, "hassub"}, {it.Danger, "danger"}, {it.Disabled, "disabled"}} {
		if variant.on {
			if v := b.Classes.Variant(PartMenuItem, variant.v); v != "" {
				rowAttrs["class"] = joinClasses(rowAttrs["class"], v)
			}
		}
	}
	if it.ID != "" {
		rowAttrs["id"] = it.ID
	}
	if it.Disabled {
		// <summary> has no native disabled attribute; aria + CSS
		// pointer-events carry the state.
		rowAttrs["aria-disabled"] = "true"
	}
	return b.El("details", PartMenuSubmenu, html.Attrs{
		"data-hui-disclosure": "",
		"data-hui-menu":       subID,
	},
		// The submenu caret is a CSS pseudo-element, not a span, so the
		// row's textContent stays the label alone — type-ahead matches
		// it. The summary wears the row part so a class map styles it
		// like any other row (plus the hassub variant).
		b.El("summary", PartMenuItem, rowAttrs,
			func() render.HTML {
				if it.Icon == "" {
					return ""
				}
				return b.El("span", PartIcon, Attrs(map[string]string{"aria-hidden": "true"}), it.Icon)
			}(),
			b.El("span", PartText, nil, render.Text(scrubControlBytes(it.Label)))),
		func() render.HTML {
			subAttrs := Attrs(map[string]string{
				"id":   subPanelID,
				"role": "menu",
			})
			Mark(subAttrs, "data-hui-menu-panel")
			return b.El("div", PartPanel, subAttrs, menuRows(b, it.Children, subPanelID))
		}(),
	)
}

// checkMenuItemCoherence refuses the caller-incoherent combinations.
func checkMenuItemCoherence(it MenuItem) {
	if len(it.Children) > 0 {
		if it.Radio != "" {
			panic("headless: MenuItem with Radio cannot have Children — a radio row is a leaf command, a submenu parent is a disclosure")
		}
		if it.Href != "" || it.RPC != "" || it.Action != nil {
			panic("headless: MenuItem with Children cannot also set Href, RPC, or Action — a submenu parent is purely a disclosure")
		}
		return
	}
	if it.Action != nil {
		if it.Href != "" || it.RPC != "" || it.Radio != "" {
			panic("headless: MenuItem with Action cannot also set Href, RPC, or Radio — a form row is a leaf command")
		}
		if len(it.Action.Fields) == 0 && !it.Action.Unsafe {
			panic("headless: MenuAction has no Fields (no CSRF token?) — pass the token in Fields or set Unsafe: true to acknowledge the endpoint protects itself")
		}
	}
}

// menuItemIDs walks the tree, refusing duplicate row ids inside one
// menu (every aria-controls and page anchor would point at the first).
func menuItemIDs(items []MenuItem, seen *[]string) {
	for _, it := range items {
		if it.Separator || it.ID == "" {
			continue
		}
		for _, s := range *seen {
			if s == it.ID {
				panic("headless: Menu renders the row id " + it.ID + " twice — every reference to it now points at the first one")
			}
		}
		*seen = append(*seen, it.ID)
		menuItemIDs(it.Children, seen)
	}
}

// menuShortHash is a small FNV-style stable hash used to derive the
// fallback id when the caller supplies none. Two structurally
// identical menus share it; callers put distinct IDs on menus that
// must not toggle each other.
func menuShortHash(s string) string {
	var h uint32 = 2166136261
	for i := range len(s) {
		h ^= uint32(s[i])
		h *= 16777619
	}
	const digits = "0123456789abcdef"
	out := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		out[i] = digits[h&0xF]
		h >>= 4
	}
	return string(out)
}

// menuPositionsForHash folds every row label (recursing into submenus)
// into the hash input, so two menus differing only in their rows
// cannot share the fallback id.
func menuPositionsForHash(items []MenuItem) string {
	var b strings.Builder
	for _, it := range items {
		b.WriteString(it.Label)
		b.WriteString("|")
		if len(it.Children) > 0 {
			b.WriteString(menuPositionsForHash(it.Children))
		}
	}
	return b.String()
}

func init() {
	Register(Spec{
		Name: "Menu",
		Anatomy: []Part{PartRoot, PartSummary, PartMenuCaret, PartPanel, PartMenuItem,
			PartIcon, PartText, PartDividerLine, PartMenuSubmenu, PartMenuTrigger, PartMenuToggle, PartMenuForm},
		Hooks: []string{"data-hui-menu", "data-hui-menu-trigger", "data-hui-menu-panel",
			"data-hui-menu-radio", "data-hui-menu-lazy", "data-hui-disclosure"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Menu(MenuProps{Label: "Options", Items: []MenuItem{
				{Label: "Profile", Href: "/profile"},
				{Label: "Preferences"},
			}, Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "a summary menu",
				Why:  "the summary is a real controller and every row is a real command with no script; the module adds the keyboard contract on top",
				HTML: Menu(MenuProps{Label: "Account", Items: []MenuItem{
					{Label: "Profile", Href: "/me"},
					{Separator: true},
					{Label: "Delete", RPC: "/api/del", RPCMethod: "DELETE", Danger: true},
				}}, s),
			}, {
				Name: "a submenu",
				Why:  "the nested panel is the same disclosure machinery at depth, so Escape walks back one level and the parent row is the controller",
				HTML: Menu(MenuProps{Label: "Theme", Items: []MenuItem{
					{Label: "Palette", Children: []MenuItem{
						{Label: "Light", Radio: "theme"},
						{Label: "Dark", Radio: "theme", Checked: true},
					}},
					{Label: "Reset"},
				}}, s),
			}, {
				Name: "a caller-owned trigger",
				Why:  "the caller's element sits beside a summary-less disclosure it controls — an interactive element inside a summary is axe nested-interactive",
				HTML: Menu(MenuProps{TriggerElement: Button(ButtonProps{Label: "Open user menu"}, k.For("Button")), Items: []MenuItem{
					{Label: "Profile", Href: "/me"},
				}}, s),
			}, {
				Name: "a disabled row and a separator",
				Why:  "the disabled row keeps its place in the panel and leaves the keyboard rotation; the separator divides groups without joining them",
				HTML: Menu(MenuProps{Label: "View", Items: []MenuItem{
					{Label: "Compact"},
					{Separator: true},
					{Label: "Experimental", Disabled: true},
				}}, s),
			}, {
				Name: "rows with an icon and a form action, lazy-mounted",
				Why:  "an icon row and a form-POST row are commands like any other, and a lazy panel's rows travel inside a template the module mounts on first open — page-scoped queries cannot see them while the menu is closed",
				HTML: Menu(MenuProps{Label: "Account", LazyPanel: true, Items: []MenuItem{
					{Label: "Open workspace", Href: "/w", Icon: SpecimenGlyph},
					{Separator: true},
					{Label: "Stop impersonation", Action: &MenuAction{
						Path:   "/admin/stop-impersonation",
						Fields: map[string]string{"csrf": "tok"},
					}},
				}}, s),
			}}
		},
	})
}
