package main

// ComponentsSidebar — category navigation for /components/*. A single
// interactive.SectionMenu: a sticky collapsible rail on desktop, a slide-in
// sheet on mobile. The active component is highlighted by the runtime's
// active-link pass (exact-href aria-current) since the sidebar persists across
// the screen-group's client-side navigation.
//
// The catalog this iterates is owned by framework/gallery; the helpers below
// re-export gallery's data so the rest of examples/site keeps using the same
// names (groupCatalog, demoSectionMenuConfig, componentGroup) it did when the
// catalog was local.

import (
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/gallery"
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
//
// Re-exported here as a wrapper because main.go and the catalog both call it
// by this name; gallery owns the actual config.
func demoSectionMenuConfig() interactive.SectionMenuConfig {
	return gallery.DemoSectionMenuConfig()
}

// groupCatalog — category-grouped catalog. Shared here to keep navigation
// in lock-step with the showcase.
func groupCatalog() []componentGroup {
	return gallery.Grouped()
}

// componentGroup is the category-grouped slice the sidebar and the index
// screen iterate. Alias of gallery.Group so callers can keep using the
// unexported name they did when the catalog was local.
type componentGroup = gallery.Group
