package headless

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The sidebar: a navigation landmark of groups and links. Groups are
// native details (the browser owns open/close; the disclosure module
// adds the mirror and Escape); the collapse state is the caller's —
// a Sidebar with no CollapseStorageKey is server-owned and the module
// never writes storage without a key, the retired module's contract
// kept. The mobile drawer is the widget runtime's (Requires declared);
// this module never reimplements its trap or return-focus.

// Sidebar parts.
const (
	PartSidebarGroup     Part = "sidebar-group"
	PartSidebarGroupList Part = "sidebar-group-list"
	PartSidebarToggle    Part = "sidebar-toggle"
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
	// Open opens a group's details at SSR. Inert on leaves.
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
	// (drawer-only). Empty takes persistent.
	Variant string
	// Collapse names the collapse behaviour for the collapsible
	// variant: "none" (server-owned state), "auto" (the module owns
	// and restores it). Empty takes none.
	Collapse string
	// DrawerName names the widget the mobile drawer opens; empty
	// renders no drawer trigger (the caller mounts the drawer).
	DrawerName string
	// CollapseStorageKey names the storage the collapse state lives
	// in when Collapse is auto. Empty means server-owned: the module
	// never writes storage without it.
	CollapseStorageKey string
	// Prepend / Footer are optional slots above and below the list.
	Prepend render.HTML
	Footer  render.HTML

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs on the root, groups, links and the toggle.
	Parts Parts
}

// Sidebar renders the navigation.
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
	checkEnum("Sidebar", "Variant", p.Variant, "persistent", "collapsible", "off-canvas")
	if p.Collapse == "" {
		p.Collapse = "none"
	}
	checkEnum("Sidebar", "Collapse", p.Collapse, "none", "auto")
	if p.CollapseStorageKey != "" {
		checkStorageKey("Sidebar", p.CollapseStorageKey)
	}
	b := p.Parts.Box(s)

	own := Merge(Safe(p.ExtraAttrs, "aria-label"), Attrs(map[string]string{
		"aria-label": scrubControlBytes(p.NavLabel),
		"id":         p.ID,
	}))
	Mark(own, "data-hui-sidebar")
	own["data-hui-sidebar-variant"] = p.Variant
	if p.Collapse != "none" {
		own["data-hui-sidebar-collapse"] = p.Collapse
		if p.CollapseStorageKey != "" {
			own["data-hui-sidebar-storage"] = p.CollapseStorageKey
		}
	}

	children := []render.HTML{}
	if p.Title != "" {
		children = append(children, b.El("div", PartTitle, nil, render.Text(scrubControlBytes(p.Title))))
	}
	if p.Prepend != "" {
		children = append(children, p.Prepend)
	}
	if p.Variant == "collapsible" || p.DrawerName != "" {
		toggleAttrs := Attrs(map[string]string{
			"type":       "button",
			"aria-label": scrubControlBytes(p.NavLabel),
		})
		Mark(toggleAttrs, "data-hui-sidebar-toggle")
		if p.DrawerName != "" {
			toggleAttrs["data-fui-open"] = p.DrawerName
		}
		children = append(children, b.El("button", PartSidebarToggle, toggleAttrs, render.Text("☰")))
	}
	list := make([]render.HTML, 0, len(p.Items))
	for _, it := range p.Items {
		list = append(list, sidebarItem(b, it, p.NavLabel))
	}
	children = append(children, b.El("ul", PartBody, nil, list...))
	if p.Footer != "" {
		children = append(children, p.Footer)
	}
	return b.El("nav", PartRoot, own, children...)
}

func sidebarItem(b Box, it SidebarItem, navLabel string) render.HTML {
	if strings.TrimSpace(it.Label) == "" {
		panic("headless: Sidebar item requires Label — a link with no text is not a link")
	}
	if len(it.Children) > 0 && it.Href != "" {
		panic("headless: Sidebar item with Children cannot also set Href — a group parent is a disclosure, not a link")
	}
	if len(it.Children) > 0 {
		groupAttrs := html.Attrs{}
		Mark(groupAttrs, "data-hui-sidebar-group")
		if it.Open {
			Mark(groupAttrs, "open")
		}
		kids := make([]render.HTML, 0, len(it.Children))
		for _, c := range it.Children {
			kids = append(kids, sidebarItem(b, c, navLabel))
		}
		return b.El("li", PartSidebarGroup, nil,
			b.El("details", PartSidebarGroup, groupAttrs,
				b.El("summary", PartControl, nil,
					it.Icon,
					b.El("span", PartText, nil, render.Text(scrubControlBytes(it.Label)))),
				b.El("ul", PartSidebarGroupList, nil, kids...)))
	}
	href := safeHref(it.Href)
	own := Attrs(map[string]string{"href": href})
	if it.Active {
		own["aria-current"] = "page"
	}
	return b.El("li", PartRoot, nil,
		b.El("a", PartControl, own,
			it.Icon,
			b.El("span", PartText, nil, render.Text(scrubControlBytes(it.Label)))))
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
		Anatomy: []Part{PartRoot, PartTitle, PartBody, PartSidebarGroup,
			PartSidebarGroupList, PartSidebarToggle, PartControl, PartText},
		Hooks: []string{"data-hui-sidebar", "data-hui-sidebar-variant",
			"data-hui-sidebar-collapse", "data-hui-sidebar-storage",
			"data-hui-sidebar-group", "data-hui-sidebar-toggle"},
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
				HTML: Sidebar(SidebarProps{NavLabel: "Primary", Title: "Workspace", Items: []SidebarItem{
					{Label: "Home", Href: "/", Active: true},
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
					Items: []SidebarItem{{Label: "One", Href: "/one"}}}, s),
			}}
		},
	})
}
