package sitesearch

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// minTermRunes is the shortest term the index keeps and the shortest
// query term the search honors: one character matches nearly every
// page, and the static shards are keyed by a term's first two runes.
const minTermRunes = 2

// token is one word run in a text, with its rune offsets.
type token struct {
	word       string // lowercased
	start, end int    // rune offsets into the source text
}

// isWordRune reports whether r belongs to a word: a Unicode letter or
// digit. The browser client splits on the same classes (\p{L}\p{Nd}).
func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// scan splits text into lowercased word runs, keeping each run's rune
// offsets so an excerpt can mark it.
func scan(text string) []token {
	var out []token
	pos, start := 0, -1
	var b strings.Builder
	flush := func(end int) {
		if start >= 0 {
			out = append(out, token{word: strings.ToLower(b.String()), start: start, end: end})
			b.Reset()
			start = -1
		}
	}
	for _, r := range text {
		if isWordRune(r) {
			if start < 0 {
				start = pos
			}
			b.WriteRune(r)
		} else {
			flush(pos)
		}
		pos++
	}
	flush(pos)
	return out
}

// queryTerms tokenizes a query: lowercased word runs of at least
// minTermRunes, duplicates dropped, in first-seen order.
func queryTerms(q string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range scan(q) {
		if utf8.RuneCountInString(t.word) < minTermRunes || seen[t.word] {
			continue
		}
		seen[t.word] = true
		out = append(out, t.word)
	}
	return out
}

// indexTerms returns the terms one source word contributes to the
// index: the lowercased word, plus the parts of a camelCase or
// PascalCase identifier (CommandPalette also indexes command and
// palette, so a query for "palette" finds ui.CommandPalette).
func indexTerms(word string) []string {
	lower := strings.ToLower(word)
	parts := camelParts(word)
	out := make([]string, 0, 1+len(parts))
	if utf8.RuneCountInString(lower) >= minTermRunes {
		out = append(out, lower)
	}
	if len(parts) > 1 {
		for _, p := range parts {
			p = strings.ToLower(p)
			if utf8.RuneCountInString(p) >= minTermRunes && p != lower {
				out = append(out, p)
			}
		}
	}
	return out
}

// camelParts splits an identifier at lower-to-upper and letter/digit
// boundaries, and before the last capital of an acronym run
// (HTMLParser: HTML, Parser).
func camelParts(word string) []string {
	rs := []rune(word)
	var parts []string
	start := 0
	for i := 1; i < len(rs); i++ {
		prev, cur := rs[i-1], rs[i]
		split := false
		switch {
		case unicode.IsLower(prev) && unicode.IsUpper(cur):
			split = true
		case unicode.IsDigit(prev) != unicode.IsDigit(cur):
			split = true
		case unicode.IsUpper(prev) && unicode.IsUpper(cur) && i+1 < len(rs) && unicode.IsLower(rs[i+1]):
			split = true
		}
		if split {
			parts = append(parts, string(rs[start:i]))
			start = i
		}
	}
	return append(parts, string(rs[start:]))
}

// sourceWords splits text into its word runs with their original case,
// the input indexTerms needs to find camelCase boundaries.
func sourceWords(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool { return !isWordRune(r) })
}
