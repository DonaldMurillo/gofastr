package fuzzy

import (
	"strconv"
	"strings"
	"testing"
)

// BenchmarkLevenshteinScaling measures cost on growing inputs. The RBAC admin grant POST (default /admin/rbac/_grant)
// can pass a Wildcard caller's submitted permission to CheckGrant, with a 1 MiB request body limit.
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
