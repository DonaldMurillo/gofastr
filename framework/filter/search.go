package filter

import (
	"strings"
)

// Condition is a single WHERE fragment (SQL + args) produced by
// SearchConditions. Convert to a hook.WhereClause and append via
// qb.Where(c.SQL, c.Args…) — the query builder wraps each Where in
// parens and AND-composes them, so multi-field search clauses combine
// safely with owner/tenant/soft-delete scopes.
type Condition struct {
	SQL  string
	Args []any
}

// MaxSearchTerms bounds the number of tokens a single ?q= value expands
// to. A search term is one whitespace-delimited token; beyond this cap,
// extra tokens are dropped. Keeps statement size bounded against an
// adversarial input.
const MaxSearchTerms = 8

// SearchConditions builds a slice of AND-composed search conditions from
// a free-text term over the given DB column names. Each whitespace-
// delimited token produces one Condition whose SQL is a parenthesized
// OR-group: (LOWER(f1) LIKE $1 ESCAPE '\' OR LOWER(f2) LIKE $2 ESCAPE
// '\'). Every Condition must match (AND); within one Condition, any field
// may match (OR). A blank/whitespace-only term returns nil (no
// conditions).
//
// Case contract: LOWER() is ASCII-only on SQLite and locale-aware on
// Postgres, so matching is ASCII-case-insensitive everywhere. Unicode
// case folding is a Postgres bonus. The token itself is lowercased before
// building the LIKE pattern so the comparison is consistent across
// dialects.
//
// LIKE metacharacters (%, _, \) in each token are escaped via the
// existing escaper so they match literally — a user searching for "50%"
// finds rows containing "50%", not every row with any character sequence.
// The args are ordered to match the $N placeholders left-to-right; the
// crud query builder renumbers $N on Build.
func SearchConditions(fields []string, term string) []Condition {
	term = strings.TrimSpace(term)
	if term == "" || len(fields) == 0 {
		return nil
	}
	tokens := tokenizeSearch(term)
	if len(tokens) == 0 {
		return nil
	}
	if len(tokens) > MaxSearchTerms {
		tokens = tokens[:MaxSearchTerms]
	}
	conditions := make([]Condition, 0, len(tokens))
	for _, tok := range tokens {
		lowered := strings.ToLower(tok)
		pattern := escapeLikePattern(lowered)
		parts := make([]string, len(fields))
		args := make([]any, len(fields))
		for i, f := range fields {
			parts[i] = "LOWER(" + f + `) LIKE $` + itoa(i+1) + ` ESCAPE '\'`
			args[i] = pattern
		}
		sql := "(" + strings.Join(parts, " OR ") + ")"
		conditions = append(conditions, Condition{SQL: sql, Args: args})
	}
	return conditions
}

// tokenizeSearch splits term on whitespace, dedupes, and drops empties.
func tokenizeSearch(term string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, raw := range strings.Fields(term) {
		t := strings.TrimSpace(raw)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// itoa is a tiny strconv.Itoa alias to keep imports minimal.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
