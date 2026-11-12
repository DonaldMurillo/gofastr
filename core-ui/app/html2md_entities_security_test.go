package app

import (
	"strings"
	"testing"
)

// Every decode after the document's control scrub was a way back in for
// &#2; and &#3;, the parked-bracket markers: the image alt was scrubbed,
// but a paragraph, heading, link text, cell, href or code still turned
// them into a raw < and >, and &#1; left a slot-close byte in a cell.
func TestHTML2MD_TextControlEntities(t *testing.T) {
	for name, in := range map[string]string{
		"paragraph": `<p>a&#2;script&#3;b</p>`,
		"linktext":  `<p><a href="/x">a&#2;script&#3;b</a></p>`,
		"heading":   `<h2>a&#2;script&#3;b</h2>`,
		"cell":      `<table><tr><td>a&#2;script&#3;b&#1;c</td></tr></table>`,
		"celllink":  `<table><tr><td><a href="/x&#2;q&#3;y">t</a></td></tr></table>`,
		"href":      `<p><a href="/x&#2;q&#3;y">t</a></p>`,
		"imgsrc":    `<p><img src="/x&#2;q&#3;y.png" alt="a"></p>`,
		"code":      `<p><code>a&#2;q&#3;b</code></p>`,
		"pre":       `<pre><code>a&#2;q&#3;b&#1;c</code></pre>`,
	} {
		got := htmlToMarkdown(in)
		if strings.ContainsAny(got, "<>\x00\x01\x02\x03") {
			t.Errorf("%s: control entity survived as %q", name, got)
		}
	}
}
