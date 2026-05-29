package dsl

import (
	"strings"
	"testing"
)

// FuzzParseDSL drives the recursive-descent query parser with arbitrary
// input. The contract is liveness: parse must always terminate and never
// panic, returning a clean error on malformed input rather than crashing
// the request goroutine. Hangs surface as fuzz timeouts.
func FuzzParseDSL(f *testing.F) {
	for _, s := range []string{
		"", "   ", "Post.where(status=\"x\")",
		"Post.where(a=1).include(b).order(c DESC).limit(10)",
		"Post.where(", "((((((((((", strings.Repeat("a.b(", 200),
		"x.where(k=\x00\n\t)", "after(" + strings.Repeat("=", 100) + ")",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		// Contract: returns, never panics. Result intentionally ignored.
		_, _ = ParseDSL(input)
	})
}
