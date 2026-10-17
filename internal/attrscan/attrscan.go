// Package attrscan finds data-cui-* attribute values in rendered HTML
// with a byte scan instead of a regexp.
//
// The SSR host scans every page it serves for signal references and
// sheet markers. The patterns those scans used opened with a [\s/]
// boundary class, which gives Go's regexp no literal prefix to jump to,
// so each scan stepped the matcher over every byte of the document:
// about 1 ms per request on the 74 KB examples/meridian homepage, a
// third of the page's serving CPU. Jumping between "data-cui-"
// occurrences with strings.Index does the same work in about 20 µs.
package attrscan

import (
	"iter"
	"strings"
)

const prefix = "data-cui-"

// Values yields, in document order, the value of every attribute named
// "data-cui-"+n (n one of names, tried in order) that sits in an
// attribute position: the byte before it is HTML whitespace or '/', so a
// literal mention in text content never counts. The value is the
// longest run of bytes valid accepts directly after `="`, at least one
// byte long. With closed set, the run must end at a '"'; otherwise it
// may end anywhere.
//
// The matching is the regexp
//
//	[\s/]data-cui-(?:name1|name2|…)="(<valid>+)"   (closed)
//	[\s/]data-cui-(?:name1|name2|…)="(<valid>+)    (not closed)
//
// run as FindAllStringSubmatch, including where the next search resumes
// after a match. valid must refuse '"'.
func Values(html string, names []string, valid func(byte) bool, closed bool) iter.Seq[string] {
	return func(yield func(string) bool) {
		// floor is where the regexp's next search would begin: a match
		// starts at its boundary byte, which must not sit inside the
		// previous match.
		floor := 0
		for i := 0; i < len(html); {
			j := strings.Index(html[i:], prefix)
			if j < 0 {
				return
			}
			at := i + j
			i = at + 1
			if at-1 < floor || !boundary(html[at-1]) {
				continue
			}
			rest := html[at+len(prefix):]
			start := -1
			for _, n := range names {
				if len(rest) > len(n)+1 && rest[:len(n)] == n && rest[len(n)] == '=' && rest[len(n)+1] == '"' {
					start = at + len(prefix) + len(n) + 2
					break
				}
			}
			if start < 0 {
				continue
			}
			end := start
			for end < len(html) && valid(html[end]) {
				end++
			}
			if end == start {
				continue
			}
			next := end
			if closed {
				if end == len(html) || html[end] != '"' {
					continue
				}
				next = end + 1
			}
			if !yield(html[start:end]) {
				return
			}
			i, floor = next, next
		}
	}
}

// boundary reports whether c is RE2's \s (tab, newline, form feed,
// carriage return, space) or '/'.
func boundary(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\f', '\r', '/':
		return true
	}
	return false
}
