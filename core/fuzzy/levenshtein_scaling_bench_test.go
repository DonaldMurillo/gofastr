package fuzzy

import (
	"strconv"
	"strings"
	"testing"
)

// BenchmarkLevenshteinScaling measures the matcher's cost curve on growing
// inputs. Levenshtein is inherently O(len(a)×len(b)) — that is the
// algorithm, not a bug — but the DoS question for this package is whether
// an attacker-controlled string of unbounded length reaches it. The two
// call sites: framework/filter's nearestField (query-param names, capped at
// 64 bytes by maxSuggestionKeyLen and pinned by TestUnknownFilterKeyIsBounded)
// and the CLI's did-you-mean (argv only). This benchmark is the measured
// evidence for that audit record.
func BenchmarkLevenshteinScaling(b *testing.B) {
	for _, sz := range []int{256, 512, 1024, 2048, 4096} {
		xa := strings.Repeat("a", sz)
		xb := strings.Repeat("b", sz)
		b.Run(strconv.Itoa(sz), func(b *testing.B) {
			for range b.N {
				_ = Levenshtein(xa, xb)
			}
		})
	}
}
