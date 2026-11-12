package app

import (
	"strings"
	"testing"
)

// Every decode after the document's control scrub was a way back in for
// &#2; and &#3;, the parked-bracket markers: the image alt was scrubbed,
// but a paragraph, heading, link text, cell, href or code still turned
// them into a raw < and >, and &#1; left a slot-close byte in a cell.
// &#0; decodes to U+FFFD, never a slot-open byte. Each fixture is held to
// its whole output, so a fix that drops the text around an entity fails
// as surely as one that lets a marker through.
func TestHTML2MD_TextControlEntities(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"paragraph": {`<p>a&#2;script&#3;b&#0;c</p>`, "ascriptb\uFFFDc"},
		"linktext":  {`<p><a href="/x">a&#2;script&#3;b&#0;c</a></p>`, "[ascriptb\uFFFDc](/x)"},
		"heading":   {`<h2>a&#2;script&#3;b&#0;c</h2>`, "## ascriptb\uFFFDc"},
		"cell":      {`<table><tr><td>a&#2;script&#3;b&#1;c&#0;d</td></tr></table>`, "| ascriptbc\uFFFDd |\n| --- |"},
		"celllink":  {`<table><tr><td><a href="/x&#2;q&#3;y&#0;z">t</a></td></tr></table>`, "| [t](/xqy\uFFFDz) |\n| --- |"},
		"href":      {`<p><a href="/x&#2;q&#3;y&#0;z">t</a></p>`, "[t](/xqy\uFFFDz)"},
		"imgsrc":    {`<p><img src="/x&#2;q&#3;y&#0;z.png" alt="a"></p>`, "![a](/xqy\uFFFDz.png)"},
		"code":      {`<p><code>a&#2;q&#3;b&#0;c</code></p>`, "`aqb\uFFFDc`"},
		"pre":       {`<pre><code>a&#2;q&#3;b&#1;c&#0;d</code></pre>`, "````\naqbc\uFFFDd\n````"},
		// A forged fence slot spelled with entities stays text.
		"forgedslot": {`<p>&#0;f0&#1; &amp; x</p>`, "\uFFFDf0 & x"},
	} {
		got := htmlToMarkdown(tc.in)
		if strings.ContainsAny(got, "<>\x00\x01\x02\x03") {
			t.Errorf("%s: control entity survived as %q", name, got)
		}
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

// A <pre> inside a paragraph leaves its fence slot in the same text run
// as the words around it. With an entity in that run, the decode scrubbed
// the slot's own marker bytes: the block vanished and "f0" was printed.
func TestHTML2MD_FenceSlotBesideEntity(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"<p>a &amp; b<pre>x\ny</pre>c</p>", "a & b\n````\nx\ny\n````\nc"},
		{"<p>a &amp; b</p><pre>x\ny</pre>c &amp; d", "a & b\n\n````\nx\ny\n````\nc & d"},
	} {
		if got := htmlToMarkdown(tc.in); got != tc.want {
			t.Errorf("got  %q\nwant %q", got, tc.want)
		}
	}
}
