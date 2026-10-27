package gallery

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/icons"
)

func iconDemo() render.HTML {
	return RenderIconCatalog(IconCatalogConfig{Action: "/components/icon"})
}

// IconCatalogConfig is the URL-backed state of the icon browser.
type IconCatalogConfig struct {
	// Action is the GET form's route. Required.
	Action string
	// Search matches words in names, aliases, and category labels, ignoring case
	// and treating hyphens and underscores as spaces.
	Search string
	// Category is an icons.Category ID. Empty or unknown IDs show all categories.
	Category string
}

// RenderIconCatalog renders the sourced icon catalog with a sticky search and
// category toolbar. Hosts read q and category from their request and pass them
// here so refresh, sharing, and browser history preserve the selected view.
func RenderIconCatalog(cfg IconCatalogConfig) render.HTML {
	names := icons.Names()
	categories := icons.Categories()
	validCategory := false
	for _, category := range categories {
		if category.ID == cfg.Category {
			validCategory = true
		}
	}
	if !validCategory {
		cfg.Category = ""
	}
	cfg.Search = strings.TrimSpace(cfg.Search)
	aliases := icons.Aliases()
	byTarget := map[string][]string{}
	for _, alias := range slices.Sorted(maps.Keys(aliases)) {
		target := aliases[alias]
		byTarget[target] = append(byTarget[target], alias)
	}
	counts := make(map[string]int, len(categories))
	for _, name := range names {
		category, _ := icons.CategoryFor(name)
		counts[category.ID]++
	}
	options := []ui.FacetOption{{Label: fmt.Sprintf("All categories (%d)", len(names))}}
	for _, category := range categories {
		options = append(options, ui.FacetOption{Value: category.ID, Label: fmt.Sprintf("%s (%d)", category.Label, counts[category.ID])})
	}
	tiles := make([]render.HTML, 0, len(names))
	for _, name := range names {
		category, _ := icons.CategoryFor(name)
		if cfg.Category != "" && category.ID != cfg.Category {
			continue
		}
		if !iconMatchesSearch(cfg.Search, name+" "+strings.Join(byTarget[name], " ")) && !iconMatchesSearch(cfg.Search, category.Label) {
			continue
		}
		id := "icon-name-" + name
		tiles = append(tiles, ui.Box(ui.BoxConfig{ExtraAttrs: html.Attrs{"data-icon-name": name, "data-icon-category": category.ID}},
			html.Span(html.TextConfig{ID: id, ExtraAttrs: html.Attrs{"hidden": ""}}, render.Text(name)),
			ui.CopyButton(ui.CopyButtonConfig{Target: id, Label: name, Icon: name, Tile: true, AriaLabel: "Copy " + name + " icon name"}),
		))
	}
	results := ui.Grid(ui.GridConfig{Min: "5rem", Gap: ui.GapMD, Fill: true}, tiles...)
	if len(tiles) == 0 {
		results = ui.EmptyState(ui.EmptyStateConfig{
			Title: "No icons found", Description: "Try another name or choose a different category.",
		})
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapMD, TrimMargins: true},
		ui.Muted(html.Span(html.TextConfig{ID: "icon-result-count"}, render.Text(fmt.Sprintf("%d / %d icons", len(tiles), len(names)))), render.Text(" · Click an icon to copy its name")),
		ui.Sticky(ui.StickyConfig{ID: "icon-catalog-header"},
			ui.Box(ui.BoxConfig{Pad: ui.BoxPadSM, Surface: true, Outlined: true},
				ui.FilterToolbar(ui.FilterToolbarConfig{
					ID: "icon-filters", Action: cfg.Action, Label: "Filters", Compact: true, Dropdown: true, ClientNavigation: true,
					Search:     &ui.FilterSearch{Name: "q", Value: cfg.Search, Label: "Search icons", Placeholder: "Search icons"},
					Facets:     []ui.Facet{{Name: "category", Label: "Category", Options: options, Value: cfg.Category}},
					ApplyLabel: "Search", ResetLabel: "Clear", HideReset: cfg.Search == "" && cfg.Category == "",
					Tools: []render.HTML{ui.Button(ui.ButtonConfig{Label: "Search icons", Icon: icons.Search, IconOnly: true, Type: "submit"})},
				}),
			),
		),
		results,
		ui.Muted(
			render.Text("Sources: "),
			ui.Link(ui.LinkConfig{Href: "https://github.com/feathericons/feather/tree/3dc050d97405062eba78aa57115c0a15c63abdaa", Text: "Feather, commit 3dc050d"}),
			render.Text(" under MIT, and "),
			ui.Link(ui.LinkConfig{Href: "https://github.com/lucide-icons/lucide/tree/7faf87628a1ad6175975bc595344337ce14a7db4", Text: "Lucide, commit 7faf876"}),
			render.Text(" under ISC with retained MIT notices. Retrieved October 24 and 25, 2026. SVGs are unmodified; sources.json records each file and alias."),
		),
		html.Heading(html.HeadingConfig{Level: 3}, render.Text("Icons in navigation")),
		ui.Grid(ui.GridConfig{Min: "16rem", Gap: ui.GapMD},
			ui.Card(ui.CardConfig{Heading: "Navigation", HeadingLevel: 4},
				ui.SidebarBody(ui.SidebarConfig{NavLabel: "Icon examples", Items: []ui.SidebarItem{
					{Label: "Files", Href: "/components/fileupload", Icon: ui.Icon(icons.Folder, ui.IconConfig{})},
					{Label: "People", Href: "/components/avatar", Icon: ui.Icon(icons.User, ui.IconConfig{})},
					{Label: "Settings", Href: "/components/themetoggle", Icon: ui.Icon(icons.Settings, ui.IconConfig{})},
				}}),
			),
			ui.Card(ui.CardConfig{Heading: "Links", HeadingLevel: 4},
				ui.Stack(ui.StackConfig{Gap: ui.GapSM, Align: ui.AlignStart},
					ui.LinkButton(ui.LinkButtonConfig{Label: "Browse files", Href: "/components/fileupload", Icon: icons.Folder}),
					ui.LinkButton(ui.LinkButtonConfig{Label: "Edit a form", Href: "/components/form", Icon: icons.Edit, Variant: ui.ButtonSecondary}),
					ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter},
						ui.Icon(icons.Check, ui.IconConfig{Size: "16"}), render.Text("All changes saved"),
					),
				),
			),
		),
	)
}

func iconMatchesSearch(query, text string) bool {
	normalize := strings.NewReplacer("-", " ", "_", " ")
	text = normalize.Replace(strings.ToLower(text))
	for _, word := range strings.Fields(normalize.Replace(strings.ToLower(query))) {
		if !strings.Contains(text, word) {
			return false
		}
	}
	return true
}
