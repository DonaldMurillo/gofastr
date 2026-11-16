package crud

import (
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// projectFromRequestQ returns the visible columns ?fields= selects, by
// column or wire name, plus the key and required columns.
func (ch *CrudHandler) projectFromRequestQ(q url.Values) ([]string, error) {
	raw := strings.TrimSpace(q.Get("fields"))
	if raw == "" {
		return ch.visibleFields(), nil
	}

	visible := ch.visibleFields()
	visibleSet := make(map[string]struct{}, len(visible))
	jsonToDB := make(map[string]string, len(visible))
	for _, c := range visible {
		visibleSet[c] = struct{}{}
		jsonToDB[ch.convertKey(c)] = c
	}

	out := append([]string{}, ch.keyCols()...)
	seen := map[string]struct{}{}
	for _, k := range out {
		seen[k] = struct{}{}
	}
	for _, f := range ch.snapshotFields() {
		if !f.Required || f.Hidden {
			continue
		}
		if _, vis := visibleSet[f.Name]; !vis {
			continue
		}
		if _, dup := seen[f.Name]; dup {
			continue
		}
		out = append(out, f.Name)
		seen[f.Name] = struct{}{}
	}

	for p := range strings.SplitSeq(raw, ",") {
		name := strings.TrimSpace(p)
		if name == "" {
			continue
		}
		col := name
		if db, ok := jsonToDB[name]; ok {
			col = db
		}
		if _, ok := visibleSet[col]; !ok {
			return nil, i18n.NewError(reasonUnknownProjection)
		}
		if _, dup := seen[col]; dup {
			continue
		}
		out = append(out, col)
		seen[col] = struct{}{}
	}
	return out, nil
}
