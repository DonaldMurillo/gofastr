package migrate

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
)

// Keyword names
//
// The migrator writes its DDL with names unquoted, so Postgres folds a
// declared Widgets to widgets and every other reader names the same table.
// A name that is a keyword cannot go bare: CREATE TABLE posts (order TEXT)
// is a syntax error on both engines, and a column named user, end or desc
// booted on SQLite and failed on Postgres. Such a name is written with
// query.QuoteFolded, which names exactly what the folded bare spelling
// would. Every other name stays bare, so the DDL and snapshots of a schema
// without keyword names are byte-identical to those of releases before
// this. The keyword set lives in core/query (query.IsKeyword), shared
// with the statement builders and CRUD, so every query that names a
// column quotes it exactly where this DDL did.

// ddlIdent renders an identifier the caller has validated (query.SafeIdent)
// for DDL: query.QuoteKeyword, each dot-separated part that is a keyword
// quoted and folded, every other part bare. A part already quoted is no
// keyword, so ddlIdent of its own output changes nothing.
func ddlIdent(name string) string { return query.QuoteKeyword(name) }

// ddlIdents is ddlIdent of each name.
func ddlIdents(names []string) []string { return query.QuoteKeywords(names) }

// unquoteDDLIdent reads a name back from DDL the migrator wrote, where a
// keyword part is double-quoted: "user" and app."user" read as user and
// app.user. A validated name holds no quote of its own.
func unquoteDDLIdent(name string) string {
	if !strings.Contains(name, `"`) {
		return name
	}
	return strings.ReplaceAll(name, `"`, "")
}
