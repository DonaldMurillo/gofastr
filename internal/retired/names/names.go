// Package names holds the retired-name data model and the rule for which
// names the runtime check keeps. internal/retired reads its generated
// table through these types, and internal/retired/internal/tablegen
// writes that table with them. It is a leaf on purpose: the generator
// must build when the table it regenerates is missing, conflicted or
// stale, so it imports this package, never internal/retired.
package names

import "strings"

// Note is the breaking migration note that retired a name: the release
// that retired it and the change it describes.
type Note struct {
	Version string
	Change  string
}

// Entry is one retired name and the note that retired it. Kind is
// "class" or "attr"; an attr Name spelled with a trailing "-" retires
// every extension of it.
type Entry struct {
	Kind string
	Name string
	Note *Note
}

// Keep returns the entries the runtime check reads, in order: names in
// the kit's namespaces only, and the first entry naming a name wins, so a
// name retired in one release and mentioned again later keeps its
// original note. Attribute names fold case, the way HTML reads them.
func Keep(entries []Entry) []Entry {
	var kept []Entry
	classes := map[string]bool{}
	attrs := map[string]bool{}
	for _, e := range entries {
		switch e.Kind {
		case "class":
			if !kitClass(e.Name) || classes[e.Name] {
				continue
			}
			classes[e.Name] = true
		case "attr":
			lower := strings.ToLower(e.Name)
			if !kitAttr(lower) || attrs[lower] {
				continue
			}
			attrs[lower] = true
		default:
			continue
		}
		kept = append(kept, e)
	}
	return kept
}

// kitClass and kitAttr are the namespaces the kit owns. A breaking
// note can retire a generic name the kit used to emit (a "card" class,
// a data-placeholder attribute), but an app may own that name too, so
// only names in these namespaces are read as retired in rendered
// markup; the source scan still reports the rest.
func kitClass(name string) bool {
	return strings.HasPrefix(name, "ui-") || strings.HasPrefix(name, "fui-")
}

// kitAttr takes a lowercased attribute name.
func kitAttr(lower string) bool {
	return strings.HasPrefix(lower, "data-cui-") || strings.HasPrefix(lower, "data-fui-") || strings.HasPrefix(lower, "data-hui-")
}
