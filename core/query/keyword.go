package query

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// Keyword names
//
// AutoMigrate writes its DDL with names unquoted, so Postgres folds a
// declared Widgets to widgets and every bare reader names the same table.
// A name that is a keyword cannot go bare: CREATE TABLE posts (order TEXT)
// and INSERT INTO posts (order) are syntax errors on both engines, a
// column named end or desc fails on Postgres, and SELECT user FROM posts
// on Postgres reads the session user, not the column. Such a name is
// written with QuoteFolded, which names exactly what the folded bare
// spelling would. Every other name stays bare, so the SQL a schema without
// keyword names produces is byte-identical to that of releases before
// this. The migrator (framework/migrate) and the statement builders use
// this one set, so a column the DDL quoted is quoted by every query that
// reads or writes it.

// keywords is every word that cannot stand bare as a table, column or
// index name in the statements the framework writes, on either engine:
// Postgres's reserved and type/function-name keywords (pg_get_keywords()
// catcode R and T, Postgres 16), and the SQLite keywords that fail at
// least one statement the migrator writes (CREATE TABLE, CREATE INDEX,
// ADD/RENAME/DROP COLUMN, REFERENCES, the rebuild's INSERT and RENAME TO),
// found by running each. Keywords either engine takes bare (key, action,
// index on Postgres, status, name) stay bare.
var keywords = func() map[string]struct{} {
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

// IsKeyword reports whether name, ASCII-folded, is a word that cannot
// stand bare as a table, column or index name on SQLite or Postgres
// (order, group, user, end, desc, limit, …). A dotted name is not one;
// QuoteKeyword reads each part.
func IsKeyword(name string) bool {
	_, ok := keywords[asciicase.Lower(name)]
	return ok
}

// QuoteKeyword renders an identifier the caller has validated (SafeIdent)
// for a statement: each dot-separated part that is a keyword quoted and
// folded (QuoteFolded), every other part bare. It is the spelling the
// migrator writes its DDL in, so a query that names a column through it
// names the column the DDL created. A part already quoted is no keyword,
// so QuoteKeyword of its own output changes nothing.
//
//	QuoteKeyword("title")       → title
//	QuoteKeyword("order")       → "order"
//	QuoteKeyword("posts.Group") → posts."group"
func QuoteKeyword(name string) string {
	if !strings.Contains(name, ".") {
		if IsKeyword(name) {
			return QuoteFolded(name)
		}
		return name
	}
	parts := strings.Split(name, ".")
	for i, p := range parts {
		if IsKeyword(p) {
			parts[i] = QuoteFolded(p)
		}
	}
	return strings.Join(parts, ".")
}

// QuoteKeywords is QuoteKeyword of each name, in a new slice.
func QuoteKeywords(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = QuoteKeyword(n)
	}
	return out
}

// identSlot renders a builder's identifier slot (a table, a column, an
// ORDER BY or cursor column) after its scrub: a plain or dotted
// identifier goes through QuoteKeyword, and anything else (*, a quoted
// name, an expression a caller put in the slot) is left as it is.
func identSlot(s string) string {
	if !identRe.MatchString(s) {
		return s
	}
	return QuoteKeyword(s)
}
