package markdown

import (
	"strings"
	"testing"
)

func TestParseFenceInfo(t *testing.T) {
	cases := []struct {
		in         string
		lang, meta string
	}{
		{"", "", ""},
		{"go", "go", ""},
		{"  go  ", "go", ""},
		{`go title="main.go"`, "go", `title="main.go"`},
		{"go {1,3-5} showLineNumbers", "go", "{1,3-5} showLineNumbers"},
		{"go\ttitle=x", "go", "title=x"},
		{`title="no language"`, `title="no`, `language"`},
	}
	for _, c := range cases {
		got := ParseFenceInfo(c.in)
		if got.Lang != c.lang || got.Meta != c.meta {
			t.Errorf("ParseFenceInfo(%q) = {%q, %q}, want {%q, %q}",
				c.in, got.Lang, got.Meta, c.lang, c.meta)
		}
	}
}

// A fence option used to be swallowed into the language name, so
// ```go title="main.go" rendered class="language-go title=&quot;main.go&quot;"
// and no highlighter matched it. Writing any option cost you highlighting.
func TestFenceOptionsKeepTheLanguageClass(t *testing.T) {
	got := string(RenderHTML("```go title=\"main.go\" showLineNumbers\nx := 1\n```\n"))
	if !strings.Contains(got, `<code class="language-go"`) {
		t.Errorf("fence options must not pollute the language class: %s", got)
	}
	if !strings.Contains(got, `data-meta="title=&quot;main.go&quot; showLineNumbers"`) {
		t.Errorf("fence options must survive in data-meta: %s", got)
	}
}

// The plain-fence output is the contract everything downstream reads; it must
// not have moved.
func TestPlainFenceOutputUnchanged(t *testing.T) {
	cases := map[string]string{
		"```\nx\n```\n":   "<pre tabindex=\"0\"><code>x\n</code></pre>\n",
		"```go\nx\n```\n": "<pre tabindex=\"0\"><code class=\"language-go\">x\n</code></pre>\n",
		"~~~go\nx\n~~~\n": "<pre tabindex=\"0\"><code class=\"language-go\">x\n</code></pre>\n",
		// Unterminated: the block runs to EOF, trailing blank line and all.
		"```go\nx\n":        "<pre tabindex=\"0\"><code class=\"language-go\">x\n\n</code></pre>\n",
		"  ```go\nx\n  ```": "<pre tabindex=\"0\"><code class=\"language-go\">x\n</code></pre>\n",
	}
	for in, want := range cases {
		if got := string(RenderHTML(in)); got != want {
			t.Errorf("plain fence %q:\n got: %q\nwant: %q", in, got, want)
		}
	}
}

// Fences longer than three characters are the only way to show a ``` block
// inside a markdown example. Reading exactly three tore the example into three
// pieces at the first inner fence.
func TestLongFenceHoldsAnInnerFence(t *testing.T) {
	src := "````md\n```go\nx := 1\n```\n````\n"
	got := string(RenderHTML(src))
	if strings.Count(got, "<pre") != 1 {
		t.Fatalf("a ```` block must render as ONE code block: %s", got)
	}
	if !strings.Contains(got, `class="language-md"`) {
		t.Errorf("language of a 4-backtick fence must be md: %s", got)
	}
	for _, want := range []string{"```go", "x := 1"} {
		if !strings.Contains(got, want) {
			t.Errorf("inner fence content %q missing from the block: %s", want, got)
		}
	}
}

// A run SHORTER than the opener is content, and one at least as long closes.
func TestFenceClosesOnlyOnALongEnoughRun(t *testing.T) {
	got := string(RenderHTML("`````\n```\n`````\n"))
	if strings.Count(got, "<pre") != 1 || !strings.Contains(got, "```") {
		t.Errorf("a short inner run must stay content: %s", got)
	}
	longer := string(RenderHTML("```\nx\n`````\n"))
	if strings.Count(longer, "<pre") != 1 || strings.Contains(longer, "`") {
		t.Errorf("a longer closing run must close the block: %s", longer)
	}
}

// An opening fence carrying an info string never closes an open block, so a
// ```go line inside a plain fence is content.
func TestInnerOpeningFenceDoesNotClose(t *testing.T) {
	got := string(RenderHTML("```\n```go\nx\n```\n"))
	if strings.Count(got, "<pre") != 1 {
		t.Errorf("an info-carrying run must not close the block: %s", got)
	}
}

// Tildes and backticks do not close each other.
func TestFenceCharsDoNotCross(t *testing.T) {
	got := string(RenderHTML("~~~\n```\nx\n~~~\n"))
	if strings.Count(got, "<pre") != 1 || !strings.Contains(got, "```") {
		t.Errorf("a backtick run must not close a tilde fence: %s", got)
	}
}

// The info string is attacker-controlled on user-submitted markdown, and it now
// lands in a second attribute. data-meta has to be escaped exactly like class.
func TestFenceMetaAttrEscaped(t *testing.T) {
	for _, info := range []string{
		`go "><img src=x onerror=alert(1)>`,
		`go x" onmouseover="alert(1)`,
	} {
		html := string(RenderHTML("```" + info + "\nbody\n```"))
		if strings.Contains(html, "<img") || strings.Contains(html, "onmouseover=\"alert") {
			t.Errorf("SECURITY: [markdown] fence meta broke out of data-meta: %s", html)
		}
	}
}
