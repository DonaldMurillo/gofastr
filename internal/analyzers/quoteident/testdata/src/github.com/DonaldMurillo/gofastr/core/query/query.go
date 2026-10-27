// Package query stubs core/query for the quoteident fixture.
package query

import "strings"

// QuoteIdent keeps case. Inside core/query it is the building block of
// QuoteFolded, so the rule stays quiet here.
func QuoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// QuoteFolded folds ASCII case and quotes each dotted part.
func QuoteFolded(s string) string {
	parts := strings.Split(strings.ToLower(s), ".")
	for i, p := range parts {
		parts[i] = QuoteIdent(p)
	}
	return strings.Join(parts, ".")
}
