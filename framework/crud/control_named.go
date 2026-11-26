package crud

import (
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// controlNamedKey reports the filter name of a field whose bare key the
// list route reads as a control instead: a field named like a list
// control word, or q on an entity that searches.
func controlNamedKey(ent *entity.Entity, f schema.Field) (string, bool) {
	if f.Hidden || f.NoQuery {
		return "", false
	}
	for _, name := range []string{f.WireName, f.Name} {
		if name == "" || !filter.IsListControl(name) {
			continue
		}
		if name == "q" && len(ent.Config.SearchFields) == 0 {
			continue
		}
		return name, true
	}
	return "", false
}

// logControlNamedFields writes one line per field whose bare key is a
// list control, naming the bracket key that filters it.
func logControlNamedFields(ent *entity.Entity) {
	for _, f := range ent.GetFields() {
		if key, ok := controlNamedKey(ent, f); ok {
			logf("crud: %s.%s shares its name with the %s list control: ?%s= sets the control; filter the column with ?%s[eq]=", ent.Config.Name, f.Name, key, key, key)
		}
	}
}
