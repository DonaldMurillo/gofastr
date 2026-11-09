package crud

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// projectFromRequest reads the `?fields=` query parameter and returns the
// validated subset of visible columns to SELECT. The primary key and every
// required column are always included regardless of whether the client
// asked for them: the response schema marks required fields required, so a
// projection that dropped them would answer a body that fails the spec it
// is documented by (and a typed client generated from it). ?fields=
// narrows the OPTIONAL columns. Unknown names yield an error so callers
// can return 400 (silent ignoring would mask typos).
//
// Accepts either the DB column name (snake_case) or the JSON-cased name
// (camelCase) so clients can pass whatever they see on the wire.
func (ch *CrudHandler) projectFromRequest(r *http.Request) ([]string, error) {
	return ch.projectFromRequestQ(r.URL.Query())
}

// projectFromRequestQ is the no-reparse variant of projectFromRequest.
// The List handler threads its single url.Values through every helper so
// ?fields= isn't re-parsed per call.
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
	// Required columns ride along with the key: the OpenAPI response
	// schema and every typed client hold them required, so a projected
	// row must carry them (#669). Hidden ones never do; they are absent
	// from the schema too.
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
			// Don't echo the user-supplied name back. A Hidden field's
			// DB name would otherwise leak into the error body, letting
			// a probe confirm "is there a column called secret_key?"
			// just by reading the 400 response.
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
