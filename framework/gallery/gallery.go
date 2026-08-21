// Package gallery ships the importable component catalog that powers the
// /components showcase on the docs site (examples/site) and any other tool
// that needs to render every design-system component against an arbitrary
// theme, most notably the theme-configuration tool that will live inside
// the gofastr CLI binary (cmd/gofastr), which cannot import examples/.
//
// The catalog is the single source of truth for which components exist and
// how each one renders with sensible default configuration. It is a
// sibling of framework/sdkdocs: both compose framework/ui + core-ui/* into
// a reusable, importable surface. Like sdkdocs, it is deliberately NOT
// part of framework/uihost: uihost must never import framework/ui (its
// always-on styles would leak into every host's CSS bundle), and a gallery
// of pre-canned demos is the opposite of a host's job.
//
// Layering: the package imports only framework/ui, core-ui/*, and
// core/render. It does not import the framework root facade, framework/crud,
// framework/entity, or anything higher up the L1–L5 diagram in
// framework/ARCHITECTURE.md, so it introduces no cycle and is safely
// importable from cmd/gofastr, examples/site, and host apps alike.
//
// Three of the 141 entries (sortablelist, optimisticcreate, optimisticdelete)
// have a live demo that is normally backed by per-visitor session state on
// the docs site. The gallery catalog ships self-contained seed-rendering
// Demo closures for them so a theme previewer or static export gets a
// realistic render out of the box; hosts that wire live session state
// (like examples/site) call the render helpers in demos.go directly from
// their own request-bearing SSR path, bypassing the Demo closure.
package gallery

import "github.com/DonaldMurillo/gofastr/core/render"

// Entry describes one component in the catalog. It is the shape every
// /components/<slug> page iterates over: main.go registers a route per
// entry, the index screen renders a card per entry, and the showcase screen
// looks up the active entry by slug.
type Entry struct {
	Slug     string
	Name     string
	Category string
	Desc     string
	// Demo returns a self-contained render.HTML showing the component live,
	// configured with sensible defaults so the page works without setup.
	// Closures that need backend wiring (DataTable's RPC island,
	// ConfirmAction's modal) render a smaller stand-alone variant or a
	// static note. IsNoteOnly reports whether a slug renders an
	// explanatory note instead of a live instance (the set itself is
	// private; that accessor is the only way in).
	Demo func() render.HTML
}

// Group is a category-grouped slice of entries: the shape the index page
// and the navigation sidebar iterate over. Groups are returned in catalog
// (display) order.
type Group struct {
	Name    string
	Entries []Entry
}

// Lookup returns the entry for slug and ok=true, or the zero Entry and
// ok=false if the slug is unknown.
func Lookup(slug string) (Entry, bool) {
	for _, e := range Catalog {
		if e.Slug == slug {
			return e, true
		}
	}
	return Entry{}, false
}

// MustLookup returns the entry for slug, panicking if it is unknown. Use
// only for slugs that are guaranteed by construction (e.g. the slug came
// from ranging over Catalog itself).
func MustLookup(slug string) Entry {
	e, ok := Lookup(slug)
	if !ok {
		panic("gallery: unknown slug " + slug)
	}
	return e
}

// ByCategory returns every entry whose Category matches, in catalog order.
// Returns nil if the category is unknown.
func ByCategory(category string) []Entry {
	var out []Entry
	for _, e := range Catalog {
		if e.Category == category {
			out = append(out, e)
		}
	}
	return out
}

// Grouped returns the catalog grouped by category in display (catalog)
// order. The same category ordering is used by the index page's sections
// and the navigation sidebar, so the two cannot drift.
func Grouped() []Group {
	var groups []Group
	seen := map[string]int{}
	for _, c := range Catalog {
		if i, ok := seen[c.Category]; ok {
			groups[i].Entries = append(groups[i].Entries, c)
			continue
		}
		seen[c.Category] = len(groups)
		groups = append(groups, Group{Name: c.Category, Entries: []Entry{c}})
	}
	return groups
}

// Categories returns the distinct category names in display (catalog)
// order: the names of Grouped().
func Categories() []string {
	groups := Grouped()
	out := make([]string, len(groups))
	for i, g := range groups {
		out[i] = g.Name
	}
	return out
}

// IsNoteOnly reports whether the slug's showcase renders an explanatory
// note instead of a live demo. Those components need per-page backend
// wiring (an RPC, a mounted widget, image sources) that a self-contained
// demo cannot provide.
func IsNoteOnly(slug string) bool { return noteOnlySlugs[slug] }

// CodeSnippet returns the example Go source for a component's showcase
// page, or "" when no snippet is registered. Read through this accessor
// rather than the private codeSnippets map so the map is never exposed
// as a mutable global to request handlers.
func CodeSnippet(slug string) string { return codeSnippets[slug] }

// DemoFor returns the Demo closure for slug, or nil for an unknown slug.
//
// Hosts whose live demos need per-request session state (e.g. examples/site
// renders sortablelist/optimisticcreate/optimisticdelete from the visitor's
// own demoState) switch on the slug in their own SSR path and call the
// Render* helpers directly.
func DemoFor(slug string) func() render.HTML {
	if e, ok := Lookup(slug); ok {
		return e.Demo
	}
	return nil
}
