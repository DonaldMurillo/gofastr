package headless

import (
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The sidebar: a shell (the collapse and drawer controls, the
// data-hui-sidebar root the module owns) around a navigation landmark
// of groups and links. Groups render in one of two dialects — native
// details (the browser owns open/close; the disclosure module adds the
// mirror, Escape and the per-group persist key) or a button pair
// (aria-expanded + aria-controls + hidden, the shape some host
// contracts pin) — and the collapse state is the caller's: a Sidebar
// with no CollapseStorageKey is server-owned and the module never
// writes storage without a key, the retired module's contract kept.
// The mobile drawer is the widget runtime's (Requires declared); this
// module never reimplements its trap or return-focus.

// Sidebar parts.
const (
	PartSidebarGroup       Part = "sidebar-group"
	PartSidebarGroupList   Part = "sidebar-group-list"
	PartSidebarGroupToggle Part = "sidebar-group-toggle"
	PartSidebarToggle      Part = "sidebar-toggle"
	PartSidebarDrawer      Part = "sidebar-drawer"
	PartSidebarInline      Part = "sidebar-inline"
	PartSidebarItem        Part = "sidebar-item"
	PartSidebarNav         Part = "sidebar-nav"
	PartSidebarPrepend     Part = "sidebar-prepend"
)

// SidebarItem is one nav entry: a link, or a group with children.
type SidebarItem struct {
	// Label is the entry's text. Required.
	Label string
	// Href is the link's destination. Required for leaves; refused on
	// group parents (a group parent is a disclosure, not a link).
	Href string
	// Icon is optional inline HTML before the label.
	Icon render.HTML
	// Active marks the current page's link.
	Active bool
	// Open opens a group at SSR. Inert on leaves.
	Open bool
	// Children make this entry a group. Mutually exclusive with Href.
	Children []SidebarItem
}

// SidebarProps configures the sidebar.
type SidebarProps struct {
	// NavLabel names the navigation landmark. Required.
	NavLabel string
	// Title is the optional heading above the list.
	Title string
	// Items, in order. Required non-empty.
	Items []SidebarItem
	// Variant names the layout posture: "persistent" (always shown),
	// "collapsible" (toggle collapses to a rail), "off-canvas"
	// (drawer-only), "auto-hide" (a rail at rest that reveals on hover
	// or focus — pure presentation, the sheet's own). Empty takes
	// persistent.
	Variant string
	// Collapse names the collapse behaviour for the collapsible
	// variant: "none" (server-owned state), "auto" (the module owns
	// and restores it). Empty takes none.
	Collapse string
	// ServerCollapsed ships the server-owned collapsed state for the
	// collapsible variant (data-collapsed on the root, aria-expanded
	// and the state-matched label on the toggle). Nil means the state
	// is not the server's to say here: with Collapse "auto" the module
	// restores it after hydration, and no data-collapsed ships.
	ServerCollapsed *bool
	// DrawerName names the widget the mobile drawer opens; empty
	// renders no drawer trigger (the caller mounts the drawer).
	DrawerName string
	// DrawerLabel names the drawer trigger button. Empty leaves the
	// trigger unnamed — a caller rendering the trigger passes the
	// words, this package says none of its own.
	DrawerLabel string
	// CollapseStorageKey names the storage the collapse state lives
	// in when Collapse is auto. Empty means server-owned: the module
	// never writes storage without it.
	CollapseStorageKey string
	// HideDrawerTrigger suppresses the drawer trigger button (a host
	// that opens the drawer from its own chrome).
	HideDrawerTrigger bool
	// CollapseLabel / ExpandLabel are the collapse toggle's two
	// accessible names, resolved by the caller. Both always ride the
	// button as data-hui-sidebar-collapse-label / -expand-label: the
	// module re-says them after a client-side toggle and carries no
	// sentence of its own. Empty falls back to NavLabel for the
	// button's initial aria-label.
	CollapseLabel string
	ExpandLabel   string
	// GroupMarkup selects the dialect for groups: "" or "details"
	// (native details + the disclosure module's persist key) or
	// "button" (aria-expanded + aria-controls + hidden, which the
	// sidebar module's group-toggle mirror owns).
	GroupMarkup string
	// GroupIDPrefix derives each group's id (the button dialect's
	// aria-controls target) and per-group persist key (the details
	// dialect), as <prefix>-g<N>. Empty takes DrawerName + "-inline",
	// or "sidebar-inline" when DrawerName is empty too.
	GroupIDPrefix string
	// Prepend / Footer are optional slots above and below the list.
	Prepend render.HTML
	Footer  render.HTML

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs on the shell, the controls, the region's parts and
	// every item part.
	Parts Parts
}

// groupIDPrefix resolves the per-render id prefix for groups.
func (p SidebarProps) groupIDPrefix() string {
	if p.GroupIDPrefix != "" {
		return p.GroupIDPrefix
	}
	if p.DrawerName != "" {
		return p.DrawerName + "-inline"
	}
	return "sidebar-inline"
}

// Sidebar renders the shell: the data-hui-sidebar root the module
// owns, the drawer trigger, the inline column with the collapse
// toggle, and the region (see SidebarRegion) inside it.
func Sidebar(p SidebarProps, s Classes) render.HTML {
	if p.NavLabel == "" {
		panic("headless: Sidebar requires NavLabel — an unnamed navigation landmark is a region a screen reader cannot jump to")
	}
	if strings.TrimSpace(p.NavLabel) == "" {
		panic("headless: Sidebar NavLabel is only whitespace — an unnamed navigation landmark is a region a screen reader cannot jump to")
	}
	if len(p.Items) == 0 {
		panic("headless: Sidebar requires at least one item — an empty navigation is a landmark with nothing under its name")
	}
	if p.Variant == "" {
		p.Variant = "persistent"
	}
	checkEnum("Sidebar", "Variant", p.Variant, "persistent", "collapsible", "off-canvas", "auto-hide")
	if p.Collapse == "" {
		p.Collapse = "none"
	}
	checkEnum("Sidebar", "Collapse", p.Collapse, "none", "auto")
	if p.ServerCollapsed != nil && p.Collapse == "auto" {
		panic("headless: Sidebar ServerCollapsed says the state is the server's while Collapse is auto — pick the owner: the attribute pair would have the module overwrite the server's bytes on hydration")
	}
	if p.CollapseStorageKey != "" {
		checkStorageKey("Sidebar", p.CollapseStorageKey)
	}
	if p.GroupMarkup != "" {
		checkEnum("Sidebar", "GroupMarkup", p.GroupMarkup, "details", "button")
	}
	checkNoControlBytes("Sidebar", "GroupIDPrefix", p.groupIDPrefix())
	b := p.Parts.Box(s)

	own := Safe(p.ExtraAttrs, "aria-label", "data-collapsed")
	Mark(own, "data-hui-sidebar")
	own["data-hui-sidebar-variant"] = p.Variant
	if p.ID != "" {
		own["id"] = p.ID
	}
	if p.Collapse != "none" {
		own["data-hui-sidebar-collapse"] = p.Collapse
		if p.CollapseStorageKey != "" {
			own["data-hui-sidebar-storage"] = p.CollapseStorageKey
		}
	}
	if p.ServerCollapsed != nil {
		// Both spellings ship: the collapsed rail keys the sheet off
		// data-collapsed="true" and the module compares !== "true", so
		// an explicit false is the server saying "expanded", not the
		// absence of an opinion.
		own["data-collapsed"] = boolWord(*p.ServerCollapsed)
	}

	children := []render.HTML{}
	if p.DrawerName != "" && !p.HideDrawerTrigger {
		drawerAttrs := Attrs(map[string]string{
			"type":          "button",
			"data-fui-open": p.DrawerName,
		})
		if p.DrawerLabel != "" {
			drawerAttrs["aria-label"] = scrubControlBytes(p.DrawerLabel)
		}
		children = append(children,
			b.El("button", PartSidebarDrawer, drawerAttrs,
				render.Tag("span", map[string]string{"aria-hidden": "true"}, render.Text("☰"))))
	}

	inlineID := p.groupIDPrefix()
	inline := []render.HTML{}
	if p.Variant == "collapsible" {
		collapsed := p.ServerCollapsed != nil && *p.ServerCollapsed
		label := p.CollapseLabel
		if collapsed && p.ExpandLabel != "" {
			label = p.ExpandLabel
		}
		if label == "" {
			label = p.NavLabel
		}
		toggleAttrs := Attrs(map[string]string{
			"type":                            "button",
			"aria-controls":                   inlineID,
			"aria-expanded":                   boolWord(!collapsed),
			"aria-label":                      scrubControlBytes(label),
			"data-hui-sidebar-collapse-label": scrubControlBytes(p.CollapseLabel),
			"data-hui-sidebar-expand-label":   scrubControlBytes(p.ExpandLabel),
		})
		Mark(toggleAttrs, "data-hui-sidebar-toggle")
		inline = append(inline,
			b.El("button", PartSidebarToggle, toggleAttrs,
				render.Tag("span", map[string]string{"aria-hidden": "true"}, render.Text("‹"))))
	}
	inline = append(inline, sidebarRegionChildren(b, p)...)
	children = append(children,
		b.El("div", PartSidebarInline, Attrs(map[string]string{"id": inlineID}), inline...))
	return b.El("div", PartRoot, own, children...)
}

// SidebarRegion renders the navigation content alone — the title, the
// prepend slot, the nav landmark with the items, the footer — with no
// data-hui-sidebar shell hooks: a host slotting the region into its
// own chrome (a drawer body, a pinned panel) calls this directly, and
// the module never treats it as a sidebar root.
func SidebarRegion(p SidebarProps, s Classes) render.HTML {
	if p.NavLabel == "" || strings.TrimSpace(p.NavLabel) == "" {
		panic("headless: SidebarRegion requires NavLabel — an unnamed navigation landmark is a region a screen reader cannot jump to")
	}
	if len(p.Items) == 0 {
		panic("headless: SidebarRegion requires at least one item — an empty navigation is a landmark with nothing under its name")
	}
	if p.GroupMarkup != "" {
		checkEnum("SidebarRegion", "GroupMarkup", p.GroupMarkup, "details", "button")
	}
	checkNoControlBytes("SidebarRegion", "GroupIDPrefix", p.groupIDPrefix())
	return sidebarRegion(p.Parts.Box(s), p)
}

func sidebarRegion(b Box, p SidebarProps) render.HTML {
	return b.El("div", PartRoot, nil, sidebarRegionChildren(b, p)...)
}

// sidebarRegionChildren renders the region's parts bare, so the shell
// can slot them straight into its inline column without an extra
// wrapper box.
func sidebarRegionChildren(b Box, p SidebarProps) []render.HTML {
	children := []render.HTML{}
	if p.Title != "" {
		children = append(children, b.El("h2", PartTitle, nil, render.Text(scrubControlBytes(p.Title))))
	}
	if p.Prepend != "" {
		children = append(children, b.El("div", PartSidebarPrepend, nil, p.Prepend))
	}
	st := &sidebarWalk{buttons: p.GroupMarkup == "button", prefix: p.groupIDPrefix()}
	items := make([]render.HTML, 0, len(p.Items))
	for _, it := range p.Items {
		items = append(items, sidebarItem(b, it, st, 0))
	}
	children = append(children,
		b.El("nav", PartSidebarNav, Attrs(map[string]string{
			"aria-label": scrubControlBytes(p.NavLabel),
		}), b.El("ul", PartBody, nil, items...)))
	if p.Footer != "" {
		children = append(children, b.El("div", PartFooter, nil, p.Footer))
	}
	return children
}

// sidebarWalk threads the group dialect and the per-render id
// allocator through the item tree.
type sidebarWalk struct {
	buttons bool
	prefix  string
	seq     int
}

func (st *sidebarWalk) groupID() string {
	st.seq++
	return st.prefix + "-g" + strconv.Itoa(st.seq)
}

func sidebarItem(b Box, it SidebarItem, st *sidebarWalk, depth int) render.HTML {
	if strings.TrimSpace(it.Label) == "" {
		panic("headless: Sidebar item requires Label — a link with no text is not a link")
	}
	if len(it.Children) > 0 && it.Href != "" {
		panic("headless: Sidebar item with Children cannot also set Href — a group parent is a disclosure, not a link")
	}
	itemAttrs := html.Attrs(nil)
	if depth > 0 {
		// The sub-item variant rides the item part's own class slot,
		// appended after the class map's item class.
		if v := b.Classes.Variant(PartSidebarItem, "sub"); v != "" {
			itemAttrs = html.Attrs{"class": v}
		}
	}
	// The icon is decorative: the label span is the name. The icon
	// part wraps whatever glyph the caller hands in, so the class map
	// can size it without forking the item. An item with no glyph
	// carries its label's initial under the icon part's fallback
	// variant — hidden at rest, shown where a rail state needs a
	// glyph the caller never drew.
	var icon render.HTML
	if it.Icon != "" {
		icon = b.El("span", PartIcon, Attrs(map[string]string{"aria-hidden": "true"}), it.Icon)
	} else if v := b.Classes.Variant(PartIcon, "fallback"); v != "" {
		icon = b.El("span", PartIcon, Attrs(map[string]string{
			"aria-hidden": "true", "class": v,
		}), render.Text(sidebarInitial(it.Label)))
	}
	if len(it.Children) > 0 {
		kids := make([]render.HTML, 0, len(it.Children))
		for _, c := range it.Children {
			kids = append(kids, sidebarItem(b, c, st, depth+1))
		}
		if st.buttons {
			// Button dialect: the toggle owns aria-expanded, the
			// element it names via aria-controls owns hidden — both
			// bound by the sidebar module's group-toggle mirror.
			id := st.groupID()
			toggleAttrs := Attrs(map[string]string{
				"type":          "button",
				"aria-expanded": boolWord(it.Open),
				"aria-controls": id,
			})
			Mark(toggleAttrs, "data-hui-sidebar-group-toggle")
			if v := b.Classes.Class(PartSidebarGroupToggle); v != "" {
				toggleAttrs["class"] = v
			}
			listAttrs := Attrs(map[string]string{"id": id})
			if !it.Open {
				Mark(listAttrs, "hidden")
			}
			return b.El("li", PartSidebarItem, itemAttrs,
				b.El("button", PartControl, toggleAttrs,
					icon, b.El("span", PartText, nil, render.Text(scrubControlBytes(it.Label)))),
				b.El("ul", PartSidebarGroupList, listAttrs, kids...))
		}
		// Details dialect: native open/close, the disclosure module's
		// mirror and Escape, and a per-group persist key so the user's
		// open sections survive in-shell navigation.
		groupAttrs := Attrs(map[string]string{
			"data-hui-disclosure-persist": st.groupID(),
		})
		Mark(groupAttrs, "data-hui-disclosure")
		if it.Open {
			Mark(groupAttrs, "open")
		}
		return b.El("li", PartSidebarItem, itemAttrs,
			b.El("details", PartSidebarGroup, groupAttrs,
				b.El("summary", PartControl, nil,
					icon, b.El("span", PartText, nil, render.Text(scrubControlBytes(it.Label)))),
				b.El("ul", PartSidebarGroupList, nil, kids...)))
	}
	href := safeHref(it.Href)
	own := Attrs(map[string]string{"href": href})
	if it.Active {
		own["aria-current"] = "page"
	}
	return b.El("li", PartSidebarItem, itemAttrs,
		b.El("a", PartControl, own,
			icon,
			b.El("span", PartText, nil, render.Text(scrubControlBytes(it.Label)))))
}

// sidebarInitial takes a label's first rune, uppercased — the glyph a
// rail state shows when the caller drew no icon. "•" when the label is
// nothing but separators.
func sidebarInitial(label string) string {
	rs := []rune(strings.TrimSpace(label))
	if len(rs) == 0 {
		return "•"
	}
	return strings.ToUpper(string(rs[0]))
}

// boolWord spells a bool the way attribute values compare it.
func boolWord(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// safeHref routes a leaf href through the anchor policy, degrading to
// "#" the way every link this package renders does.
func safeHref(href string) string {
	if h := cleanTabHref(href); h != "" {
		return h
	}
	return "#"
}

func init() {
	Register(Spec{
		Name: "Sidebar",
		Anatomy: []Part{PartRoot, PartTitle, PartBody, PartFooter, PartIcon,
			PartSidebarGroup, PartSidebarGroupList, PartSidebarToggle,
			PartSidebarDrawer, PartSidebarInline, PartSidebarItem,
			PartSidebarNav, PartSidebarPrepend, PartControl, PartText,
			PartSidebarGroupToggle},
		Hooks: []string{"data-hui-sidebar", "data-hui-sidebar-variant",
			"data-hui-sidebar-collapse", "data-hui-sidebar-storage",
			"data-hui-sidebar-toggle",
			"data-hui-sidebar-collapse-label", "data-hui-sidebar-expand-label",
			"data-hui-sidebar-group-toggle"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Sidebar(SidebarProps{NavLabel: "Primary", Items: []SidebarItem{
				{Label: "Home", Href: "/"},
			}, Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "a persistent sidebar",
				Why:  "every leaf is a real link and every group a native details, so the whole navigation works with no script",
				HTML: Sidebar(SidebarProps{NavLabel: "Primary", Title: "Workspace",
					Prepend: render.Text("Workspace: acme"),
					Footer:  render.Text("v1.2.3"), Items: []SidebarItem{
						{Label: "Home", Href: "/", Active: true,
							Icon: render.Text("◆")},
						{Label: "Settings", Children: []SidebarItem{
							{Label: "Profile", Href: "/settings/profile"},
							{Label: "Team", Href: "/settings/team"},
						}},
					}}, s),
			}, {
				Name: "collapsible with storage",
				Why:  "the collapse state is opt-in to storage through the key — without one the state is the server's, and the module never writes",
				HTML: Sidebar(SidebarProps{NavLabel: "Primary", Variant: "collapsible",
					Collapse: "auto", CollapseStorageKey: "nav.side", DrawerName: "nav-drawer",
					DrawerLabel: "Open navigation", Items: []SidebarItem{{Label: "One", Href: "/one"}}}, s),
			}, {
				Name: "a server-collapsed sidebar with button groups",
				Why:  "the collapsed state ships in the SSR bytes and the module never writes storage for it, and the button dialect is the shape host contracts pin",
				HTML: Sidebar(SidebarProps{NavLabel: "Primary", Variant: "collapsible",
					ServerCollapsed: new(true), CollapseLabel: "Collapse navigation",
					ExpandLabel: "Expand navigation", GroupMarkup: "button",
					Items: []SidebarItem{{Label: "One", Href: "/one"}, {Label: "Two", Children: []SidebarItem{
						{Label: "Two A", Href: "/two-a"},
					}}}}, s),
			}}
		},
	})
}

func init() {
	Register(Spec{
		Name: "SidebarRegion",
		Anatomy: []Part{PartRoot, PartTitle, PartBody, PartFooter, PartIcon,
			PartSidebarNav, PartSidebarPrepend, PartSidebarGroup,
			PartSidebarGroupList, PartSidebarItem, PartControl, PartText,
			PartSidebarGroupToggle},
		Hooks: []string{"data-hui-sidebar-group-toggle"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return SidebarRegion(SidebarProps{NavLabel: "Primary", Items: []SidebarItem{
				{Label: "Home", Href: "/"},
			}, Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "a region slotted into a host's chrome",
				Why:  "no shell hooks render, so the module never treats the host's chrome as a sidebar root",
				HTML: SidebarRegion(SidebarProps{NavLabel: "Sections", Title: "Docs",
					GroupIDPrefix: "host-panel", GroupMarkup: "button",
					Prepend: render.Text("Workspace: acme"),
					Footer:  render.Text("v1.2.3"), Items: []SidebarItem{
						{Label: "One", Href: "/one",
							Icon: render.Text("◆")},
						{Label: "Two", Children: []SidebarItem{
							{Label: "Two A", Href: "/two-a"},
						}},
					}}, s),
			}, {
				Name: "a region with details groups",
				Why:  "the default dialect is native details, so the region's groups open with no script at all",
				HTML: SidebarRegion(SidebarProps{NavLabel: "Sections",
					GroupIDPrefix: "host-panel", Items: []SidebarItem{
						{Label: "Two", Children: []SidebarItem{
							{Label: "Two A", Href: "/two-a"},
						}},
					}}, s),
			}}
		},
	})
}
