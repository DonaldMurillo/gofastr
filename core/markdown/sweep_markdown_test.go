package markdown

import (
	"strings"
	"testing"
)

func TestSweepMarkdownLeadingBOMIsDropped(t *testing.T) {
	doc := Render("\ufeff---\ntitle: T\n---\n# H")
	if doc.Frontmatter["title"] != "T" || strings.Contains(string(doc.HTML), "\ufeff") {
		t.Fatalf("a leading BOM was kept: %q %q", doc.Frontmatter, doc.HTML)
	}
}

func TestSweepMarkdownCRLFIsOneLineBreak(t *testing.T) {
	if got := string(RenderHTML("a\r\nb\r\n\r\nc")); got != "<p>a\nb</p>\n<p>c</p>\n" {
		t.Fatalf("CRLF input = %q", got)
	}
}

func TestSweepMarkdownLinkURLNestsParens(t *testing.T) {
	got := string(RenderHTML("[x](https://e.test/a_(b_(c)))"))
	if !strings.Contains(got, `href="https://e.test/a_(b_(c))"`) {
		t.Fatalf("doubly nested parens = %q", got)
	}
}

func TestSweepMarkdownImagePolicyIsImageSource(t *testing.T) {
	if got := string(RenderHTML("![x](data:image/png;base64,AAAA)")); !strings.Contains(got, `src="data:image/png;base64,AAAA"`) {
		t.Fatalf("raster data image refused: %q", got)
	}
	if got := string(RenderHTML("![x](mailto:a@b.test)")); !strings.Contains(got, `src="#"`) {
		t.Fatalf("mailto image kept: %q", got)
	}
}

func TestSweepMarkdownLinkURLDropsControlBytes(t *testing.T) {
	if got := string(RenderHTML("[a](https://e.test/a\tb)")); !strings.Contains(got, `href="https://e.test/ab"`) {
		t.Fatalf("tab inside a link URL = %q, want it removed as a browser would", got)
	}
}

func TestSweepMarkdownLinkParenPastWindowIsNoLink(t *testing.T) {
	if got := string(RenderHTML("[x [a](b](c)")); got != "<p><a href=\"c\">x [a](b</a></p>\n" {
		t.Fatalf("a link whose ')' lies past its window = %q", got)
	}
}

func TestSweepMarkdownIndentedCommentOpenerIsText(t *testing.T) {
	if got := string(RenderHTML("    <!-- note -->")); !strings.Contains(got, "&lt;!-- note --&gt;") {
		t.Fatalf("a comment opener indented four spaces was hidden: %q", got)
	}
}

func TestSweepMarkdownListCommentMaySpanUnindentedLines(t *testing.T) {
	got := string(RenderHTML("- a\n  <!--\nnote\n-->\n- b\n"))
	if strings.Contains(got, "note") || !strings.Contains(got, "<li>b</li>") {
		t.Fatalf("a comment under a list item = %q", got)
	}
}
