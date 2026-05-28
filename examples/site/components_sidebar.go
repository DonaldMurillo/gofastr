package main

// ComponentsSidebar — multi-level navigation for /components/*. Renders
// two different components depending on viewport via framework/ui.Responsive:
//
//   Desktop (>= 1024): the nestedlist tree (Overview + categories with
//                       expandable child links).
//   Mobile  (< 1024):  a category dropdown + jump <select> — fewer taps
//                       to reach a component than collapsible accordion,
//                       respects native iOS/Android picker, no JS needed
//                       to toggle.
//
// Both variants always render in the SSR output. The framework's
// Responsive primitive registers an @media stylesheet that hides the
// inactive variant via display:none — that also removes it from the
// accessibility tree, so AT users never hear duplicate landmarks.

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/patterns/nestedlist"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type ComponentsSidebar struct{}

func (s *ComponentsSidebar) Render() render.HTML {
	return ui.Responsive(ui.ResponsiveConfig{Breakpoint: 1024},
		s.renderDesktop(),
		s.renderMobile(),
	)
}

// renderDesktop — the full nestedlist tree. Always-on at desktop widths.
func (s *ComponentsSidebar) renderDesktop() render.HTML {
	groups := groupCatalog()
	items := []nestedlist.Item{
		{Label: "Overview", Href: "/components/"},
	}
	for _, g := range groups {
		children := make([]nestedlist.Item, 0, len(g.Entries))
		for _, c := range g.Entries {
			children = append(children, nestedlist.Item{
				Label: c.Name,
				Href:  "/components/" + c.Slug,
			})
		}
		items = append(items, nestedlist.Item{
			Label:    g.Name,
			Children: children,
			Expanded: true,
		})
	}
	return nestedlist.Render(nestedlist.Config{
		Items:     items,
		AriaLabel: "Components navigation",
		Class:     "components-sidebar",
	})
}

// renderMobile — a native <details> "Sections" disclosure containing
// the nested list of categories + components. Tap the summary to open
// (native OS picker behavior on touch); tap any link to navigate. The
// framework runtime's data-fui-disclosure rule auto-closes [open] on
// cross-page nav, so the drawer never lingers over the destination.
// Zero JS, accessible, lighter than the desktop tree (single-flat
// list — categories as <details> branches inside the outer details).
func (s *ComponentsSidebar) renderMobile() render.HTML {
	// Same nestedlist as desktop, but rendered with branches collapsed
	// by default so the drawer opens to a compact category list.
	groups := groupCatalog()
	items := []nestedlist.Item{
		{Label: "Overview", Href: "/components/"},
	}
	for _, g := range groups {
		children := make([]nestedlist.Item, 0, len(g.Entries))
		for _, c := range g.Entries {
			children = append(children, nestedlist.Item{
				Label: c.Name,
				Href:  "/components/" + c.Slug,
			})
		}
		items = append(items, nestedlist.Item{
			Label:    g.Name,
			Children: children,
			// Collapsed by default on mobile — tapping a category opens it,
			// keeping the drawer scannable instead of a 93-item flat scroll.
			Expanded: false,
		})
	}

	return render.Tag("details",
		map[string]string{
			"class":               "components-mobile-drawer",
			"data-fui-disclosure": "",
		},
		render.Tag("summary",
			map[string]string{"class": "components-mobile-drawer__toggle"},
			render.Text("Sections"),
		),
		html.Div(html.DivConfig{Class: "components-mobile-drawer__body"},
			nestedlist.Render(nestedlist.Config{
				Items:     items,
				AriaLabel: "Components navigation (mobile)",
				Class:     "components-sidebar",
			}),
		),
	)
}

// groupCatalog — category-grouped catalog used by both variants. Shared
// here to keep the two render paths in lock-step.
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
