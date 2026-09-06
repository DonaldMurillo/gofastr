package markdown

import (
	"strings"
	"unicode"
)

// Fenced code blocks, per CommonMark's two rules the original three-backtick
// scan got wrong.
//
// The info string is a language followed by options, not one opaque language
// name. Folding the whole line into the class attribute produced
// class="language-go title=&quot;main.go&quot;", which matches no language, so
// writing any fence option silently cost the block its syntax highlighting.
//
// A fence is three OR MORE of the same character, and it closes only on a run
// at least as long. That is the only way to write a markdown example that
// itself contains a ``` block: reading exactly three characters tore such an
// example apart at its first inner fence.

// FenceInfo is a parsed fence info string, the text after a fence's opening run.
type FenceInfo struct {
	// Lang is the first whitespace-delimited token, the language name
	// ("go", "bash"). Empty for a fence with no info string.
	Lang string
	// Meta is everything after Lang, verbatim and trimmed: `title="main.go"`,
	// `{1,3-5}`, `showLineNumbers`. The renderer decides what those mean; this
	// package only carries them through, in the <code> tag's data-meta
	// attribute. Empty for a plain fence.
	Meta string
}

// ParseFenceInfo splits a fence info string into its language and its options.
func ParseFenceInfo(info string) FenceInfo {
	info = strings.TrimSpace(info)
	i := strings.IndexFunc(info, unicode.IsSpace)
	if i < 0 {
		return FenceInfo{Lang: info}
	}
	return FenceInfo{Lang: info[:i], Meta: strings.TrimSpace(info[i:])}
}

// fenceDelim is an opening code fence: the character that runs it, how long
// that run is, and the info string trailing it.
type fenceDelim struct {
	char byte
	n    int
	info string
}

// openFence parses line as an opening code fence.
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
	if n < 3 {
		return fenceDelim{}, false
	}
	return fenceDelim{char: ch, n: n, info: strings.TrimSpace(t[n:])}, true
}

// closes reports whether line closes the block d opened: a run of at least as
// many of the same character, with nothing but whitespace after it. The length
// test is what lets a ```` fence hold a ``` block, and the trailing test is
// what keeps an inner opening fence (```go) from closing the outer one.
func (d fenceDelim) closes(line string) bool {
	t := strings.TrimLeft(line, " ")
	n := 0
	for n < len(t) && t[n] == d.char {
		n++
	}
	return n >= d.n && strings.TrimSpace(t[n:]) == ""
}
