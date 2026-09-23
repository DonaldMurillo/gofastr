package ui_test

import (
	"regexp"
	"slices"
	"strings"
)

// The external test package's twin of class_token_test.go: whole-token
// class assertions, because a bare strings.Contains(h, "fui-x") passes
// on fui-x__y and a bare Contains(h, "ui-x") passes on fui-x (the fui-
// spelling contains the ui- one).

var tokClassAttrRe = regexp.MustCompile(`class="([^"]*)"`)

// classTokenPresent reports whether any class attribute in h carries
// tok as a whole token.
func classTokenPresent(h, tok string) bool {
	for _, m := range tokClassAttrRe.FindAllStringSubmatch(h, -1) {
		if slices.Contains(strings.Fields(m[1]), tok) {
			return true
		}
	}
	return false
}

// classTokenIndex returns the byte offset of the first class attribute
// in h whose value carries tok as a whole token, or -1.
func classTokenIndex(h, tok string) int {
	for _, m := range tokClassAttrRe.FindAllStringSubmatchIndex(h, -1) {
		if slices.Contains(strings.Fields(h[m[2]:m[3]]), tok) {
			return m[0]
		}
	}
	return -1
}
