package query

import (
	"fmt"
	"regexp"
	"strings"
)

// identRe matches a safe SQL identifier: one or more alphanumeric/underscore
// characters, optionally dot-separated (for schema.table). Must start with a
// letter or underscore. Rejects empty strings, quotes, semicolons, spaces, etc.
var identRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*(\.[a-zA-Z_][a-zA-Z0-9_]*)*$`)

// SafeIdent validates that s is a safe SQL identifier and returns it quoted.
// This prevents SQL injection when table or column names must be interpolated
// into queries (they can't be parameterized with $1 placeholders).
//
// Returns an error if s contains characters outside [a-zA-Z0-9_.].
func SafeIdent(s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("query: empty SQL identifier")
	}
	if !identRe.MatchString(s) {
		return "", fmt.Errorf("query: unsafe SQL identifier %q", s)
	}
	return s, nil
}

// MustIdent is like SafeIdent but panics on invalid identifiers.
// Use in init/config-time code where the identifier is a hard-coded constant.
func MustIdent(s string) string {
	ident, err := SafeIdent(s)
	if err != nil {
		panic(err)
	}
	return ident
}

// QuoteIdent wraps an identifier in double-quotes with internal quotes escaped.
// The caller is responsible for validating the identifier first (via SafeIdent).
//
//	QuoteIdent("users")       → "users"
//	QuoteIdent(`weird"name`)  → "weird""name"
func QuoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// SafeQuote validates and quotes a SQL identifier in one step.
func SafeQuote(s string) (string, error) {
	if _, err := SafeIdent(s); err != nil {
		return "", err
	}
	return QuoteIdent(s), nil
}
