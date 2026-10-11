package headless

import (
	"encoding/json"
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
	// kernel's data-cui-rpc contract. Use for "Delete this row" items.
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

	// RPCAttrs carries a built RPC's attributes, core-ui/interactive's
	// Action.Attrs(), for a row that needs more of the RPC contract
	// than RPC and Confirm spell: a navigate on success, an error
	// toast. Only data-cui-rpc* and data-cui-confirm keys are taken,
	// data-cui-rpc is required and must be a same-origin path, and any
	// other key is refused at render. Mutually exclusive with Href,
	// RPC, Action, Copy, Radio and Children.
	RPCAttrs map[string]string

	// Copy renders the row as a copy-to-clipboard command over another
	// element's text, the headless-feedback copy contract CopyButton
	// uses. Mutually exclusive with Href, RPC, RPCAttrs, Action, Radio
	// and Children.
	Copy *MenuCopy

	// Children nests a submenu behind this row. The row renders as a
	// disclosure summary; setting Href, RPC, Action or Radio on it is
	// refused at render.
	Children []MenuItem

	// ExtraAttrs forwards additional attributes onto the row. Keys the
	// row owns are dropped, as everywhere in this package.
	ExtraAttrs html.Attrs
}

// MenuCopy is the row's copy shape.
type MenuCopy struct {
	// Target is the id of the element whose text the row copies. A
	// leading "#" is dropped; anything else that reads as a selector
	// is refused at render, since the module resolves an id.
	Target string
	// Toast is the title of the success toast shown on copy. Empty
	// shows none.
	Toast string
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
	// The summary is the trigger; the caret says the activation opens a
	// list. TriggerHTML is the caller's, replacing the summary's whole
	// content — with none, the label and the caret are both the
	// component's own, and the mark goes on the summary itself.
	summaryOwn := Attrs(map[string]string{
		"aria-haspopup": "menu",
		"aria-controls": panelID,
	})
	if p.TriggerHTML == "" {
		summaryOwn = Internal(summaryOwn)
	}
	summary := b.El("summary", PartSummary, summaryOwn, menuTriggerContent(b, p))
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
	hasContent := menuItemsHaveOwnContent(items)
	rows := menuRows(b, items, panelID, hasContent)
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
	if !hasContent {
		// No row in this panel carries a caller icon, so the whole
		// panel is the component's own and the mark sits on it; with
		// any icon present the mark moves to each row that lacks one
		// instead (menuRows), so a caller's icon stays reachable.
		panelAttrs = Internal(panelAttrs)
	}
	panel := b.El("div", PartPanel, panelAttrs, rows)
	if summary != "" {
		return b.El("details", root, own, summary, panel)
	}
	return b.El("details", root, own, panel)
}

// menuItemsHaveOwnContent reports whether any row in items carries a
// caller icon — the only caller content a row can hold. A submenu
// row's own icon counts here for the panel that renders it; the rows
// nested behind it live in a separate panel, checked on its own.
func menuItemsHaveOwnContent(items []MenuItem) bool {
	for _, it := range items {
		if it.Icon != "" {
			return true
		}
	}
	return false
}

// menuRows renders one panel's rows. mark says whether a row lacking
// its own icon must carry the internal mark itself: only needed when
// some sibling in items DOES have an icon, so the panel around them
// cannot be marked as a whole (see menuDetails and menuSubmenu, which
// mark the panel instead when it is false).
func menuRows(b Box, items []MenuItem, parentPanelID string, mark bool) render.HTML {
	var out []render.HTML
	for i, it := range items {
		out = append(out, menuItemEl(b, it, parentPanelID, i, mark))
	}
	if out == nil {
		return ""
	}
	return render.Join(out...)
}

// menuItemEl renders one row (or separator, or submenu).
func menuItemEl(b Box, it MenuItem, parentPanelID string, idx int, mark bool) render.HTML {
	if it.Separator {
		hrOwn := html.Attrs{"role": "separator"}
		if mark {
			hrOwn = Internal(hrOwn)
		}
		return b.El("hr", PartDividerLine, hrOwn)
	}
	if strings.TrimSpace(it.Label) == "" {
		panic("headless: MenuItem requires Label (or Separator: true) — a row of nothing but whitespace is a command nobody can read")
	}
	checkMenuItemCoherence(it)
	if len(it.Children) > 0 {
		return menuSubmenu(b, it, parentPanelID, idx, mark)
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
	// The icon is the caller's; the label text beside it is always the
	// component's own. With no icon at all the row itself (the a/button
	// this function returns, or the form wrapping it below) holds
	// nothing of the caller's, so the mark moves to the row — but only
	// when some sibling row in this panel DOES have an icon, which is
	// exactly when the panel around them cannot carry the mark as a
	// whole (see menuRows/menuDetails/menuSubmenu).
	textOwn, rowMark, hiddenMark := html.Attrs(nil), html.Attrs(nil), html.Attrs(nil)
	if it.Icon != "" {
		icon = b.El("span", PartIcon, Attrs(map[string]string{"aria-hidden": "true"}), it.Icon)
		textOwn = Internal(nil)
		if mark {
			hiddenMark = Internal(nil)
		}
	} else if mark {
		rowMark = Internal(nil)
	}
	kids := []render.HTML{icon, b.El("span", PartText, textOwn, render.Text(scrubControlBytes(it.Label)))}

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
			hidden = append(hidden, render.VoidTag("input", Merge(html.Attrs{
				"type":  "hidden",
				"name":  k,
				"value": it.Action.Fields[k],
			}, hiddenMark)))
		}
		rowOwn := own
		if it.Confirm != "" {
			// data-cui-confirm rides the submit control: the runtime
			// honours it on any form submit, so a destructive form row
			// asks before it POSTs.
			rowOwn = Merge(own, Attrs(map[string]string{"data-cui-confirm": it.Confirm}))
		}
		// role="none": a form is not one of a menu's required owned
		// elements (menuitem, menuitemcheckbox, menuitemradio, group,
		// separator) — the presentation role takes it out of the AX
		// tree so the row's menuitem is what a menu parent is judged
		// against. The mark sits on the form (the row's own topmost
		// element) rather than the button, so the hidden inputs beside
		// it are covered too.
		return b.El("form", PartMenuForm, Merge(Attrs(map[string]string{
			"role":   "none",
			"method": method,
			"action": action,
		}), rowMark), append(hidden,
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
		return b.El("a", PartMenuItem, Merge(own, rowMark), kids...)
	}

	if it.RPC != "" {
		method := it.RPCMethod
		if method == "" {
			method = "POST"
		}
		own["data-cui-rpc"] = it.RPC
		own["data-cui-rpc-method"] = method
		if it.Confirm != "" {
			own["data-cui-confirm"] = it.Confirm
		}
	}
	// checkMenuItemCoherence has refused every key but the RPC
	// contract's.
	for k, v := range it.RPCAttrs {
		own[k] = v
	}
	if it.Copy != nil {
		// The row is the copy module's wrapper: the module resolves the
		// clicked element's closest [data-hui-copy] and reads the
		// target and toast from it.
		own["data-hui-copy"] = ""
		own["data-hui-copy-target"] = strings.TrimPrefix(it.Copy.Target, "#")
		if it.Copy.Toast != "" {
			b, _ := json.Marshal(map[string]any{"variant": "success", "title": it.Copy.Toast, "ttl": 3000})
			own["data-hui-copy-toast"] = string(b)
		}
	}
	if it.Disabled {
		// Presence, not value: the button element's own spelling.
		Mark(own, "disabled")
	}
	return b.El("button", PartMenuItem, Merge(Merge(own, rowMark), Attrs(map[string]string{"type": "button"})), kids...)
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
// mark says whether the summary must carry the internal mark itself
// when it has no icon (see menuRows); the nested panel behind it is a
// separate subtree with its own such decision, made here.
func menuSubmenu(b Box, it MenuItem, parentPanelID string, idx int, mark bool) render.HTML {
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
	// The row's own icon is caller content; the label beside it is
	// always the component's own. With no icon, the summary row holds
	// nothing of the caller's, so the mark moves to it — but only when
	// mark says some sibling row DOES have one (see menuRows). The
	// nested panel behind the summary is a separate subtree and makes
	// its own such decision below.
	textOwn := html.Attrs(nil)
	if it.Icon != "" {
		textOwn = Internal(nil)
	} else if mark {
		rowAttrs = Internal(rowAttrs)
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
			b.El("span", PartText, textOwn, render.Text(scrubControlBytes(it.Label)))),
		func() render.HTML {
			subAttrs := Attrs(map[string]string{
				"id":   subPanelID,
				"role": "menu",
			})
			Mark(subAttrs, "data-hui-menu-panel")
			childHasContent := menuItemsHaveOwnContent(it.Children)
			if !mark {
				// This whole row's enclosing panel is already marked as
				// a whole (mark is false), which — since that decision
				// already recursed into every descendant — covers this
				// nested panel too; marking it again would be marking
				// inside an already-marked subtree.
				return b.El("div", PartPanel, subAttrs, menuRows(b, it.Children, subPanelID, false))
			}
			if !childHasContent {
				subAttrs = Internal(subAttrs)
			}
			return b.El("div", PartPanel, subAttrs, menuRows(b, it.Children, subPanelID, childHasContent))
		}(),
	)
}

// checkMenuItemCoherence refuses the caller-incoherent combinations.
func checkMenuItemCoherence(it MenuItem) {
	checkMenuRowShape(it)
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

// checkMenuRowShape refuses an RPCAttrs or Copy row that also names
// another way to act, and the values neither may carry.
func checkMenuRowShape(it MenuItem) {
	if it.RPCAttrs != nil {
		if it.Href != "" || it.RPC != "" || it.Action != nil || it.Copy != nil || it.Radio != "" || len(it.Children) > 0 {
			panic("headless: MenuItem with RPCAttrs cannot also set Href, RPC, Action, Copy, Radio or Children — the row is one RPC")
		}
		path := it.RPCAttrs["data-cui-rpc"]
		if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
			panic("headless: MenuItem RPCAttrs needs data-cui-rpc set to a same-origin path, got " + strconv.Quote(path))
		}
		for k := range it.RPCAttrs {
			if k != "data-cui-confirm" && !strings.HasPrefix(k, "data-cui-rpc") {
				panic("headless: MenuItem RPCAttrs takes only data-cui-rpc* and data-cui-confirm keys, got " + strconv.Quote(k))
			}
		}
	}
	if it.Copy != nil {
		if it.Href != "" || it.RPC != "" || it.Action != nil || it.Radio != "" || len(it.Children) > 0 {
			panic("headless: MenuItem with Copy cannot also set Href, RPC, Action, Radio or Children — the row is one copy command")
		}
		id := strings.TrimPrefix(it.Copy.Target, "#")
		if id == "" || strings.ContainsAny(id, " \t\n\r\f.>[:#") {
			panic("headless: MenuCopy Target must be an element id (a leading # is allowed), not a CSS selector: " + strconv.Quote(it.Copy.Target))
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
