// Package fuzzy holds small string-similarity helpers shared across the
// codebase. It is dependency-free and low-level so both the framework and
// the CLI (which cannot import each other) can reuse one implementation
// instead of maintaining copies.
package fuzzy

// Levenshtein returns the edit distance between a and b — the minimum number
// of single-character insertions, deletions, or substitutions to turn one
// into the other. Inputs are compared bytewise; for ASCII identifiers
// (command names, field names) that is equivalent to rune distance.
//
// It uses two rolling rows, so allocation and memory are O(len(b)); the
// identifier lists it runs against are tiny.
func Levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
