package markdown

import (
	"strings"
	"testing"
)

func TestCommentBlockRendersNothing(t *testing.T) {
	cases := map[string]string{
		"one line":  "Before.\n\n<!-- a note -->\n\nAfter.\n",
		"multiline": "Before.\n\n<!-- gofastr:compile\nimport \"net/http\"\n-->\n```go\nx := 1\n```\n\nAfter.\n",
		"indented":  "Before.\n\n   <!-- a note -->\n\nAfter.\n",
		"no blank":  "Before.\n<!-- a note -->\n\nAfter.\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			got := string(RenderHTML(src))
			if strings.Contains(got, "&lt;!--") || strings.Contains(got, "note") || strings.Contains(got, "gofastr:compile") {
				t.Errorf("comment leaked into output: %s", got)
			}
			if !strings.Contains(got, "<p>Before.</p>") || !strings.Contains(got, "<p>After.</p>") {
				t.Errorf("surrounding paragraphs lost: %s", got)
			}
		})
	}
}

func TestCommentBlockKeepsFollowingFence(t *testing.T) {
	src := "<!-- gofastr:compile\nimport \"net/http\"\n-->\n```go\nx := 1\n```\n"
	got := string(RenderHTML(src))
	if !strings.Contains(got, `<code class="language-go">x := 1`) {
		t.Errorf("fence after a comment did not render: %s", got)
	}
}

func TestUnclosedCommentStaysEscaped(t *testing.T) {
	got := string(RenderHTML("<!-- never closed\n\nStill here.\n"))
	if !strings.Contains(got, "&lt;!-- never closed") || !strings.Contains(got, "Still here.") {
		t.Errorf("unclosed comment should render as escaped text, not swallow the rest: %s", got)
	}
}

func TestCommentWithTrailingTextStaysEscaped(t *testing.T) {
	for _, src := range []string{"<!-- note --> visible\n", "<!-- a --> visible -->\n", "<!-- a\nb --> visible\n"} {
		if got := string(RenderHTML(src)); !strings.Contains(got, "visible") {
			t.Errorf("text after the comment's close was dropped from %q: %s", src, got)
		}
	}
}
