package appgen

import (
	"strings"
)

// lowerFirst lowers the first rune of s without touching the rest.
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
