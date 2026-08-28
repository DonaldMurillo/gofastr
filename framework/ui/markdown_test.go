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
