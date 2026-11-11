package app

import (
	"strings"
	"testing"
)

// ============================================================================
// htmlToMarkdown: thorough tests
// ============================================================================

func TestHTML2MD_Headings(t *testing.T) {
	tests := []struct {
		name, html, wantSubstring string
	}{
		{"h1", "<h1>Title</h1>", "# Title"},
		{"h2", "<h2>Section</h2>", "## Section"},
		{"h3", "<h3>Sub</h3>", "### Sub"},
		{"h4", "<h4>Deep</h4>", "#### Deep"},
		{"h5", "<h5>Deeper</h5>", "##### Deeper"},
		{"h6", "<h6>Deepest</h6>", "###### Deepest"},
		{"h with attrs", `<h2 class="title">Styled</h2>`, "## Styled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := htmlToMarkdown(tt.html)
			if !strings.Contains(got, tt.wantSubstring) {
				t.Errorf("htmlToMarkdown(%q) = %q, want substring %q", tt.html, got, tt.wantSubstring)
			}
		})
	}
}

func TestHTML2MD_BoldItalic(t *testing.T) {
	tests := []struct {
		name, html, wantSubstring string
	}{
		{"strong", "<strong>bold</strong>", "**bold**"},
		{"b", "<b>bold</b>", "**bold**"},
		{"em", "<em>italic</em>", "*italic*"},
		{"i", "<i>italic</i>", "*italic*"},
		{"nested", "<strong><em>both</em></strong>", "***both***"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := htmlToMarkdown(tt.html)
			if !strings.Contains(got, tt.wantSubstring) {
				t.Errorf("htmlToMarkdown(%q) = %q, want substring %q", tt.html, got, tt.wantSubstring)
			}
		})
	}
}

func TestHTML2MD_Links(t *testing.T) {
	html := `<a href="https://example.com">Click here</a>`
	got := htmlToMarkdown(html)
	want := "[Click here](https://example.com)"
	if !strings.Contains(got, want) {
		t.Errorf("got %q, want substring %q", got, want)
	}
}

func TestHTML2MD_Images(t *testing.T) {
	html := `<img alt="Logo" src="/logo.png" />`
	got := htmlToMarkdown(html)
	want := "![Logo](/logo.png)"
	if !strings.Contains(got, want) {
		t.Errorf("got %q, want substring %q", got, want)
	}
}

func TestHTML2MD_Code(t *testing.T) {
	t.Run("inline code", func(t *testing.T) {
		html := `<code>x++</code>`
		got := htmlToMarkdown(html)
		if !strings.Contains(got, "`x++`") {
			t.Errorf("got %q", got)
		}
	})

	t.Run("code block", func(t *testing.T) {
		html := "<pre><code>func main() {\n\tprintln(\"hi\")\n}</code></pre>"
		got := htmlToMarkdown(html)
		if !strings.Contains(got, "```\nfunc main()") {
			t.Errorf("got %q", got)
		}
		if !strings.Contains(got, "```") {
			t.Errorf("expected closing ```")
		}
	})
}

func TestHTML2MD_Lists(t *testing.T) {
	t.Run("unordered", func(t *testing.T) {
		html := "<ul><li>Apple</li><li>Banana</li></ul>"
		got := htmlToMarkdown(html)
		if !strings.Contains(got, "- Apple") {
			t.Errorf("got %q", got)
		}
		if !strings.Contains(got, "- Banana") {
			t.Errorf("got %q", got)
		}
	})

	t.Run("ordered", func(t *testing.T) {
		html := "<ol><li>First</li><li>Second</li></ol>"
		got := htmlToMarkdown(html)
		if !strings.Contains(got, "1. First") {
			t.Errorf("got %q", got)
		}
		if !strings.Contains(got, "1. Second") {
			t.Errorf("got %q", got)
		}
	})
}

func TestHTML2MD_Table(t *testing.T) {
	html := `<table>
		<tr><th>Name</th><th>Age</th></tr>
		<tr><td>Alice</td><td>30</td></tr>
	</table>`
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "| Name | Age |") {
		t.Errorf("got %q", got)
	}
	if !strings.Contains(got, "| --- | --- |") {
		t.Errorf("expected separator row, got %q", got)
	}
	if !strings.Contains(got, "| Alice | 30 |") {
		t.Errorf("expected data row, got %q", got)
	}
}

func TestHTML2MD_HR(t *testing.T) {
	got := htmlToMarkdown("<hr>")
	if !strings.Contains(got, "---") {
		t.Errorf("got %q", got)
	}
}

func TestHTML2MD_Paragraphs(t *testing.T) {
	html := "<p>Hello</p><p>World</p>"
	got := htmlToMarkdown(html)
	if !strings.Contains(got, "Hello") || !strings.Contains(got, "World") {
		t.Errorf("got %q", got)
	}
	// Should have a blank line between paragraphs
	if !strings.Contains(got, "Hello\n\nWorld") {
		t.Errorf("expected blank line between paragraphs, got %q", got)
	}
}

func TestHTML2MD_ScriptStyleStripped(t *testing.T) {
	html := `<script>alert('xss')</script><p>Visible</p><style>.x{color:red}</style>`
	got := htmlToMarkdown(html)
	if strings.Contains(got, "alert") {
		t.Errorf("script content should be stripped, got %q", got)
	}
	if strings.Contains(got, "color") {
		t.Errorf("style content should be stripped, got %q", got)
	}
	if !strings.Contains(got, "Visible") {
		t.Errorf("visible content should remain, got %q", got)
	}
}

func TestHTML2MD_HTMLEntities(t *testing.T) {
	tests := []struct {
		name, html, want string
	}{
		{"amp", "<p>A &amp; B</p>", "A & B"},
		{"ltgt", "<p>1 &lt; 2</p>", "1 < 2"},
		{"quot", "<p>&quot;hello&quot;</p>", `"hello"`},
		{"numeric", "<p>&#39;hi&#39;</p>", "'hi'"},
		{"hex", "<p>&#x27;hey&#x27;</p>", "'hey'"},
		{"nbsp", "<p>hello&nbsp;world</p>", "hello\u00a0world"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := htmlToMarkdown(tt.html)
			if !strings.Contains(got, tt.want) {
				t.Errorf("htmlToMarkdown(%q) = %q, want substring %q", tt.html, got, tt.want)
			}
		})
	}
}

func TestHTML2MD_EmptyInput(t *testing.T) {
	got := htmlToMarkdown("")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestHTML2MD_ComplexPage(t *testing.T) {
	html := `
		<h1>Demo Page</h1>
		<p>This is a <strong>demo</strong> page with <em>formatting</em>.</p>
		<ul>
			<li>Item one</li>
			<li>Item two</li>
		</ul>
		<pre><code>fmt.Println("hello")</code></pre>
		<p>See <a href="/docs">the docs</a> for more.</p>
	`
	got := htmlToMarkdown(html)

	checks := []string{
		"# Demo Page",
		"**demo**",
		"*formatting*",
		"- Item one",
		"- Item two",
		"```",
		`fmt.Println("hello")`,
		"[the docs](/docs)",
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in output:\n%s", want, got)
		}
	}
}

// ============================================================================
// Regex false-positive: <b> must not match <blockquote>, <body>, etc.
// <i> must not match <img>, <input>, etc.
// ============================================================================

func TestHTML2MD_BoldDoesNotMatchBlockquote(t *testing.T) {
	// The regex <b[^>]*> could match <blockquote> because 'l' is not '>'
	// If the bug exists, <blockquote>text</blockquote> becomes **text**
	html := "<blockquote>This is a quote</blockquote>"
	got := htmlToMarkdown(html)
	if strings.Contains(got, "**This is a quote**") {
		t.Errorf("<blockquote> should NOT be converted to bold, got: %q", got)
	}
	// The text should still be present (stripped)
	if !strings.Contains(got, "This is a quote") {
		t.Errorf("expected quote text to remain, got: %q", got)
	}
}

func TestHTML2MD_ItalicDoesNotMatchImg(t *testing.T) {
	html := `<img src="/photo.jpg" alt="A photo" />`
	got := htmlToMarkdown(html)
	if strings.Contains(got, "*A photo*") {
		t.Errorf("<img> should NOT be converted to italic, got: %q", got)
	}
	// Should be converted to markdown image syntax instead
	if !strings.Contains(got, "![A photo](/photo.jpg)") {
		t.Errorf("expected image markdown, got: %q", got)
	}
}

func TestHTML2MD_BoldDoesNotMatchBody(t *testing.T) {
	// <body>...</body>, the <b[^>]*> matches opening, </b> would NOT match </body>
	// so this is actually safe. But be explicit.
	html := "<body><p>Hello</p></body>"
	got := htmlToMarkdown(html)
	if strings.Contains(got, "**Hello**") {
		t.Errorf("<body> should NOT be converted to bold, got: %q", got)
	}
	if !strings.Contains(got, "Hello") {
		t.Errorf("expected paragraph text, got: %q", got)
	}
}

func TestHTML2MD_BoldDoesNotMatchButton(t *testing.T) {
	html := "<button>Click me</button>"
	got := htmlToMarkdown(html)
	if strings.Contains(got, "**Click me**") {
		t.Errorf("<button> should NOT be converted to bold, got: %q", got)
	}
}

func TestHTML2MD_ItalicDoesNotMatchInput(t *testing.T) {
	html := `<input type="text" value="hello">`
	got := htmlToMarkdown(html)
	if strings.Contains(got, "*hello*") {
		t.Errorf("<input> should NOT be converted to italic, got: %q", got)
	}
}

// A highlighted code block is a <pre> of per-line spans with no <code>
// child (framework/ui's CodeBlock, and every fenced block ui.Markdown
// renders). Its line breaks are the content: flattened like a text node
// the whole program came out as one line of prose.
func TestHTML2MD_PreWithoutCodeKeepsItsLines(t *testing.T) {
	in := `<pre class="fui-code-block__body" tabindex="0"><span class="line"><span class="tk-kw">func</span> main() {
</span><span class="line">	x := a &lt; b <span class="tk-com">// # not a heading</span>
</span><span class="line">}</span></pre>`
	got := htmlToMarkdown(in)
	want := "````\nfunc main() {\n\tx := a < b // # not a heading\n}\n````"
	if got != want {
		t.Errorf("a <pre> without <code> lost its lines:\ngot:  %q\nwant: %q", got, want)
	}
	// One line is flowing text, as before: a diff viewer's per-line
	// <pre> does not become a fence a line.
	if got := htmlToMarkdown(`<div><pre>only line</pre></div>`); got != "only line" {
		t.Errorf("a one-line <pre> should stay text, got %q", got)
	}
}

// A link or an image inside a table cell stays one: the cell escaping
// ran over the already-built markdown and wrote \[Alice\]\(/u/1\).
func TestHTML2MD_TableCellKeepsLinks(t *testing.T) {
	in := `<table><tr><th>Name</th><th>Pic</th></tr>` +
		`<tr><td><a href="/u/1?a=1&amp;b=2|3"><span>Al|ice</span></a> [x](/evil)</td>` +
		`<td><img src="/p.png" alt="P"> one<br>two</td></tr></table>`
	got := htmlToMarkdown(in)
	want := "| Name | Pic |\n| --- | --- |\n" +
		`| [Al\|ice](/u/1?a=1&b=2%7C3) \[x\]\(/evil\) | ![P](/p.png) one two |`
	if got != want {
		t.Errorf("table cell links:\ngot:  %q\nwant: %q", got, want)
	}
}

// A description list's term and description are two elements with
// nothing between them: stripped to text they ran together
// ("NameAlice"), which is what a record's detail page read as.
func TestHTML2MD_DescriptionListSeparatesTermAndValue(t *testing.T) {
	in := `<dl><div><dt class="l">Name</dt><dd class="v">Alice</dd></div><div><dt>Email</dt> <dd>a@b.c</dd></div></dl>`
	got := htmlToMarkdown(in)
	want := "Name: Alice\n\nEmail: a@b.c"
	if got != want {
		t.Errorf("description list:\ngot:  %q\nwant: %q", got, want)
	}
}

// <link> is not a list item.
func TestHTML2MD_LinkTagIsNotAListItem(t *testing.T) {
	if got := htmlToMarkdown(`<p>one</p><link rel="preload" href="/x"><p>two</p>`); got != "one\n\ntwo" {
		t.Errorf("a <link> became a bullet: %q", got)
	}
}

// An image inside a link inside a cell: the link was parked with the
// image's slot in its text, and one restore pass left the slot itself,
// two NUL bytes and an index, in the row: | [\x00a0\x00](/users/7) |.
func TestHTML2MD_TableCellLinkedImage(t *testing.T) {
	in := `<table><tr><th>User</th></tr>` +
		`<tr><td><a href="/users/7"><img alt="avatar" src="/a/7.png"></a></td></tr></table>`
	got := htmlToMarkdown(in)
	want := "| User |\n| --- |\n| [![avatar](/a/7.png)](/users/7) |"
	if got != want {
		t.Errorf("linked image in a cell:\ngot:  %q\nwant: %q", got, want)
	}
	// A NUL in the page is no slot: it cannot name a parked link.
	if got := htmlToMarkdown("<table><tr><td><a href=\"/x\">a\x00a0\x00</a></td></tr></table>"); strings.Contains(got, "\x00") {
		t.Errorf("a NUL in a cell reached the markdown: %q", got)
	}
}

// A line break inside a cell link's text separates its words, as it
// does in the rest of the cell and in a link outside a table: the
// parked text was stripped of the <br> and read "onetwo".
func TestHTML2MD_TableCellLinkBreak(t *testing.T) {
	in := `<table><tr><th>A</th></tr><tr><td><a href="/a">one<br>two</a></td></tr></table>`
	got := htmlToMarkdown(in)
	want := "| A |\n| --- |\n| [one two](/a) |"
	if got != want {
		t.Errorf("break in a cell link:\ngot:  %q\nwant: %q", got, want)
	}
}
