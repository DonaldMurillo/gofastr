// Package a holds the quoteident fixture reduced from the 2026-10-26 bug
// bash: battery/auth's entity store quoted the declared users table with
// QuoteIdent, so a table declared "AppUsers" (created folded, appusers)
// or "auth.users" (one identifier with a dot in it) named nothing on
// Postgres.
package a

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/query"
)

type store struct{ table string }

// Pre-fix: a declared name keeps its case and its dot.
func (s *store) count() string {
	return fmt.Sprintf("SELECT count(*) FROM %s", query.QuoteIdent(s.table)) // want `query.QuoteIdent keeps the name's case`
}

// A function value is the same quoting.
var quote = query.QuoteIdent // want `query.QuoteIdent keeps the name's case`

// The fix.
func (s *store) countFixed() string {
	return fmt.Sprintf("SELECT count(*) FROM %s", query.QuoteFolded(s.table))
}

// A name read back from the catalog is the stored spelling already.
func dropStored(stored string) string {
	//gofastr:allow(quoteident) stored is read from information_schema, already in its stored case
	return "DROP TABLE " + query.QuoteIdent(stored)
}
