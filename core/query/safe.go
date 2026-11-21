package query

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

var identRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*(\.[a-zA-Z_][a-zA-Z0-9_]*)*$`)

// SafeIdent returns s unchanged when it is dot-separated parts of [A-Za-z_][A-Za-z0-9_]*, and an error otherwise.
func SafeIdent(s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("query: empty SQL identifier")
	}
	if !identRe.MatchString(s) {
		return "", fmt.Errorf("query: unsafe SQL identifier %q", s)
	}
	return s, nil
}

// SafeColumns validates each column through SafeIdent and wraps the first
// error as `invalid <label> "<name>": <why>`.
func SafeColumns(cols []string, label string) ([]string, error) {
	out := make([]string, len(cols))
	for i, c := range cols {
		s, err := SafeIdent(c)
		if err != nil {
			return nil, fmt.Errorf("invalid %s %q: %w", label, c, err)
		}
		out[i] = s
	}
	return out, nil
}

// MaxIdentBytes is the longest identifier Postgres keeps (NAMEDATALEN-1).
const MaxIdentBytes = 63

// FitIdent returns name when it fits MaxIdentBytes, else its first 54 bytes, "_" and 8 hex digits of the SHA-256 of the folded whole.
func FitIdent(name string) string {
	if len(name) <= MaxIdentBytes {
		return name
	}
	return name[:MaxIdentBytes-9] + "_" + identHash(name)
}

func identHash(name string) string {
	folded := []byte(name)
	for i, c := range folded {
		if 'A' <= c && c <= 'Z' {
			folded[i] = c + ('a' - 'A')
		}
	}
	sum := sha256.Sum256(folded)
	return hex.EncodeToString(sum[:4])
}

// ObjectName is the name Postgres gives an object it names itself, name1_name2_label, trimming the longer part until it fits MaxIdentBytes.
func ObjectName(name1, name2, label string) string {
	avail := MaxIdentBytes - len(label) - 2
	n1, n2 := len(name1), len(name2)
	for n1+n2 > avail {
		if n1 > n2 {
			n1--
		} else {
			n2--
		}
	}
	return name1[:n1] + "_" + name2[:n2] + "_" + label
}

// FitObjectName is ObjectName for an object this code creates, with a hash of the length-prefixed parts before the label when the name is too long.
func FitObjectName(name1, name2, label string) string {
	whole := name1 + "_" + name2 + "_" + label
	if len(whole) <= MaxIdentBytes {
		return whole
	}
	parts := fmt.Sprintf("%d:%s|%d:%s|%d:%s", len(name1), name1, len(name2), name2, len(label), label)
	return ObjectName(name1, name2, identHash(parts)+"_"+label)
}

// MustIdent is SafeIdent that panics on an invalid identifier.
func MustIdent(s string) string {
	ident, err := SafeIdent(s)
	if err != nil {
		panic(err)
	}
	return ident
}

// QuoteIdent wraps an identifier in double quotes, doubling any quote inside; validate it first.
//
//	QuoteIdent("users")       → "users"
//	QuoteIdent(`weird"name`)  → "weird""name"
func QuoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// QuoteFolded lowercases ASCII letters and quotes each dot-separated part, naming what unquoted DDL created.
//
//	QuoteFolded("Widgets")        → "widgets"
//	QuoteFolded("app.UserEvents") → "app"."userevents"
func QuoteFolded(s string) string {
	parts := strings.Split(s, ".")
	for i, part := range parts {
		b := []byte(part)
		for j, c := range b {
			if 'A' <= c && c <= 'Z' {
				b[j] = c + ('a' - 'A')
			}
		}
		parts[i] = QuoteIdent(string(b))
	}
	return strings.Join(parts, ".")
}

// QuoteIndexName keeps the part after the last dot, fits it with FitIdent and quotes it with QuoteFolded.
//
//	QuoteIndexName("app.UserEvents_idx") → "userevents_idx"
func QuoteIndexName(s string) string {
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		s = s[i+1:]
	}
	return QuoteFolded(FitIdent(s))
}

// IndexOn renders "<index> ON <table>" for CREATE INDEX, moving table's schema onto the index name when sqlite is true.
//
//	IndexOn(false, `"x_idx"`, `"main"."events"`) → "x_idx" ON "main"."events"
//	IndexOn(true, `"x_idx"`, `"main"."events"`)  → "main"."x_idx" ON "events"
func IndexOn(sqlite bool, name, table string) string {
	if i := strings.LastIndexByte(table, '.'); i >= 0 && sqlite {
		return table[:i] + "." + name + " ON " + table[i+1:]
	}
	return name + " ON " + table
}

// SQLitePragma renders PRAGMA fn(table) with table's schema moved onto the pragma.
//
//	SQLitePragma("table_info", `"main"."events"`) → PRAGMA "main".table_info("events")
func SQLitePragma(fn, table string) string {
	if i := strings.LastIndexByte(table, '.'); i >= 0 {
		return "PRAGMA " + table[:i] + "." + fn + "(" + table[i+1:] + ")"
	}
	return "PRAGMA " + fn + "(" + table + ")"
}

// SafeQuote validates and quotes a SQL identifier in one step.
func SafeQuote(s string) (string, error) {
	if _, err := SafeIdent(s); err != nil {
		return "", err
	}
	parts := strings.Split(s, ".")
	for i, part := range parts {
		parts[i] = QuoteIdent(part)
	}
	return strings.Join(parts, "."), nil
}

var reservedSQLIdents = map[string]struct{}{
	"select": {}, "insert": {}, "update": {}, "delete": {},
	"drop": {}, "create": {}, "table": {}, "from": {}, "where": {},
	"users": {}, "user": {}, "migrations": {}, "sessions": {}, "accounts": {},
}

// ReservedIdent reports whether name, case-insensitively, is a SQL keyword or common system-table name refused as a store's table.
func ReservedIdent(name string) bool {
	_, bad := reservedSQLIdents[strings.ToLower(name)]
	return bad
}

// SafeTableName reports whether name is a dotless SafeIdent of at most 64 bytes that is not ReservedIdent.
func SafeTableName(name string) bool {
	if name == "" || len(name) > 64 || strings.Contains(name, ".") {
		return false
	}
	if _, err := SafeIdent(name); err != nil {
		return false
	}
	return !ReservedIdent(name)
}

var dangerousSeqs = []string{";", "--", "/*", "*/"}

func sanitizeFragment(s string) string {
	if fragmentIsClean(s) {
		return s
	}
	return scrubFragment(s)
}

func scrubFragment(s string) string {
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
	out = parenReplacer.Replace(out)
	for _, seq := range dangerousSeqs {
		out = strings.ReplaceAll(out, seq, " ")
	}
	return out
}

var parenReplacer = strings.NewReplacer("(", " ", ")", " ")

func fragmentIsClean(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == 0x7f || c == '(' || c == ')' || c == ';' {
			return false
		}
		if i+1 < len(s) {
			switch next := s[i+1]; {
			case c == '-' && next == '-', c == '/' && next == '*', c == '*' && next == '/':
				return false
			}
		}
	}
	return true
}

func sanitizeColumn(s string) string {
	out := sanitizeFragment(s)
	if !strings.ContainsAny(out, " \t") {
		return out
	}
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

func sanitizeDirection(s string) string {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "ASC", "DESC":
		return strings.ToUpper(strings.TrimSpace(s))
	default:
		return ""
	}
}
