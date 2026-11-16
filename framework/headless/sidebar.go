package headless

import (
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

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
	PartSidebarCount       Part = "sidebar-count"
)

// SidebarItem is one nav entry: a link, or a group with children.
type SidebarItem struct {
	// Label is the entry's text. Required.
	Label string
	// Href is a leaf's destination; a group parent has none.
	Href string
	// Icon is optional inline HTML before the label.
	Icon render.HTML
	// Active marks the current page's link for first paint.
	Active bool
	// Open opens a group at SSR. Inert on leaves.
	Open bool
	// MatchPrefix is the section prefix the runtime's active-link module matches sub-paths against.
	MatchPrefix string
	// Count is caller content after the label, such as an unread total.
	Count render.HTML
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
	// Variant is persistent (empty), collapsible, off-canvas or auto-hide.
	Variant string
	// Collapse is none (empty, server-owned) or auto (the module restores it).
	Collapse string
	// ServerCollapsed ships the server-owned collapsed state; nil leaves it unsaid.
	ServerCollapsed *bool
	// DrawerName names the widget the mobile drawer opens; empty renders no drawer trigger.
	DrawerName string
	// DrawerLabel names the drawer trigger; blank takes Strings.SidebarOpenDrawer.
	DrawerLabel string
	// CollapseStorageKey names the storage an auto collapse state persists in; empty persists nothing.
	CollapseStorageKey string
	// HideDrawerTrigger suppresses the shell's drawer trigger.
	HideDrawerTrigger bool
	// CollapseLabel / ExpandLabel name the collapse toggle in each state; empty takes NavLabel.
	CollapseLabel string
	ExpandLabel   string
	// ToggleIcon is the collapse toggle's decorative mark; empty draws ‹.
	ToggleIcon render.HTML
	// ToggleText is the collapse toggle's visible word, inside its accessible name.
	ToggleText string
	// GroupMarkup is the group dialect: details (empty) or button.
	GroupMarkup string
	// GroupIDPrefix derives each group's id as <prefix>-g<N>; empty takes DrawerName-inline or sidebar-inline.
	GroupIDPrefix string
	// Prepend / Footer are optional slots above and below the list.
	Prepend render.HTML
	Footer  render.HTML

	ID         string
	ExtraAttrs html.Attrs

	Parts   Parts
	Strings *Strings
}

func (p SidebarProps) groupIDPrefix() string {
	if p.GroupIDPrefix != "" {
		return p.GroupIDPrefix
	}
	if p.DrawerName != "" {
		return p.DrawerName + "-inline"
	}
	return "sidebar-inline"
}

// Sidebar renders the shell: the drawer trigger, the inline column with the collapse toggle, and the region.
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
		own["data-collapsed"] = boolWord(*p.ServerCollapsed)
	}

	children := []render.HTML{}
	if p.DrawerName != "" && !p.HideDrawerTrigger {
		children = append(children, sidebarDrawerTrigger(b, p, PartSidebarDrawer))
	}

	inlineID := p.groupIDPrefix()
	hasToggle := p.Variant == "collapsible"
	regionChildren, hasContent := sidebarRegionChildren(b, p, false)
	inline := append([]render.HTML{}, regionChildren...)
	if hasToggle {
		collapsed := p.ServerCollapsed != nil && *p.ServerCollapsed
		collapseLabel, expandLabel := p.CollapseLabel, p.ExpandLabel
		if collapseLabel == "" {
			collapseLabel = p.NavLabel
		}
		if expandLabel == "" {
			expandLabel = p.NavLabel
		}
		label := collapseLabel
		if collapsed {
			label = expandLabel
		}
		toggleAttrs := Attrs(map[string]string{
			"type":                            "button",
			"aria-controls":                   inlineID,
			"aria-expanded":                   boolWord(!collapsed),
			"aria-label":                      scrubControlBytes(label),
			"data-hui-sidebar-collapse-label": scrubControlBytes(collapseLabel),
			"data-hui-sidebar-expand-label":   scrubControlBytes(expandLabel),
		})
		Mark(toggleAttrs, "data-hui-sidebar-toggle")
		if hasContent {
			toggleAttrs = Internal(toggleAttrs)
		}
		mark := p.ToggleIcon
		if mark == "" {
			mark = render.Text("‹")
		}
		face := []render.HTML{render.Tag("span", html.Attrs{"aria-hidden": "true"}, mark)}
		if p.ToggleText != "" {
			face = append(face, b.El("span", PartText, nil, render.Text(scrubControlBytes(p.ToggleText))))
		}
		inline = append(inline, b.El("button", PartSidebarToggle, toggleAttrs, face...))
	}
	inlineOwn := Attrs(map[string]string{"id": inlineID})
	if !hasContent {
		inlineOwn = Internal(inlineOwn)
	}
	children = append(children, b.El("div", PartSidebarInline, inlineOwn, inline...))
	return b.El("div", PartRoot, own, children...)
}

// SidebarDrawerTrigger renders the shell's drawer trigger on its own; empty DrawerName renders nothing.
func SidebarDrawerTrigger(p SidebarProps, s Classes) render.HTML {
	if p.DrawerName == "" {
		return ""
	}
	return sidebarDrawerTrigger(p.Parts.Box(s), p, PartRoot)
}

func sidebarDrawerTrigger(b Box, p SidebarProps, part Part) render.HTML {
	drawerAttrs := Attrs(map[string]string{
		"type":          "button",
		"data-cui-open": p.DrawerName,
	})
	label := p.DrawerLabel
	if strings.TrimSpace(label) == "" {
		label = p.Strings.Resolve().SidebarOpenDrawer
	}
	drawerAttrs["aria-label"] = scrubControlBytes(label)
	glyphAttrs := html.Attrs{"aria-hidden": "true"}
	if part == PartRoot {
		glyphAttrs = Internal(glyphAttrs)
	} else {
		drawerAttrs = Internal(drawerAttrs)
	}
	return b.El("button", part, drawerAttrs, render.Tag("span", glyphAttrs, render.Text("☰")))
}

// SidebarRegion renders the navigation content alone, with no shell hooks.
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
	children, _ := sidebarRegionChildren(b, p, true)
	return b.El("div", PartRoot, nil, children...)
}

func sidebarItemsHaveOwnContent(items []SidebarItem) bool {
	for _, it := range items {
		if it.Icon != "" || it.Count != "" {
			return true
		}
		if len(it.Children) > 0 && sidebarItemsHaveOwnContent(it.Children) {
			return true
		}
	}
	return false
}

func sidebarRegionChildren(b Box, p SidebarProps, selfMark bool) ([]render.HTML, bool) {
	itemsContent := sidebarItemsHaveOwnContent(p.Items)
	hasContent := p.Prepend != "" || p.Footer != "" || itemsContent

	children := []render.HTML{}
	if p.Title != "" {
		titleOwn := html.Attrs(nil)
		if hasContent || selfMark {
			titleOwn = Internal(nil)
		}
		children = append(children, b.El("h2", PartTitle, titleOwn, render.Text(scrubControlBytes(p.Title))))
	}
	if p.Prepend != "" {
		children = append(children, b.El("div", PartSidebarPrepend, nil, p.Prepend))
	}
	st := &sidebarWalk{buttons: p.GroupMarkup == "button", prefix: p.groupIDPrefix()}
	items := make([]render.HTML, 0, len(p.Items))
	for _, it := range p.Items {
		items = append(items, sidebarItem(b, it, st, 0, itemsContent))
	}
	navOwn := Attrs(map[string]string{"aria-label": scrubControlBytes(p.NavLabel)})
	if !itemsContent && (hasContent || selfMark) {
		navOwn = Internal(navOwn)
	}
	children = append(children,
		b.El("nav", PartSidebarNav, navOwn, b.El("ul", PartBody, nil, items...)))
	if p.Footer != "" {
		children = append(children, b.El("div", PartFooter, nil, p.Footer))
	}
	return children, hasContent
}

type sidebarWalk struct {
	buttons bool
	prefix  string
	seq     int
}

func (st *sidebarWalk) groupID() string {
	st.seq++
	return st.prefix + "-g" + strconv.Itoa(st.seq)
}

func sidebarItem(b Box, it SidebarItem, st *sidebarWalk, depth int, mark bool) render.HTML {
	if strings.TrimSpace(it.Label) == "" {
		panic("headless: Sidebar item requires Label — a link with no text is not a link")
	}
	if len(it.Children) > 0 && it.Href != "" {
		panic("headless: Sidebar item with Children cannot also set Href — a group parent is a disclosure, not a link; put the section's overview page in the group's first child link instead")
	}
	callerContent := it.Icon != "" || it.Count != ""
	itemAttrs := html.Attrs(nil)
	if depth > 0 {
		if v := b.Classes.Variant(PartSidebarItem, "sub"); v != "" {
			itemAttrs = html.Attrs{"class": v}
		}
	}
	var icon render.HTML
	if it.Icon != "" {
		icon = b.El("span", PartIcon, Attrs(map[string]string{"aria-hidden": "true"}), it.Icon)
	} else if v := b.Classes.Variant(PartIcon, "fallback"); v != "" {
		fallback := Attrs(map[string]string{"aria-hidden": "true", "class": v})
		if callerContent {
			fallback = Internal(fallback)
		}
		icon = b.El("span", PartIcon, fallback, render.Text(sidebarInitial(it.Label)))
	}
	textOwn, controlMark := html.Attrs(nil), html.Attrs(nil)
	if callerContent {
		textOwn = Internal(nil)
	} else if mark {
		controlMark = Internal(nil)
	}
	var count render.HTML
	if it.Count != "" {
		count = b.El("span", PartSidebarCount, nil, it.Count)
	}
	if len(it.Children) > 0 {
		childMark := sidebarItemsHaveOwnContent(it.Children)
		kids := make([]render.HTML, 0, len(it.Children))
		for _, c := range it.Children {
			kids = append(kids, sidebarItem(b, c, st, depth+1, childMark))
		}
		listOwn := html.Attrs(nil)
		if mark && !childMark {
			listOwn = Internal(nil)
		}
		if st.buttons {
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
			listAttrs := Merge(Attrs(map[string]string{"id": id}), listOwn)
			if !it.Open {
				Mark(listAttrs, "hidden")
			}
			return b.El("li", PartSidebarItem, itemAttrs,
				b.El("button", PartControl, Merge(toggleAttrs, controlMark),
					icon, b.El("span", PartText, textOwn, render.Text(scrubControlBytes(it.Label))), count),
				b.El("ul", PartSidebarGroupList, listAttrs, kids...))
		}
		groupAttrs := Attrs(map[string]string{
			"data-hui-disclosure-persist": st.groupID(),
		})
		Mark(groupAttrs, "data-hui-disclosure")
		if it.Open {
			Mark(groupAttrs, "open")
		}
		return b.El("li", PartSidebarItem, itemAttrs,
			b.El("details", PartSidebarGroup, groupAttrs,
				b.El("summary", PartControl, controlMark,
					icon, b.El("span", PartText, textOwn, render.Text(scrubControlBytes(it.Label))), count),
				b.El("ul", PartSidebarGroupList, listOwn, kids...)))
	}
	href := safeHref(it.Href)
	own := Attrs(map[string]string{"href": href})
	if it.Active {
		own["aria-current"] = "page"
	}
	Mark(own, "data-cui-activelink")
	if it.MatchPrefix != "" {
		own["data-cui-match-prefix"] = scrubControlBytes(it.MatchPrefix)
	}
	return b.El("li", PartSidebarItem, itemAttrs,
		b.El("a", PartControl, Merge(own, controlMark),
			icon,
			b.El("span", PartText, textOwn, render.Text(scrubControlBytes(it.Label))),
			count))
}

func sidebarInitial(label string) string {
	rs := []rune(strings.TrimSpace(label))
	if len(rs) == 0 {
		return "•"
	}
	return strings.ToUpper(string(rs[0]))
}

func boolWord(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

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

func init() {
	Register(Spec{
		Name:    "SidebarDrawerTrigger",
		Anatomy: []Part{PartRoot},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return SidebarDrawerTrigger(SidebarProps{NavLabel: "Primary", DrawerName: "nav-drawer",
				DrawerLabel: "Open navigation", Items: []SidebarItem{{Label: "Home", Href: "/"}},
				Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "the hamburger on its own",
				Why:  "the standalone trigger is the SAME button and widget contract as the shell's own — a host that relocates it into its header opens the same drawer, and the button is the component's root so the host's overrides land on it",
				HTML: SidebarDrawerTrigger(SidebarProps{NavLabel: "Primary", DrawerName: "nav-drawer",
					DrawerLabel: "Open navigation", Items: []SidebarItem{{Label: "Home", Href: "/"}}}, s),
			}, {
				Name: "an unlabelled copy",
				Why:  "with no DrawerLabel the button is named from Strings (Open navigation): it is the phone's only way into the navigation, and an unnamed hamburger is one a screen reader calls button",
				HTML: SidebarDrawerTrigger(SidebarProps{NavLabel: "Primary", DrawerName: "nav-drawer",
					Items: []SidebarItem{{Label: "Home", Href: "/"}}}, s),
			}}
		},
	})
}
