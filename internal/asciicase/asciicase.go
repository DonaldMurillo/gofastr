// Package asciicase folds case A–Z only: the folding Postgres applies to
// an unquoted identifier, CSS to a keyword or unit, and the repo to a
// name, table, file extension or key it compares case-insensitively.
//
// strings.ToLower and ToUpper use Unicode case mapping, which maps
// look-alikes onto ASCII (U+212A KELVIN SIGN lowers to k, U+0130 to i,
// U+017F uppers to S), so a homoglyph spelling equals a real name once
// both sides fold. Lower leaves every byte outside A–Z as it is, so a
// look-alike never folds onto an ASCII name. The asciifold analyzer
// reports the Unicode spelling.
package asciicase

// Lower returns s with A–Z mapped to a–z and every other byte unchanged.
func Lower(s string) string {
	for i := 0; i < len(s); i++ {
		if c := s[i]; 'A' <= c && c <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				b[j] = lowerByte(b[j])
			}
			return string(b)
		}
	}
	return s
}

// EqualFold reports whether a and b match when folding ASCII letters only.
func EqualFold[T ~string | ~[]byte](a, b T) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if lowerByte(a[i]) != lowerByte(b[i]) {
			return false
		}
	}
	return true
}

func lowerByte(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}
