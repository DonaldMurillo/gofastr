package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestParseLineRanges(t *testing.T) {
	ok := []struct {
		in   string
		want []LineRange
	}{
		{"", nil},
		{"1", []LineRange{{From: 1, To: 0}}},
		{"2", []LineRange{{From: 2, To: 0}}},
		{"1,3-5", []LineRange{{From: 1, To: 0}, {From: 3, To: 5}}},
		{"1-1", []LineRange{{From: 1, To: 1}}},
		{" 1, 3-5 ", []LineRange{{From: 1, To: 0}, {From: 3, To: 5}}},
	}
	for _, c := range ok {
		got, err := ParseLineRanges(c.in)
		if err != nil {
			t.Errorf("ParseLineRanges(%q) unexpected error: %v", c.in, err)
			continue
		}
		if !rangesEqual(got, c.want) {
			t.Errorf("ParseLineRanges(%q) = %v, want %v", c.in, got, c.want)
		}
	}
	bad := []string{"0", "a", "5-3", "1,", "3-", "-5", "0-3", "2-0", "1-2-3", "1,,2"}
	for _, in := range bad {
		if got, err := ParseLineRanges(in); err == nil {
			t.Errorf("ParseLineRanges(%q) = %v, want an error", in, got)
		}
	}
}

func rangesEqual(a, b []LineRange) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Ranges are 1-based; To == 0 means "From only". A range past the last
// line matches nothing rather than panicking.
func TestCodeBlockHighlightLineClasses(t *testing.T) {
	ranges, err := ParseLineRanges("2,9-99")
	if err != nil {
		t.Fatalf("ParseLineRanges: %v", err)
	}
	h := string(CodeBlock(CodeBlockConfig{Code: "one\ntwo\nthree", HighlightLines: ranges}))
	if want := `<span class="ui-code-block__line ui-code-block__line--highlight">two</span>`; !strings.Contains(h, want) {
		t.Errorf("highlighted line missing its class:\n%s", h)
	}
	for _, plain := range []string{
		`<span class="ui-code-block__line">one</span>`,
		`<span class="ui-code-block__line">three</span>`,
	} {
		if !strings.Contains(h, plain) {
			t.Errorf("unhighlighted line must keep the bare class:\n%s", h)
		}
	}
}

func TestCodeBlockDiffLineClasses(t *testing.T) {
	h := string(CodeBlock(CodeBlockConfig{
		Code: "+added\n-removed\n ctx\n+++ new.go\n--- old.go",
		Diff: true,
	}))
	for _, want := range []string{
		`class="ui-code-block__line ui-code-block__line--added">+added`,
		`class="ui-code-block__line ui-code-block__line--removed">-removed`,
		`class="ui-code-block__line"> ctx`,
		// File headers count as added/removed too (first char rules).
		`class="ui-code-block__line ui-code-block__line--added">+++ new.go`,
		`class="ui-code-block__line ui-code-block__line--removed">--- old.go`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("diff classes wrong, missing %q:\n%s", want, h)
		}
	}
}

// On the Lines (pre-rendered HTML) path the marker sits behind a token
// span; classification must skip tags, not read the raw first byte.
func TestCodeBlockDiffLinesPathTags(t *testing.T) {
	h := string(CodeBlock(CodeBlockConfig{
		Lines: []render.HTML{
			render.HTML(`<span class="tk-pn">-</span><span class="tk-str">gone</span>`),
			render.HTML(`<span class="tk-pn">+</span>back`),
			render.Text(" ctx"),
		},
		Diff: true,
	}))
	for _, want := range []string{
		`ui-code-block__line--removed"><span class="tk-pn">-</span>`,
		`ui-code-block__line--added"><span class="tk-pn">+</span>`,
		`class="ui-code-block__line"> ctx`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("Lines-path diff classification wrong, missing %q:\n%s", want, h)
		}
	}
}

// Words match literally in the SOURCE text: `<b>` in the source is data,
// never a tag. Marks wrap the escaped text.
func TestCodeBlockWordMarksEscapeSafe(t *testing.T) {
	h := string(CodeBlock(CodeBlockConfig{
		Code:           `call <b> & done`,
		HighlightWords: []string{"<b>", "done"},
	}))
	if want := `<mark class="ui-code-block__mark">&lt;b&gt;</mark>`; !strings.Contains(h, want) {
		t.Errorf("source-looking word must be marked as escaped text:\n%s", h)
	}
	if want := `<mark class="ui-code-block__mark">done</mark>`; !strings.Contains(h, want) {
		t.Errorf("plain word must be marked:\n%s", h)
	}
	if n := strings.Count(h, "<mark"); n != 2 {
		t.Errorf("want exactly 2 marks, got %d:\n%s", n, h)
	}
	if strings.Contains(h, "&amp;amp;") {
		t.Errorf("ampersand double-escaped:\n%s", h)
	}
}

// A word never matches across a tag boundary on the Lines path: marking
// is per text node, so a caller's token markup stays intact.
func TestCodeBlockWordsNeverMarkAcrossTags(t *testing.T) {
	line := render.HTML(`foo<span class="tk-str">bar</span>baz`)
	h := string(CodeBlock(CodeBlockConfig{Lines: []render.HTML{line}, HighlightWords: []string{"foobar"}}))
	if strings.Contains(h, "<mark") {
		t.Errorf("a word spanning two text nodes must not match:\n%s", h)
	}
	h = string(CodeBlock(CodeBlockConfig{Lines: []render.HTML{line}, HighlightWords: []string{"bar"}}))
	if want := `<span class="tk-str"><mark class="ui-code-block__mark">bar</mark></span>`; !strings.Contains(h, want) {
		t.Errorf("a word inside one text node must be marked in place:\n%s", h)
	}
}

func TestCodeBlockWrapModifier(t *testing.T) {
	bare := string(CodeBlock(CodeBlockConfig{Code: "x", Wrap: true}))
	if !strings.Contains(bare, `class="ui-code-block ui-code-block--wrap"`) {
		t.Errorf("bare Wrap block missing modifier:\n%s", bare)
	}
	framed := string(CodeBlock(CodeBlockConfig{Code: "x", Filename: "f", Wrap: true}))
	if !strings.Contains(framed, `ui-code-block--framed ui-code-block--wrap`) {
		t.Errorf("framed Wrap block missing modifier:\n%s", framed)
	}
}

// The compatibility contract: a config that sets none of the new fields
// renders byte-identical markup to before they existed.
func TestCodeBlockZeroConfigUnchanged(t *testing.T) {
	bare := string(CodeBlock(CodeBlockConfig{Code: "x := 1\ny := 2\nit's <tagged>"}))
	wantBare := "<pre aria-label=\"source code\" class=\"ui-code-block\" tabindex=\"0\" data-fui-comp=\"ui-code-block\"><code>x := 1\ny := 2\nit&#39;s &lt;tagged&gt;</code></pre>"
	if bare != wantBare {
		t.Errorf("bare zero-config output changed:\n got: %s\nwant: %s", bare, wantBare)
	}
	framed := string(CodeBlock(CodeBlockConfig{
		Filename:    "main.go",
		Code:        "x := 1",
		LineNumbers: true,
		ShowCopy:    true,
		ID:          "fixed-id",
	}))
	wantFramed := `<div class="ui-code-block ui-code-block--framed ui-code-block--numbered" id="fixed-id" data-fui-comp="ui-code-block"><div class="ui-code-block__head"><span aria-hidden="true" class="ui-code-block__status"></span><span class="ui-code-block__file">main.go</span><div class="ui-code-block__meta"><span class="ui-copy-btn-wrap" data-fui-comp="ui-copy-btn"><button class="ui-copy-btn ui-code-block__copy" data-fui-copy-announce="Copied" data-fui-copy-text-from="#fixed-id" id="" type="button"><span class="ui-copy-btn__label">copy</span><span aria-hidden="true" class="ui-copy-btn__copied">copied</span></button><span aria-live="polite" class="ui-visually-hidden" data-fui-copy-status="" role="status"></span></span></div></div><pre aria-label="source code" class="ui-code-block__body" id="fixed-id" tabindex="0"><code>x := 1</code></pre></div>`
	if framed != wantFramed {
		t.Errorf("framed zero-config output changed:\n got: %s\nwant: %s", framed, wantFramed)
	}
	lines := string(CodeBlock(CodeBlockConfig{
		Filename: "lines.go",
		Lines:    []render.HTML{render.Text("a := 1"), render.Text("b := 2")},
	}))
	wantLines := `<div class="ui-code-block ui-code-block--framed" data-fui-comp="ui-code-block"><div class="ui-code-block__head"><span aria-hidden="true" class="ui-code-block__status"></span><span class="ui-code-block__file">lines.go</span><div class="ui-code-block__meta"><span>2 lines</span></div></div><pre aria-label="source code" class="ui-code-block__body" tabindex="0"><span class="ui-code-block__line">a := 1</span><span class="ui-code-block__line">b := 2</span></pre></div>`
	if lines != wantLines {
		t.Errorf("Lines zero-config output changed:\n got: %s\nwant: %s", lines, wantLines)
	}
}

// The per-line rules must extend to the block's edge (negative inline
// margin over the body's horizontal padding) and read their colours from
// overridable tokens derived from the theme's status colours.
func TestCodeBlockCSSPerLineRules(t *testing.T) {
	css := codeBlockCSS(style.Theme{})
	for _, want := range []string{
		"--ui-code-block-highlight-bg",
		"--ui-code-block-added-bg",
		"--ui-code-block-removed-bg",
		"--ui-code-block-mark-bg",
		"var(--color-primary",
		"var(--color-success",
		"var(--color-danger",
		"var(--color-warning",
		"margin-inline: calc(-1 * var(--spacing-lg, 16px))",
		"padding-inline: var(--spacing-lg, 16px)",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("codeBlockCSS missing %q", want)
		}
	}
	for _, line := range []string{"--highlight", "--added", "--removed"} {
		start := strings.Index(css, "ui-code-block__line"+line)
		if start < 0 {
			t.Fatalf("no rule for %s lines", line)
		}
		block := css[start : strings.Index(css[start:], "}")+start]
		if !strings.Contains(block, "margin-inline") || !strings.Contains(block, "padding-inline") {
			t.Errorf("%s line rule must extend to the block edge:\n%s", line, block)
		}
	}
	if !strings.Contains(css, "white-space: pre-wrap") {
		t.Errorf("wrap variant must switch the body to pre-wrap")
	}
}
