package markdown

import (
	"strings"
	"unicode"
)

// FenceInfo is a parsed fence info string, the text after a fence's opening run.
type FenceInfo struct {
	// Lang is the first token, empty when the info string has no language.
	Lang string
	// Meta is the rest, trimmed, carried through in data-meta.
	Meta string
}

// ParseFenceInfo splits a fence info string into its language and its options.
func ParseFenceInfo(info string) FenceInfo {
	info = strings.TrimSpace(info)
	i := strings.IndexFunc(info, unicode.IsSpace)
	first := info
	if i >= 0 {
		first = info[:i]
	}
	if strings.Contains(first, "=") || strings.HasPrefix(first, "{") {
		return FenceInfo{Meta: info}
	}
	if i < 0 {
		return FenceInfo{Lang: info}
	}
	return FenceInfo{Lang: first, Meta: strings.TrimSpace(info[i:])}
}

type fenceDelim struct {
	char   byte
	n      int
	info   string
	indent int
}

func openFence(line string) (fenceDelim, bool) {
	t := strings.TrimLeft(line, " ")
	if t == "" || (t[0] != '`' && t[0] != '~') {
		return fenceDelim{}, false
	}
	ch := t[0]
	n := 0
	for n < len(t) && t[n] == ch {
		n++
	}
	if n < 3 || ch == '`' && strings.ContainsRune(t[n:], '`') {
		return fenceDelim{}, false
	}
	return fenceDelim{char: ch, n: n, info: strings.TrimSpace(t[n:]), indent: len(line) - len(t)}, true
}

func (d fenceDelim) closes(line string) bool {
	t := strings.TrimLeft(line, " ")
	n := 0
	for n < len(t) && t[n] == d.char {
		n++
	}
	return n >= d.n && strings.TrimSpace(t[n:]) == ""
}
