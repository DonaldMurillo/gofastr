package markdown

import (
	"strings"
	"testing"
	"time"
)

// A nested list is a list inside its item. Its lines used to join the
// parent item as inline text, so the page showed "- nested" with its dash.
func TestNestedListRendersAsList(t *testing.T) {
	got := string(RenderHTML("- item\n  - nested\n    wrapped\n  - nested2\n- two"))
	want := "<ul>\n  <li>item\n<ul>\n  <li>nested\nwrapped</li>\n  <li>nested2</li>\n</ul>\n</li>\n  <li>two</li>\n</ul>\n"
	if got != want {
		t.Fatalf("nested list:\n got %q\nwant %q", got, want)
	}
	ordered := string(RenderHTML("1. one\n   - a\n   - b\n2. two"))
	if strings.Count(ordered, "<ol>") != 1 || strings.Count(ordered, "<ul>") != 1 || strings.Contains(ordered, "- a") {
		t.Fatalf("bullets under a numbered step: %q", ordered)
	}
}

// A fenced block under a list item is a code block in that item. Read as
// inline text the fence became one inline code span with its lines run
// together; unindented from a blank line, it ended the list.
func TestFenceUnderListItem(t *testing.T) {
	got := string(RenderHTML("- **Type**: the shape:\n  ```go\n  type T struct {\n      A int\n  }\n  ```\n- next"))
	want := "<ul>\n  <li><strong>Type</strong>: the shape:\n<pre tabindex=\"0\"><code class=\"language-go\">type T struct {\n    A int\n}\n</code></pre>\n</li>\n  <li>next</li>\n</ul>\n"
	if got != want {
		t.Fatalf("fence in item:\n got %q\nwant %q", got, want)
	}
	steps := string(RenderHTML("1. Install:\n\n   ```sh\n   go install x\n   ```\n\n2. Run it.\n"))
	if strings.Count(steps, "<ol") != 1 || strings.Count(steps, "<li>") != 2 {
		t.Fatalf("a code block under step 1 split the numbered list: %q", steps)
	}
	if !strings.Contains(steps, "<code class=\"language-sh\">go install x\n</code>") {
		t.Fatalf("the step's code lost its fence or kept its indentation: %q", steps)
	}
}

// Items separated by a blank line are one list. As two lists, a numbered
// one counted 1, 1.
func TestBlankLineBetweenItemsKeepsOneList(t *testing.T) {
	for _, src := range []string{"1. a\n\n2. b\n", "- a\n\n- b\n"} {
		got := string(RenderHTML(src))
		if strings.Count(got, "<li>") != 2 || strings.Count(got, "</ol>")+strings.Count(got, "</ul>") != 1 {
			t.Errorf("%q rendered as %q, want one list of two items", src, got)
		}
	}
	// A paragraph after the list is not pulled into it.
	if got := string(RenderHTML("- a\n\ntext\n")); got != "<ul>\n  <li>a</li>\n</ul>\n<p>text</p>\n" {
		t.Errorf("paragraph after a list: %q", got)
	}
}

// An ordered list that resumes after unindented content keeps its number.
func TestOrderedListStart(t *testing.T) {
	got := string(RenderHTML("1. a\n\ntext between\n\n2. b\n3. c\n"))
	if !strings.Contains(got, "<ol>\n  <li>a</li>") || !strings.Contains(got, "<ol start=\"2\">\n  <li>b</li>\n  <li>c</li>") {
		t.Fatalf("resumed list lost its number: %q", got)
	}
}

// An indented list's second item is an item, not a continuation of the first.
func TestIndentedListSiblings(t *testing.T) {
	got := string(RenderHTML("  - a\n  - b\n"))
	if got != "<ul>\n  <li>a</li>\n  <li>b</li>\n</ul>\n" {
		t.Fatalf("indented list: %q", got)
	}
}

// Each nesting level re-parses its item's text, so the levels are capped
// like blockquotes: 2000 levels render in the time of 32.
func TestListNestingIsBounded(t *testing.T) {
	var b strings.Builder
	for i := range 2000 {
		b.WriteString(strings.Repeat(" ", 2*i))
		b.WriteString("- x\n")
	}
	done := make(chan struct{})
	go func() { RenderHTML(b.String()); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("deeply nested lists did not render in 5s")
	}
}

// "\|" is a literal pipe in a table cell, in a code span as well.
func TestTableEscapedPipe(t *testing.T) {
	got := string(RenderHTML("| attr | meaning |\n|---|---|\n| `mode=\"text\\|number\"` | a \\| b |\n"))
	want := "<tr><td><code>mode=&quot;text|number&quot;</code></td><td>a | b</td></tr>"
	if !strings.Contains(got, want) {
		t.Fatalf("escaped pipes split the row:\n got %q\nwant it to contain %q", got, want)
	}
}

func TestHeadingKeepsLeadingHashInText(t *testing.T) {
	cases := map[string]string{
		"## #1 priority": "<h2 id=\"1-priority\">#1 priority</h2>\n",
		"## Setup ##":    "<h2 id=\"setup\">Setup</h2>\n",
		"# C#":           "<h1 id=\"c\">C#</h1>\n",
		"# Title #hash":  "<h1 id=\"title-hash\">Title #hash</h1>\n",
	}
	for src, want := range cases {
		if got := string(RenderHTML(src)); got != want {
			t.Errorf("%q: got %q, want %q", src, got, want)
		}
	}
}

// A '*' or '_' run with whitespace after it opens nothing, and one with
// whitespace before it closes nothing.
func TestEmphasisNeedsFlankingText(t *testing.T) {
	cases := map[string]string{
		"2 * 3 * 4":         "<p>2 * 3 * 4</p>\n",
		"a ** b ** c":       "<p>a ** b ** c</p>\n",
		"data-* and aria-*": "<p>data-* and aria-*</p>\n",
		"*a * b*":           "<p><em>a * b</em></p>\n",
		"*a* and **b**":     "<p><em>a</em> and <strong>b</strong></p>\n",
		"foo*bar*baz":       "<p>foo<em>bar</em>baz</p>\n",
	}
	for src, want := range cases {
		if got := string(RenderHTML(src)); got != want {
			t.Errorf("%q: got %q, want %q", src, got, want)
		}
	}
}

// A link URL keeps its balanced parentheses.
func TestLinkURLBalancedParens(t *testing.T) {
	cases := map[string]string{
		"[Go](https://en.wikipedia.org/wiki/Go_(game)) after": `<p><a href="https://en.wikipedia.org/wiki/Go_(game)">Go</a> after</p>` + "\n",
		"[a](b) (see (c))": `<p><a href="b">a</a> (see (c))</p>` + "\n",
		"[a](b(c) tail":    `<p><a href="b(c">a</a> tail</p>` + "\n",
		"*[a](x_(y)) b*":   `<p><em><a href="x_(y)">a</a> b</em></p>` + "\n",
	}
	for src, want := range cases {
		if got := string(RenderHTML(src)); got != want {
			t.Errorf("%q: got %q, want %q", src, got, want)
		}
	}
}
