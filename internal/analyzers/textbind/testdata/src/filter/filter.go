// Package filter reconstructs framework/filter's search and coercion
// around 145b37ab and 155cafcd. No request is read here: the package
// only exports summaries, and the handlers in package a are where the
// request text enters.
package filter

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// Condition is filter.Condition: a SQL fragment and its binds.
type Condition struct {
	SQL  string
	Args []any
}

var errValue = errors.New("value is not text")

// SearchConditionsPreFix is SearchConditions before 145b37ab: the term
// is trimmed, tokenised, lowered and escaped, and every one of those
// keeps its bytes, so a NUL or 0xff reaches the LIKE bind.
func SearchConditionsPreFix(fields []string, term string) []Condition { // want SearchConditionsPreFix:`sink=10 return=0 check=0`
	term = strings.TrimSpace(term)
	if term == "" || len(fields) == 0 {
		return nil
	}
	tokens := tokenize(term)
	conds := make([]Condition, 0, len(tokens))
	for _, tok := range tokens {
		pattern := "%" + escapeLike(strings.ToLower(tok)) + "%"
		parts := make([]string, len(fields))
		args := make([]any, len(fields))
		for i, f := range fields {
			parts[i] = "LOWER(" + f + ") LIKE ?"
			args[i] = pattern
		}
		conds = append(conds, Condition{SQL: strings.Join(parts, " OR "), Args: args})
	}
	return conds
}

// SearchConditions is the fix: a term that is not storable text is
// refused before it is tokenised.
func SearchConditions(fields []string, term string) ([]Condition, error) { // want SearchConditions:`sink=0 return=0 check=10 `
	term = strings.TrimSpace(term)
	if term == "" || len(fields) == 0 {
		return nil, nil
	}
	if !storableText(term) {
		return nil, errValue
	}
	var conds []Condition
	for _, tok := range strings.Fields(term) {
		conds = append(conds, Condition{SQL: "x LIKE ?", Args: []any{"%" + escapeLike(tok) + "%"}})
	}
	return conds, nil
}

// tokenize is tokenizeSearch: a range over an iterator keeps the
// term's bytes in every token.
func tokenize(term string) []string { // want tokenize:`return=1 `
	seen := map[string]bool{}
	var out []string
	for raw := range strings.FieldsSeq(term) {
		if t := strings.TrimSpace(raw); t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

func escapeLike(s string) string { // want escapeLike:`return=1 `
	return strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(s)
}

// storableText is the check the fixes introduced, credited by its body.
func storableText(raw string) bool { // want storableText:`check=1 `
	return utf8.ValidString(raw) && strings.IndexByte(raw, 0) < 0
}

// CoerceValue delegates to storableText, so it is a check of raw too
// (155cafcd): a caller that keeps its error is credited.
func CoerceValue(field string, raw string) (any, error) { // want CoerceValue:`check=10 `
	if !storableText(raw) {
		return nil, errValue
	}
	return raw, nil
}

// LooksValid is named like a check and is not one: it never tests
// utf8 validity or NUL, so it clears nothing.
func LooksValid(raw string) bool {
	return len(raw) < 256
}
