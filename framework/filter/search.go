package filter

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// Condition is one WHERE fragment and its args, for qb.Where(c.SQL,
// c.Args...).
type Condition struct {
	SQL  string
	Args []any
}

// MaxSearchTerms is the most tokens a ?q= term contributes; the rest
// are dropped.
const MaxSearchTerms = 8

// SearchConditions returns one Condition per distinct whitespace token
// of term, each an OR over fields of LikeClause, to be ANDed together.
// A blank term returns nil; a term that is not storable text errors
// naming "q".
func SearchConditions(fields []string, term string) ([]Condition, error) {
	term = strings.TrimSpace(term)
	if term == "" || len(fields) == 0 {
		return nil, nil
	}

	if !textsafe.Storable(term) {
		return nil, valueTypeError("q", schema.Text)
	}
	tokens := tokenizeSearch(term)
	if len(tokens) == 0 {
		return nil, nil
	}
	conditions := make([]Condition, 0, len(tokens))
	for _, tok := range tokens {
		parts := make([]string, len(fields))
		args := make([]any, len(fields))
		for i, f := range fields {
			clause, arg := LikeClause(f, i+1, tok)
			parts[i], args[i] = clause, arg
		}
		sql := "(" + strings.Join(parts, " OR ") + ")"
		conditions = append(conditions, Condition{SQL: sql, Args: args})
	}
	return conditions, nil
}

func tokenizeSearch(term string) []string {
	seen := make(map[string]bool)
	var out []string
	for raw := range strings.FieldsSeq(term) {
		t := strings.ToLower(raw)
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
		if len(out) == MaxSearchTerms {
			break
		}
	}
	return out
}
