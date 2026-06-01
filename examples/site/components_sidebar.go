package main

// ComponentsSidebar — category navigation for /components/*. A single
// interactive.SectionMenu: a sticky collapsible rail on desktop, a slide-in
// sheet on mobile. The active component is highlighted by the runtime's
// active-link pass (exact-href aria-current) since the sidebar persists across
// the screen-group's client-side navigation.

import (
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type ComponentsSidebar struct{}

func (s *ComponentsSidebar) Render() render.HTML {
	return interactive.SectionMenu(componentsSectionMenuConfig())
}

// componentsSectionMenuConfig is the single source of truth for the components
// nav — shared by the inline rail (ComponentsSidebar.Render) and the mounted
// mobile drawer (SectionMenuDrawer in main.go). Active state is left to the
// runtime's client-side aria-current pass since the sidebar persists across
// the screen group's navigation.
func componentsSectionMenuConfig() interactive.SectionMenuConfig {
	groups := groupCatalog()
	menuGroups := make([]interactive.SectionGroup, 0, len(groups))
	for _, g := range groups {
		items := make([]interactive.SectionItem, 0, len(g.Entries))
		for _, c := range g.Entries {
			items = append(items, interactive.SectionItem{
				Label: c.Name,
				Href:  "/components/" + c.Slug,
			})
		}
		// Collapsed by default so the mobile drawer stays scannable (the desktop
		// rail force-expands every group via CSS regardless).
		menuGroups = append(menuGroups, interactive.SectionGroup{
			Label:     g.Name,
			Items:     items,
			Collapsed: true,
		})
	}
	return interactive.SectionMenuConfig{
		AriaLabel:    "Components navigation",
		TriggerLabel: "Sections",
		DrawerName:   "components-section-menu",
		Lead:         &interactive.SectionItem{Label: "Overview", Href: "/components/"},
		Groups:       menuGroups,
	}
}

// demoSectionMenuConfig powers the /components/section-menu showcase — a small
// self-contained menu whose drawer is mounted in main.go like any real menu.
func demoSectionMenuConfig() interactive.SectionMenuConfig {
	return interactive.SectionMenuConfig{
		AriaLabel:    "Demo sections",
		TriggerLabel: "Sections",
		DrawerName:   "demo-section-menu",
		Lead:         &interactive.SectionItem{Label: "Overview", Href: "#overview"},
		Groups: []interactive.SectionGroup{
			{Eyebrow: "01", Label: "Modeling", Items: []interactive.SectionItem{
				{Label: "Entities", Href: "#entities", Active: true},
				{Label: "Filter DSL", Href: "#dsl"},
				{Label: "Relations", Href: "#relations"},
			}},
			{Eyebrow: "02", Label: "Serving", Collapsed: true, Items: []interactive.SectionItem{
				{Label: "Screens", Href: "#screens"},
				{Label: "Islands", Href: "#islands"},
			}},
		},
	}
}

// groupCatalog — category-grouped catalog. Shared here to keep navigation
// in lock-step with the showcase.
func groupCatalog() []componentGroup {
	var groups []componentGroup
	seen := map[string]int{}
	for _, c := range componentCatalog {
		if i, ok := seen[c.Category]; ok {
			groups[i].Entries = append(groups[i].Entries, c)
			continue
		}
		seen[c.Category] = len(groups)
		groups = append(groups, componentGroup{Name: c.Category, Entries: []componentEntry{c}})
	}
	return groups
}

type componentGroup struct {
	Name    string
	Entries []componentEntry
}
