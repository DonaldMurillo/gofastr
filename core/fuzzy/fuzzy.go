// Package fuzzy holds string-similarity helpers shared by the framework and the CLI.
package fuzzy

// Levenshtein returns the bytewise edit distance between a and b.
func Levenshtein(a, b string) int {
	return levenshtein([]byte(a), []byte(b))
}

// LevenshteinRunes returns the edit distance in Unicode code points.
func LevenshteinRunes(a, b string) int {
	return levenshtein([]rune(a), []rune(b))
}

func levenshtein[T comparable](a, b []T) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	if la < lb {
		a, b = b, a
		la, lb = lb, la
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
			curr[j] = min(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}
