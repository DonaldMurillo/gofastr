package markdown

import (
	"strings"
	"testing"
)

func TestHeadingsEmitIDs(t *testing.T) {
	got := string(RenderHTML("# Hello World\n\n## Sub Section\n"))
	if !strings.Contains(got, `<h1 id="hello-world">Hello World</h1>`) {
		t.Errorf("missing H1: %s", got)
	}
	if !strings.Contains(got, `<h2 id="sub-section">Sub Section</h2>`) {
		t.Errorf("missing H2: %s", got)
	}
}

func TestParagraphAndInline(t *testing.T) {
	got := string(RenderHTML("This is **bold** and *italic* and `code`.\n"))
	want := "<p>This is <strong>bold</strong> and <em>italic</em> and <code>code</code>.</p>\n"
	if got != want {
		t.Errorf("paragraph mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestFencedCodeEscapesHTML(t *testing.T) {
	got := string(RenderHTML("```go\nfmt.Println(\"<hi>\")\n```\n"))
	if !strings.Contains(got, `<pre tabindex="0"><code class="language-go">`) {
		t.Errorf("missing language class: %s", got)
	}
	if !strings.Contains(got, "&lt;hi&gt;") {
		t.Errorf("html not escaped: %s", got)
	}
}

func TestUnorderedAndOrderedLists(t *testing.T) {
	ul := string(RenderHTML("- one\n- two\n- three\n"))
	if !strings.Contains(ul, "<ul>") || strings.Count(ul, "<li>") != 3 {
		t.Errorf("ul wrong: %s", ul)
	}
	ol := string(RenderHTML("1. first\n2. second\n"))
	if !strings.Contains(ol, "<ol>") || strings.Count(ol, "<li>") != 2 {
		t.Errorf("ol wrong: %s", ol)
	}
}

func TestBlockquoteRecursesIntoBlock(t *testing.T) {
	got := string(RenderHTML("> quoted **strong**\n> still quoted\n"))
	if !strings.Contains(got, "<blockquote>") || !strings.Contains(got, "<strong>strong</strong>") {
		t.Errorf("blockquote: %s", got)
	}
}

func TestHorizontalRule(t *testing.T) {
	got := string(RenderHTML("before\n\n---\n\nafter\n"))
	if !strings.Contains(got, "<hr>") {
		t.Errorf("hr missing: %s", got)
	}
}

func TestLinkAndImage(t *testing.T) {
	got := string(RenderHTML("[GoFastr](https://example.com) and ![logo](/img.png)\n"))
	if !strings.Contains(got, `<a href="https://example.com">GoFastr</a>`) {
		t.Errorf("link: %s", got)
	}
	if !strings.Contains(got, `<img src="/img.png" alt="logo">`) {
		t.Errorf("image: %s", got)
	}
}

func TestTableWithAlignment(t *testing.T) {
	src := `| Name | Score |
|:-----|------:|
| A    |    10 |
| B    |    20 |
`
	got := string(RenderHTML(src))
	if !strings.Contains(got, "<table>") || !strings.Contains(got, "<thead>") || !strings.Contains(got, "<tbody>") {
		t.Errorf("table missing: %s", got)
	}
	// Class-based alignment (CSP-safe) — strict-CSP hosts block inline
	// style attributes; the rendered output carries .md-align-<dir>
	// instead, mapped to text-align by the host stylesheet.
	if !strings.Contains(got, `class="md-align-left"`) || !strings.Contains(got, `class="md-align-right"`) {
		t.Errorf("alignment missing: %s", got)
	}
	if strings.Count(got, "<tr>") != 3 {
		t.Errorf("rows wrong: %s", got)
	}
}

func TestFrontmatterParsing(t *testing.T) {
	src := "---\ntitle: Hello\nauthor: \"Don\"\n---\n\n# Body\n"
	doc := Render(src)
	if doc.Frontmatter["title"] != "Hello" || doc.Frontmatter["author"] != "Don" {
		t.Errorf("frontmatter wrong: %#v", doc.Frontmatter)
	}
	if !strings.Contains(string(doc.HTML), "Body") {
		t.Errorf("body not rendered: %s", doc.HTML)
	}
	if doc.Title != "Body" {
		t.Errorf("title should be first H1, got %q", doc.Title)
	}
}

func TestFrontmatterTitleFallback(t *testing.T) {
	doc := Render("---\ntitle: Hello\n---\n\nNo heading here.\n")
	if doc.Title != "Hello" {
		t.Errorf("expected fallback to frontmatter title, got %q", doc.Title)
	}
}

func TestRawHTMLIsEscaped(t *testing.T) {
	got := string(RenderHTML("inline <script>alert(1)</script>\n"))
	if strings.Contains(got, "<script>") {
		t.Errorf("raw HTML leaked: %s", got)
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Errorf("html not escaped: %s", got)
	}
}

func TestEscapedPunctuationLiterals(t *testing.T) {
	got := string(RenderHTML(`literal \*not italic\*` + "\n"))
	if !strings.Contains(got, "*not italic*") {
		t.Errorf("escape didn't preserve literal: %s", got)
	}
	if strings.Contains(got, "<em>") {
		t.Errorf("escape failed; em emitted: %s", got)
	}
}

func TestMixedDocument(t *testing.T) {
	src := `# Title

A paragraph with **bold**.

- one
- two

` + "```\ncode block\n```\n" + `

> a quote
`
	got := string(RenderHTML(src))
	for _, want := range []string{"<h1", "<p>", "<strong>bold</strong>", "<ul>", `<pre tabindex="0"><code>`, "<blockquote>"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in output:\n%s", want, got)
		}
	}
}
