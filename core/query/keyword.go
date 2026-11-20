package query

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

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

// IsKeyword reports whether name, ASCII-folded, cannot stand bare as a table, column or index name on SQLite or Postgres.
func IsKeyword(name string) bool {
	_, ok := keywords[asciicase.Lower(name)]
	return ok
}

// QuoteKeyword renders a validated identifier with each keyword part quoted and folded (QuoteFolded) and every other part bare.
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

func identSlot(s string) string {
	if !identRe.MatchString(s) {
		return s
	}
	return QuoteKeyword(s)
}
