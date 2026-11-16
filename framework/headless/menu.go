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

const (
	PartMenuCaret   Part = "menu-caret"
	PartMenuItem    Part = "menu-item"
	PartMenuSubmenu Part = "menu-submenu"
	PartMenuTrigger Part = "menu-trigger"
	PartMenuToggle  Part = "menu-toggle"
	PartMenuForm    Part = "menu-form"
)

// MenuItem is one row: a link, an RPC, a form action, a copy command, an option, a submenu parent or a separator.
type MenuItem struct {
	// Label is the row's visible text. Required unless Separator.
	Label string

	// Href makes the row a link.
	Href string

	// RPC and RPCMethod (default POST) wire the row to a same-origin data-cui-rpc handler.
	RPC, RPCMethod string

	// Confirm asks the reader to confirm before the RPC or the form submit.
	Confirm string

	// Icon renders before the Label, aria-hidden.
	Icon render.HTML

	// Danger tints destructive rows; a visual hint only.
	Danger bool

	// Disabled keeps the row in the panel and makes it inert: no href, no submit.
	Disabled bool

	// Separator renders a horizontal divider instead of a row.
	Separator bool

	// ID is the row's id; a duplicate inside one menu is refused.
	ID string

	// Radio names the radio group the row is an option of.
	Radio string

	// Check renders the row as a server-owned checkbox option.
	Check bool

	// Checked sets aria-checked on a Radio or Check row.
	Checked bool

	// Action renders the row as a form submission.
	Action *MenuAction

	// RPCAttrs carries a built RPC's data-cui-rpc* and data-cui-confirm* attributes.
	RPCAttrs map[string]string

	// Copy renders the row as a copy-to-clipboard command.
	Copy *MenuCopy

	// Children nest a submenu behind the row.
	Children []MenuItem

	// ExtraAttrs are added to the row; keys the row owns are dropped.
	ExtraAttrs html.Attrs
}

// MenuCopy is the row's copy shape.
type MenuCopy struct {
	// Target is the id of the element whose text the row copies; a leading # is dropped.
	Target string
	// Toast is the success toast's title; empty shows none.
	Toast string
}

// MenuAction is the row's form shape; with no Fields it panics unless Unsafe.
type MenuAction struct {
	// Path is the form's action URL.
	Path string
	// Method defaults to POST.
	Method string
	// Fields are the hidden inputs, the CSRF token first.
	Fields map[string]string
	// Unsafe acknowledges an endpoint that protects itself without Fields.
	Unsafe bool
}

// MenuProps configures a dropdown menu.
type MenuProps struct {
	// ID pairs the trigger with the panel; empty derives one from the menu's content.
	ID string
	// Label is the trigger's visible text.
	Label string
	// TriggerHTML overrides Label with custom inline HTML.
	TriggerHTML render.HTML
	// TriggerElement replaces the summary with a caller-owned element that controls the menu.
	TriggerElement render.HTML
	// Items are the rows. Required.
	Items []MenuItem
	// Position names the panel's anchoring variant; empty is bottom-start.
	Position string
	// LazyPanel ships the rows in an inert template the module mounts on first open.
	LazyPanel bool

	ExtraAttrs html.Attrs

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
		return b.El("div", PartRoot, rootAttrs,
			b.El("div", PartMenuTrigger, Attrs(map[string]string{
				"data-hui-menu-trigger": id,
				"role":                  "presentation",
			}), p.TriggerElement),
			menuDetails(b, id, panelID, p.Items, p.LazyPanel, PartMenuToggle, "", html.Attrs{}),
		)
	}
	summaryOwn := Attrs(map[string]string{
		"aria-haspopup": "menu",
		"aria-controls": panelID,
	})
	if p.TriggerHTML == "" || ownedSlot(p.TriggerHTML) {
		summaryOwn = Internal(summaryOwn)
	}
	summary := b.El("summary", PartSummary, summaryOwn, menuTriggerContent(b, p))
	return menuDetails(b, id, panelID, p.Items, p.LazyPanel, PartRoot, summary, rootAttrs)
}

func menuTriggerContent(b Box, p MenuProps) render.HTML {
	if p.TriggerHTML != "" {
		return p.TriggerHTML
	}
	return render.Join(
		render.Text(p.Label),
		b.El("span", PartMenuCaret, Attrs(map[string]string{"aria-hidden": "true"}), render.Text("▾")),
	)
}

func menuDetails(b Box, id, panelID string, items []MenuItem, lazy bool, root Part, summary render.HTML, rootAttrs html.Attrs) render.HTML {
	own := Merge(rootAttrs, html.Attrs{
		"data-hui-disclosure":         "",
		"data-hui-disclosure-dismiss": "",
		"data-hui-menu":               id,
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
		panelAttrs = Internal(panelAttrs)
	}
	panel := b.El("div", PartPanel, panelAttrs, rows)
	if summary != "" {
		return b.El("details", root, own, summary, panel)
	}
	return b.El("details", root, own, panel)
}

func menuItemsHaveOwnContent(items []MenuItem) bool {
	for _, it := range items {
		if it.Icon != "" {
			return true
		}
	}
	return false
}

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
	own["tabindex"] = "-1"
	own["role"] = menuRowRole(it)
	if it.Radio != "" || it.Check {
		checked := "false"
		if it.Checked {
			checked = "true"
		}
		own["aria-checked"] = checked
	}
	if it.Radio != "" {
		own["data-hui-menu-radio"] = it.Radio
	}
	if it.Disabled {
		own["aria-disabled"] = "true"
	}
	var icon render.HTML
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
			rowOwn = Merge(own, Attrs(map[string]string{"data-cui-confirm": it.Confirm}))
		}
		if it.Disabled {
			rowOwn = Mark(Merge(rowOwn, nil), "disabled")
		}
		return b.El("form", PartMenuForm, Merge(Attrs(map[string]string{
			"role":   "none",
			"method": method,
			"action": action,
		}), rowMark), append(hidden,
			b.El("button", PartMenuItem, Merge(rowOwn, Attrs(map[string]string{"type": "submit"})), kids...))...)
	}

	if it.Href != "" {
		if it.Disabled {
			return b.El("a", PartMenuItem, Merge(own, rowMark), kids...)
		}
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
	for k, v := range it.RPCAttrs {
		own[k] = v
	}
	if it.Copy != nil {
		own["data-hui-copy"] = ""
		own["data-hui-copy-target"] = strings.TrimPrefix(it.Copy.Target, "#")
		if it.Copy.Toast != "" {
			b, _ := json.Marshal(map[string]any{"variant": "success", "title": it.Copy.Toast, "ttl": 3000})
			own["data-hui-copy-toast"] = string(b)
		}
	}
	if it.Disabled {
		Mark(own, "disabled")
	}
	return b.El("button", PartMenuItem, Merge(Merge(own, rowMark), Attrs(map[string]string{"type": "button"})), kids...)
}

func menuRowRole(it MenuItem) string {
	if it.Radio != "" {
		return "menuitemradio"
	}
	if it.Check {
		return "menuitemcheckbox"
	}
	return "menuitem"
}

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
		rowAttrs["aria-disabled"] = "true"
	}
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
				return b.El("div", PartPanel, subAttrs, menuRows(b, it.Children, subPanelID, false))
			}
			if !childHasContent {
				subAttrs = Internal(subAttrs)
			}
			return b.El("div", PartPanel, subAttrs, menuRows(b, it.Children, subPanelID, childHasContent))
		}(),
	)
}

func checkMenuItemCoherence(it MenuItem) {
	checkMenuRowShape(it)
	if it.RPC != "" {
		checkSameOrigin("MenuItem", "RPC", it.RPC)
	}
	if it.Check && it.Radio != "" {
		panic("headless: MenuItem cannot be both Check and Radio — a row is one kind of option")
	}
	if len(it.Children) > 0 {
		if it.Check {
			panic("headless: MenuItem with Check cannot have Children — a checkbox row is a leaf option, a submenu parent is a disclosure")
		}
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

func checkMenuRowShape(it MenuItem) {
	if it.RPCAttrs != nil {
		if it.Href != "" || it.RPC != "" || it.Action != nil || it.Copy != nil || it.Radio != "" || len(it.Children) > 0 {
			panic("headless: MenuItem with RPCAttrs cannot also set Href, RPC, Action, Copy, Radio or Children — the row is one RPC")
		}
		checkSameOrigin("MenuItem", "RPCAttrs data-cui-rpc", it.RPCAttrs["data-cui-rpc"])
		for k := range it.RPCAttrs {
			switch k {
			case "data-cui-confirm", "data-cui-confirm-title", "data-cui-confirm-accept", "data-cui-confirm-tone":
				continue
			}
			if !strings.HasPrefix(k, "data-cui-rpc") {
				panic("headless: MenuItem RPCAttrs takes only data-cui-rpc* and data-cui-confirm* keys, got " + strconv.Quote(k))
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

func menuItemIDs(items []MenuItem, seen *[]string) {
	for _, it := range items {
		if it.Separator {
			continue
		}
		if it.ID != "" {
			for _, s := range *seen {
				if s == it.ID {
					panic("headless: Menu renders the row id " + it.ID + " twice — every reference to it now points at the first one")
				}
			}
			*seen = append(*seen, it.ID)
		}
		menuItemIDs(it.Children, seen)
	}
}

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
			"data-hui-menu-radio", "data-hui-menu-lazy", "data-hui-disclosure", "data-hui-disclosure-dismiss"},
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
