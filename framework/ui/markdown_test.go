package ui

import (
	"strings"
	"testing"
)

func TestMarkdownRequiresSource(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Markdown without Source should panic")
		}
	}()
	Markdown(MarkdownConfig{})
}

func TestMarkdownRendersHeadingsAndParas(t *testing.T) {
	h := string(Markdown(MarkdownConfig{
		Source: "# Title\n\nHello **world**.\n",
	}))
	if !strings.Contains(h, "<h1") {
		t.Errorf("# heading should render as <h1>:\n%s", h)
	}
	if !strings.Contains(h, "<p") {
		t.Errorf("paragraph should render as <p>:\n%s", h)
	}
	if !strings.Contains(h, "<strong") {
		t.Errorf("**bold** should render as <strong>:\n%s", h)
	}
}

func TestMarkdownCompactClass(t *testing.T) {
	h := string(Markdown(MarkdownConfig{
		Source: "Hello.", Compact: true,
	}))
	if !strings.Contains(h, "ui-markdown--compact") {
		t.Errorf("Compact=true should add modifier class:\n%s", h)
	}
}

func TestMarkdownDataFuiComp(t *testing.T) {
	h := string(Markdown(MarkdownConfig{Source: "Hi"}))
	if !strings.Contains(h, `data-fui-comp="ui-markdown"`) {
		t.Errorf("Markdown should emit data-fui-comp marker:\n%s", h)
	}
}

// A fence option used to be swallowed into the language name, so a block
// written ```go title="main.go" lost its highlighting. core/markdown now passes
// the options through in data-meta and the two this renderer understands land
// on the CodeBlock.

func TestMarkdownFenceOptionsReachTheCodeBlock(t *testing.T) {
	h := string(Markdown(MarkdownConfig{
		Source: "```go title=\"main.go\" showLineNumbers\nfunc main() {}\n```\n",
	}))
	if !strings.Contains(h, `class="ui-code-block__file">main.go<`) {
		t.Errorf("title= should become the code block's filename header:\n%s", h)
	}
	if !strings.Contains(h, "ui-code-block--numbered") {
		t.Errorf("showLineNumbers should turn on the gutter:\n%s", h)
	}
	// The point of the fix: highlighting survives the options.
	if !strings.Contains(h, `class="tk-kw"`) {
		t.Errorf("Go keywords should still be highlighted with options present:\n%s", h)
	}
}

func TestMarkdownPlainFenceUnaffected(t *testing.T) {
	h := string(Markdown(MarkdownConfig{Source: "```go\nfunc main() {}\n```\n"}))
	if !strings.Contains(h, `class="tk-kw"`) {
		t.Errorf("a plain fence should still highlight:\n%s", h)
	}
	if strings.Contains(h, "data-meta") {
		t.Errorf("a plain fence should emit no data-meta:\n%s", h)
	}
}

func TestMarkdownFenceScrollForwards(t *testing.T) {
	h := string(Markdown(MarkdownConfig{Source: "```go scroll\nx := 1\n```\n"}))
	if !strings.Contains(h, "ui-code-block--scroll") {
		t.Errorf("scroll option should reach the block:\n%s", h)
	}
}

func TestMarkdownFenceHighlightForwards(t *testing.T) {
	for _, meta := range []string{"highlight=2", "{2}"} {
		h := string(Markdown(MarkdownConfig{Source: "```txt " + meta + "\nalpha\nbeta\n```\n"}))
		if !strings.Contains(h, `class="ui-code-block__line ui-code-block__line--highlight">beta<`) {
			t.Errorf("option %q should highlight line 2:\n%s", meta, h)
		}
		if strings.Contains(h, `--highlight">alpha`) {
			t.Errorf("option %q highlighted the wrong line:\n%s", meta, h)
		}
	}
}

// The option turns on diff rendering; a fence whose LANGUAGE is diff is
// still just a language (a highlighter may know it) and implies nothing.
func TestMarkdownFenceDiffForwards(t *testing.T) {
	h := string(Markdown(MarkdownConfig{
		Source: "```diff title=\"p.diff\" diff\n--- a/main.go\n+++ b/main.go\n ctx\n-old\n+new\n```\n",
	}))
	for _, want := range []string{
		`--removed">--- a/main.go`,
		`--added">+++ b/main.go`,
		`--removed">-old`,
		`--added">+new`,
		`class="ui-code-block__line"> ctx`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("diff option missing %q:\n%s", want, h)
		}
	}
	langOnly := string(Markdown(MarkdownConfig{Source: "```diff\n-x\n```\n"}))
	if strings.Contains(langOnly, "ui-code-block__line--") {
		t.Errorf("lang diff must not imply diff marking:\n%s", langOnly)
	}
}

func TestMarkdownFenceWordsForwards(t *testing.T) {
	h := string(Markdown(MarkdownConfig{Source: "```txt words=\"beta\"\nalpha beta\n```\n"}))
	if !strings.Contains(h, `<mark class="ui-code-block__mark">beta</mark>`) {
		t.Errorf("words option should mark matches:\n%s", h)
	}
}

func TestMarkdownFenceWrapForwards(t *testing.T) {
	h := string(Markdown(MarkdownConfig{Source: "```txt wrap\nalpha\n```\n"}))
	if !strings.Contains(h, "ui-code-block--wrap") {
		t.Errorf("wrap option should reach the block:\n%s", h)
	}
}

// Recognized options are consumed; the raw info string still rides on
// the block root in data-meta so tooling (and tests) can see it.
func TestMarkdownUnknownMetaKeptInDataMeta(t *testing.T) {
	h := string(Markdown(MarkdownConfig{Source: "```txt foo=bar title=\"t.txt\"\nx\n```\n"}))
	if !strings.Contains(h, `data-meta="foo=bar title=&quot;t.txt&quot;"`) {
		t.Errorf("unknown tokens must stay in data-meta:\n%s", h)
	}
}

func TestParseFenceMeta(t *testing.T) {
	cases := []struct {
		in       string
		filename string
		lines    bool
		scroll   bool
		diff     bool
		wrap     bool
		hl       []LineRange
		words    []string
	}{
		{in: ``},
		{in: `title="main.go"`, filename: "main.go"},
		{in: `title=main.go`, filename: "main.go"},
		{in: `title="cmd/api/main.go" showLineNumbers`, filename: "cmd/api/main.go", lines: true},
		{in: `showLineNumbers title="a b.go"`, filename: "a b.go", lines: true},
		{in: `showLineNumbers=false`},
		{in: `scroll diff wrap`, scroll: true, diff: true, wrap: true},
		{in: `wrap=false`},
		{in: `nowrap`},
		{in: `diff=false`},
		{in: `{1,3-5}`, hl: []LineRange{{From: 1}, {From: 3, To: 5}}},
		{in: `highlight=1,3-5`, hl: []LineRange{{From: 1}, {From: 3, To: 5}}},
		// Later tokens win; an invalid spec degrades to no highlight.
		{in: `{1,3-5} highlight=2`, hl: []LineRange{{From: 2}}},
		{in: `highlight=5-3`},
		{in: `words="a,b"`, words: []string{"a", "b"}},
		{in: `words=a,b`, words: []string{"a", "b"}},
		{in: `foo=bar`}, // unknown token: ignored, never fatal
	}
	for _, c := range cases {
		got := parseFenceMeta(c.in)
		if got.filename != c.filename || got.lineNumbers != c.lines ||
			got.scroll != c.scroll || got.diff != c.diff || got.wrap != c.wrap ||
			!rangesEqual(got.highlight, c.hl) || !slicesEqual(got.words, c.words) {
			t.Errorf("parseFenceMeta(%q) = %+v, want {filename:%q lines:%v scroll:%v diff:%v wrap:%v hl:%v words:%v}",
				c.in, got, c.filename, c.lines, c.scroll, c.diff, c.wrap, c.hl, c.words)
		}
	}
}

func slicesEqual(a, b []string) bool {
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

func TestMarkdownExtraAttrsCannotOverrideOwned(t *testing.T) {
	h := Markdown(MarkdownConfig{Source: "hi", ID: "real", ExtraAttrs: map[string]string{
		"data-test": "hook", "id": "evil", "Class": "evil",
	}})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("root missing data-test:\n%s", root)
	}
	if !strings.Contains(root, `id="real"`) {
		t.Errorf("framework id lost:\n%s", root)
	}
	if !strings.Contains(root, "ui-markdown") {
		t.Errorf("framework class lost:\n%s", root)
	}
	if strings.Contains(root, "evil") {
		t.Errorf("owned attr overridden by ExtraAttrs:\n%s", root)
	}
}
