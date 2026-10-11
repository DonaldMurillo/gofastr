package framework

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// checkFilterSuffixCollisions refuses a queryable field whose name or
// wire name is another queryable field's name or wire name plus an
// operator suffix from filter.FilterSuffixes. Every URL parser tries
// the suffix before the plain key, so an entity declaring both `status`
// and `status_ne` parses ?status_ne=x as `status != x`: the operator
// wins, and the `status_ne` column can never be filtered at all — a
// silent read of the wrong column, not an error the caller ever sees.
// The same holds one relation down (?author.status_ne=) and inside
// include scopes (include=author(status_ne=…)), which share the table.
//
// Refused at registration, naming both fields, the same boot check
// Display's names get. It lives here and not in entity.Validate because
// framework/entity sits below framework/filter in the layering (L2
// imports core/ + internal/casing only) and the suffix list must be the
// filter package's own, never a copy that drifts.
func checkFilterSuffixCollisions(ent *entity.Entity) error {
	type claim struct{ field, key string }
	var claims []claim
	for _, f := range ent.Config.Fields {
		// Hidden and NoQuery columns are unreachable as filters, so they
		// claim no query key: a NoQuery `status_ne` is refused by name
		// on every surface, and cannot shadow anything.
		if f.Hidden || f.NoQuery {
			continue
		}
		// Both spellings the parsers accept: the Name always, the
		// WireName override when one is set.
		claims = append(claims, claim{f.Name, f.Name})
		if f.WireName != "" && f.WireName != f.Name {
			claims = append(claims, claim{f.Name, f.WireName})
		}
	}
	byKey := make(map[string]string, len(claims))
	for _, c := range claims {
		byKey[c.key] = c.field
	}
	// Declaration order for the base field, table order for the suffix:
	// the first collision a reader meets is the one reported, the same
	// fixed order every other registration check reports in.
	for _, base := range claims {
		for _, s := range filter.FilterSuffixes {
			shadow := base.key + s.Suffix
			other, ok := byKey[shadow]
			if !ok || other == base.field {
				continue
			}
			return fmt.Errorf(
				"entity %q: field %q collides with field %q: the query key %q is %q plus the %s operator suffix, the operator wins, so %q can never be filtered; rename one field or set a WireName",
				ent.Config.Name, other, base.field, shadow, base.key, s.Suffix, other)
		}
	}
	return nil
}
