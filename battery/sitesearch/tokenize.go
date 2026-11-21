package sitesearch

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	minTermRunes   = 2
	minPrefixRunes = 3
)

type token struct {
	word       string
	start, end int
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

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
			//gofastr:allow(asciifold) dedup of one word's own parts against its whole; nothing is looked up, and search folds the query the same way
			if utf8.RuneCountInString(p) >= minTermRunes && p != lower {
				out = append(out, p)
			}
		}
	}
	return out
}

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

func sourceWords(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool { return !isWordRune(r) })
}
