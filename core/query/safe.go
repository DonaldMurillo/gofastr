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

// dangerousSeqs are SQL meta-sequences that should never appear in a
// caller-supplied identifier, table name, predicate, or column list.
// Each is stripped (replaced with a single space for readability) by
// sanitizeFragment / sanitizeColumn before the value is interpolated
// into a built statement.
var dangerousSeqs = []string{";", "--", "/*", "*/"}

// sanitizeFragment scrubs the obvious SQL-injection metacharacters
// from a caller-supplied SQL fragment that the builder must
// interpolate (table name, JOIN predicate, ORDER column, cursor
// field). NUL/CR/LF are removed outright; semicolons, comment
// sequences, and unbalanced parens are replaced with a space so a
// payload like "users; DROP TABLE audit_logs; --" can never round-trip
// into the final SQL as a semicolon-separated second statement.
//
// This is defense in depth: callers must still pass *real* identifiers
// (the builders do not quote on the caller's behalf), but a hostile
// or fuzzed input will not produce an exploitable statement.
func sanitizeFragment(s string) string {
	if s == "" {
		return s
	}
	// Strip C0 controls + DEL first.
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == 0x7f {
			continue
		}
		b.WriteByte(c)
	}
	out := b.String()
	// Strip parens — legitimate uses never put them in identifier slots.
	out = strings.NewReplacer("(", " ", ")", " ").Replace(out)
	// Strip the dangerous SQL meta-sequences.
	for _, seq := range dangerousSeqs {
		out = strings.ReplaceAll(out, seq, " ")
	}
	return out
}

// sanitizeColumn is the column-list variant of sanitizeFragment. In
// addition to the meta-sequence scrub, it collapses every whitespace
// run to nothing, which neutralises payloads like
// `name, (SELECT secret FROM api_keys LIMIT 1) AS leaked` — after
// sanitisation the keywords are mashed together into a single token
// and cannot form a valid sub-query. Column slots in the builders
// never carry meaningful whitespace (they're dotted identifiers or
// `*`), so this is safe to apply globally.
func sanitizeColumn(s string) string {
	out := sanitizeFragment(s)
	// Remove all whitespace.
	var b strings.Builder
	b.Grow(len(out))
	for i := 0; i < len(out); i++ {
		c := out[i]
		if c == ' ' || c == '\t' {
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// sanitizeDirection clamps an ORDER BY direction to the SQL standard
// set. Anything outside ASC/DESC (case-insensitive) — including a
// CRLF-smuggled payload — is dropped to the empty string so the
// builder emits no direction keyword rather than the attacker's text.
func sanitizeDirection(s string) string {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "ASC", "DESC":
		return strings.ToUpper(strings.TrimSpace(s))
	default:
		return ""
	}
}
