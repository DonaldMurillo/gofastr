package crud

import (
	"fmt"
	"net/http"
	"strings"
)

// projectFromRequest reads the `?fields=` query parameter and returns the
// validated subset of visible columns to SELECT. The primary key is always
// included regardless of whether the client asked for it — clients need it
// to do follow-up reads. Unknown names yield an error so callers can return
// 400 (silent ignoring would mask typos).
//
// Accepts either the DB column name (snake_case) or the JSON-cased name
// (camelCase) so clients can pass whatever they see on the wire.
func (ch *CrudHandler) projectFromRequest(r *http.Request) ([]string, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("fields"))
	if raw == "" {
		return ch.VisibleFields(), nil
	}

	visible := ch.VisibleFields()
	visibleSet := make(map[string]struct{}, len(visible))
	jsonToDB := make(map[string]string, len(visible))
	for _, c := range visible {
		visibleSet[c] = struct{}{}
		jsonToDB[ch.convertKey(c)] = c
	}

	out := []string{ch.PrimaryKey}
	seen := map[string]struct{}{ch.PrimaryKey: {}}

	for _, p := range strings.Split(raw, ",") {
		name := strings.TrimSpace(p)
		if name == "" {
			continue
		}
		col := name
		if db, ok := jsonToDB[name]; ok {
			col = db
		}
		if _, ok := visibleSet[col]; !ok {
			return nil, fmt.Errorf("unknown field %q", name)
		}
		if _, dup := seen[col]; dup {
			continue
		}
		out = append(out, col)
		seen[col] = struct{}{}
	}
	return out, nil
}
