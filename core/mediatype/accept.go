// Package mediatype recognizes HTTP media types without imposing strict parsing.
package mediatype

import (
	"iter"
	"slices"
	"strings"
)

// AcceptsAny matches lowercase types against a permissive, comma-separated
// Accept field. It ignores parameters, including quality and quoted values.
func AcceptsAny(field string, types ...string) bool {
	for mediaRange := range acceptRanges(field, false) {
		mediaType, _, _ := strings.Cut(mediaRange, ";")
		if slices.Contains(types, Fold(strings.TrimSpace(mediaType))) {
			return true
		}
	}
	return false
}

// AcceptRanges splits an Accept field on commas outside quoted parameters.
func AcceptRanges(field string) iter.Seq[string] {
	return acceptRanges(field, true)
}

func acceptRanges(field string, honorQuotes bool) iter.Seq[string] {
	return func(yield func(string) bool) {
		start, quoted := 0, false
		for i := 0; i < len(field); i++ {
			switch field[i] {
			case '\\':
				if quoted {
					i++
				}
			case '"':
				if honorQuotes {
					quoted = !quoted
				}
			case ',':
				if quoted {
					continue
				}
				if !yield(field[start:i]) {
					return
				}
				start = i + 1
			}
		}
		yield(field[start:])
	}
}
