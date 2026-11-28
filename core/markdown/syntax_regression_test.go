package markdown

import (
	"strings"
	"testing"
)

func TestBackticksInFenceInfo(t *testing.T) {
	if _, ok := openFence("```example `literal`"); ok {
		t.Fatal("backtick fence accepted backticks in its info string")
	}
	if _, ok := openFence("~~~example `literal`"); !ok {
		t.Fatal("tilde fence rejected backticks in its info string")
	}
}

func TestFrontmatterIgnoresNestedFields(t *testing.T) {
	fields, _ := splitFrontmatter("---\nmetadata:\n  title: nested\ntitle: top level\n---\n")
	if got := fields["title"]; got != "top level" {
		t.Fatalf("title = %q, want top level", got)
	}
}

func TestHeadingKeepsLiteralLeadingHash(t *testing.T) {
	if got := Render("# #hashtag").Title; got != "#hashtag" {
		t.Fatalf("heading title = %q, want #hashtag", got)
	}
}

func TestOrderedListKeepsStartingNumber(t *testing.T) {
	if got := string(Render("7. Seven\n8. Eight").HTML); !strings.Contains(got, `<ol start="7">`) {
		t.Fatalf("ordered list lost starting number: %s", got)
	}
}

func TestClosedHeadingOmitsClosingHashes(t *testing.T) {
	got := Render("## Installation ###\n\n## C#")
	if !strings.Contains(string(got.HTML), ">Installation</h2>") || !strings.Contains(string(got.HTML), ">C#</h2>") {
		t.Fatalf("headings = %s", got.HTML)
	}
}

func TestBlockquoteMarkerCanTouchContent(t *testing.T) {
	if got := string(Render(">Quoted\n\nordinary > comparison").HTML); !strings.Contains(got, "<blockquote>\n<p>Quoted</p>") {
		t.Fatalf("blockquote marker was rendered literally: %s", got)
	}
}

func TestTablePreservesEscapedPipeInCell(t *testing.T) {
	got := string(Render("| Value |\n| --- |\n| a\\|b |").HTML)
	if !strings.Contains(got, "<td>a|b</td>") {
		t.Fatalf("escaped pipe split a cell: %s", got)
	}
}

func TestTableNeedsOneSeparatorPerHeader(t *testing.T) {
	got := string(Render("| A | B |\n| --- |\n| x | y |").HTML)
	if strings.Contains(got, "<table>") {
		t.Fatalf("different header and delimiter counts became a table: %s", got)
	}
}

func TestIndentedFenceRemovesItsOwnIndent(t *testing.T) {
	got := string(Render("  ```\n  first\n    second\n  ```").HTML)
	if !strings.Contains(got, "<code>first\n  second\n</code>") {
		t.Fatalf("fence treated block indentation as code: %s", got)
	}
}
