package migrate

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// Keyword names
//
// The migrator writes its DDL with names unquoted, so Postgres folds a
// declared Widgets to widgets and every other reader (CRUD's
// query.QuoteFolded) names the same table. A name that is a keyword cannot
// go bare: CREATE TABLE posts (order TEXT) is a syntax error on both
// engines, and a column named user, end or desc booted on SQLite and
// failed on Postgres. Such a name is written with query.QuoteFolded, which
// names exactly what the folded bare spelling would. Every other name
// stays bare, so the DDL and snapshots of a schema without keyword names
// are byte-identical to those of releases before this.

// ddlKeywords is every word that cannot stand bare as a table, column or
// index name in the DDL the migrator writes, on either engine: Postgres's
// reserved and type/function-name keywords (pg_get_keywords() catcode R
// and T, Postgres 16), and the SQLite keywords that fail at least one
// statement the migrator writes (CREATE TABLE, CREATE INDEX, ADD/RENAME/
// DROP COLUMN, REFERENCES, the rebuild's INSERT and RENAME TO), found by
// running each. Keywords either engine takes bare (key, action, index on
// Postgres, status, name) stay bare.
var ddlKeywords = func() map[string]struct{} {
	words := strings.Fields(`
		add all alter analyse analyze and any array as asc asymmetric
		authorization autoincrement between binary both case cast check
		collate collation column commit concurrently constraint create cross
		current_catalog current_date current_role current_schema current_time
		current_timestamp current_user default deferrable delete desc distinct
		do drop else end escape except exists false fetch for foreign freeze
		from full grant group having if ilike in index initially inner insert
		intersect into is isnull join lateral leading left like limit
		localtime localtimestamp natural not nothing notnull null offset on
		only or order outer overlaps placing primary raise references
		returning right select session_user set similar some symmetric
		system_user table tablesample then to trailing transaction true union
		unique update user using values variadic verbose when where window
		with`)
	m := make(map[string]struct{}, len(words))
	for _, w := range words {
		m[w] = struct{}{}
	}
	return m
}()

// isDDLKeyword reports whether name, ASCII-folded, is one of ddlKeywords.
func isDDLKeyword(name string) bool {
	_, ok := ddlKeywords[asciicase.Lower(name)]
	return ok
}

// ddlIdent renders an identifier the caller has validated (query.SafeIdent)
// for DDL: each dot-separated part that is a keyword quoted and folded
// (query.QuoteFolded), every other part bare. A part already quoted is no
// keyword, so ddlIdent of its own output changes nothing.
func ddlIdent(name string) string {
	if !strings.Contains(name, ".") {
		if isDDLKeyword(name) {
			return query.QuoteFolded(name)
		}
		return name
	}
	parts := strings.Split(name, ".")
	for i, p := range parts {
		if isDDLKeyword(p) {
			parts[i] = query.QuoteFolded(p)
		}
	}
	return strings.Join(parts, ".")
}

// ddlIdents is ddlIdent of each name.
func ddlIdents(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = ddlIdent(n)
	}
	return out
}

// unquoteDDLIdent reads a name back from DDL the migrator wrote, where a
// keyword part is double-quoted: "user" and app."user" read as user and
// app.user. A validated name holds no quote of its own.
func unquoteDDLIdent(name string) string {
	if !strings.Contains(name, `"`) {
		return name
	}
	return strings.ReplaceAll(name, `"`, "")
}
