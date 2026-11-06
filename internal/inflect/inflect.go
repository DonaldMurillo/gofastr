// Package inflect holds the repo's one English singularizer for entity
// display names, shared by the app generator and the admin battery.
package inflect

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// Singular turns a plural English noun into its singular form with a few
// suffix rules: categories → category, statuses → status, boxes → box,
// batches → batch, users → user. Houses and causes retain their final e;
// cookies, movies, ties and the other ieNouns are regular -s plurals, not
// -y stems, and series and species are the same in both numbers.
// Words that already read as singular
// (address, status, analysis) come back unchanged. It is naive by design:
// entity names are identifiers, and an irregular plural (people) is left as
// it is.
func Singular(s string) string {
	if ieNounPlural(s) {
		return s[:len(s)-1]
	}
	if invariantPlural(s) {
		return s
	}
	switch {
	case strings.HasSuffix(s, "ies") && len(s) > 3:
		return s[:len(s)-3] + "y"
	case strings.HasSuffix(s, "sses"),
		strings.HasSuffix(s, "uses") && !strings.HasSuffix(s, "ouses") && !strings.HasSuffix(s, "auses"),
		strings.HasSuffix(s, "xes"),
		strings.HasSuffix(s, "ches"),
		strings.HasSuffix(s, "shes"):
		return s[:len(s)-2]
	case strings.HasSuffix(s, "ss"),
		strings.HasSuffix(s, "us"),
		strings.HasSuffix(s, "is"):
		return s
	case strings.HasSuffix(s, "s"):
		return s[:len(s)-1]
	}
	return s
}

// ieNouns end in -ie in the singular, so their plural is a bare -s. The
// list holds common ones; an -ies word missing from it reads as a -y stem.
var ieNouns = []string{
	"auntie", "birdie", "brownie", "calorie", "cookie", "freebie", "genie",
	"goalie", "hoodie", "lie", "movie", "newbie", "pie", "prairie", "rookie",
	"selfie", "smoothie", "sortie", "tie", "zombie",
}

// ieNounPlural reports whether s ends in the plural of an ieNouns word that
// starts a word of s: at the start, after _, - or a space, or as a
// camelCase part (a capital after a lowercase letter). The boundary keeps
// parties, copies and allies, and PARTies, on the -y rule.
func ieNounPlural(s string) bool {
	return endsInWord(s, ieNouns, "s")
}

// invariantPlural reports whether s ends in a word spelled the same in
// the singular, where the -ies rule would cut it.
func invariantPlural(s string) bool {
	return endsInWord(s, []string{"series", "species"}, "")
}

func endsInWord(s string, words []string, suffix string) bool {
	lower := asciicase.Lower(s)
	for _, w := range words {
		tail := w + suffix
		if !strings.HasSuffix(lower, tail) {
			continue
		}
		start := len(s) - len(tail)
		if start == 0 {
			return true
		}
		if prev := s[start-1]; prev == '_' || prev == '-' || prev == ' ' {
			return true
		}
		// A camelCase part: a capital after a lowercase letter. Inside an
		// uppercase run (PARTies, COPIES) a capital starts no word.
		if prev, first := s[start-1], s[start]; first >= 'A' && first <= 'Z' && prev >= 'a' && prev <= 'z' {
			return true
		}
	}
	return false
}
