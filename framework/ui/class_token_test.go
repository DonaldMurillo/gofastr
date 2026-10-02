package ui

import (
	"regexp"
	"slices"
	"strings"
)

// Whole-token class assertions. A bare strings.Contains(h, "fui-hero")
// passes on fui-hero-split, and a bare Contains(h, "ui-foo") passes on
// fui-foo (the fui- spelling contains the ui- one): both slipped a
// class rename past these suites more than once. Every test that names
// a class asserts it as a token inside a class="…" value instead.

// tokClassAttrRe matches the class attributes render.Tag emits. Single
// quotes and unquoted values do not occur in framework output.
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
// in h whose value carries tok as a whole token, or -1. Ordering
// assertions keep their meaning: the offset names the element.
func classTokenIndex(h, tok string) int {
	for _, m := range tokClassAttrRe.FindAllStringSubmatchIndex(h, -1) {
		if slices.Contains(strings.Fields(h[m[2]:m[3]]), tok) {
			return m[0]
		}
	}
	return -1
}

// classTokenCount counts every class-attribute occurrence of tok as a
// whole token.
func classTokenCount(h, tok string) int {
	n := 0
	for _, m := range tokClassAttrRe.FindAllStringSubmatch(h, -1) {
		for _, t := range strings.Fields(m[1]) {
			if t == tok {
				n++
			}
		}
	}
	return n
}

// classTokenPrefixPresent reports whether any class token in h starts
// with prefix — the shape a "no variant class at all" negative needs,
// where naming each variant would let a new one slip past.
func classTokenPrefixPresent(h, prefix string) bool {
	for _, m := range tokClassAttrRe.FindAllStringSubmatch(h, -1) {
		for _, tok := range strings.Fields(m[1]) {
			if strings.HasPrefix(tok, prefix) {
				return true
			}
		}
	}
	return false
}
