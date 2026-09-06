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

func TestParseFenceMeta(t *testing.T) {
	cases := []struct {
		in       string
		filename string
		lines    bool
	}{
		{"", "", false},
		{`title="main.go"`, "main.go", false},
		{`title=main.go`, "main.go", false},
		{`title="cmd/api/main.go" showLineNumbers`, "cmd/api/main.go", true},
		{`showLineNumbers title="a b.go"`, "a b.go", true},
		{`showLineNumbers=false`, "", false},
		{`{1,3-5} highlight=2`, "", false}, // options meant for someone else
	}
	for _, c := range cases {
		got := parseFenceMeta(c.in)
		if got.filename != c.filename || got.lineNumbers != c.lines {
			t.Errorf("parseFenceMeta(%q) = {%q, %v}, want {%q, %v}",
				c.in, got.filename, got.lineNumbers, c.filename, c.lines)
		}
	}
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
