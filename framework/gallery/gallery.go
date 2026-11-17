// Package gallery is the importable component catalog behind the
// /components showcase and the theme editor: Catalog holds every
// design-system component as a self-contained Demo, read through Lookup,
// Grouped and CodeSnippet. Hosts call MountDemoWidgets and
// MountDemoFormRoute before mounting their UIHost.
package gallery

import "github.com/DonaldMurillo/gofastr/core/render"

// Entry describes one component in the catalog.
type Entry struct {
	Slug     string
	Name     string
	Category string
	Desc     string
	// Demo renders the component live with default configuration, or a
	// note when IsNoteOnly reports the slug needs host wiring.
	Demo func() render.HTML
}

// Group is one category's entries, in catalog order.
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

// Grouped returns the catalog grouped by category, in catalog order.
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
// note instead of a live demo.
func IsNoteOnly(slug string) bool { return noteOnlySlugs[slug] }

// CodeSnippet returns the example Go source for a component's showcase
// page, or "" when no snippet is registered.
func CodeSnippet(slug string) string { return codeSnippets[slug] }

// DemoFor returns the Demo closure for slug, or nil for an unknown slug.
func DemoFor(slug string) func() render.HTML {
	if e, ok := Lookup(slug); ok {
		return e.Demo
	}
	return nil
}
