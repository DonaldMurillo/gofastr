package markdown

import (
	"runtime"
	"strings"
	"testing"
)

// TestNestedLinksShareIndex: nested link text and emphasis content are
// windows of one block and share its bracket index. Rebuilding the index
// at every nesting level allocated about 2 GB for 200,000 nested links
// (1 MB of input) and ran eight times slower than the scan it replaced.
func TestNestedLinksShareIndex(t *testing.T) {
	n := 200000
	in := strings.Repeat("[", n) + "x" + strings.Repeat("](u)", n)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	RenderHTML(in)
	runtime.ReadMemStats(&after)
	if got, max := after.TotalAlloc-before.TotalAlloc, uint64(400*len(in)); got > max {
		t.Fatalf("nested links allocated %d MB for %d KB of input; want under %d MB", got>>20, len(in)>>10, max>>20)
	}
}

// TestLinkWindowStopsAtItsEnd: inside link text, a ')' past the text
// belongs to the outer link, so the shared index must not hand it to the
// window. A link binds tighter than emphasis (CommonMark), so a '*' in a
// link's text or URL closes no emphasis opened before the link.
func TestLinkWindowStopsAtItsEnd(t *testing.T) {
	for in, want := range map[string]string{
		"[[a](b](c)":  `<p><a href="c">[a](b</a></p>`,
		"*[a*](u)":    `<p>*<a href="u">a*</a></p>`,
		"*[a](b*)":    `<p>*<a href="b*">a</a></p>`,
		"*[*a*](b)*":  `<p><em><a href="b"><em>a</em></a></em></p>`,
		"[[a](b)](c)": `<p><a href="c"><a href="b">a</a></a></p>`,
	} {
		if got := strings.TrimSpace(string(RenderHTML(in))); got != want {
			t.Errorf("RenderHTML(%q) = %s, want %s", in, got, want)
		}
	}
}
