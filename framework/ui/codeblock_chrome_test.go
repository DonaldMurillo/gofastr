package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// Default (no chrome) stays a bare <pre> so existing consumers + CSS are
// unaffected.
func TestCodeBlockDefaultStaysBarePre(t *testing.T) {
	h := string(CodeBlock(CodeBlockConfig{Code: "x := 1"}))
	if !strings.HasPrefix(strings.TrimSpace(h), "<pre") {
		t.Errorf("default CodeBlock should still be a bare <pre>:\n%s", h)
	}
	if strings.Contains(h, "ui-code-block__head") {
		t.Errorf("default CodeBlock should not render chrome:\n%s", h)
	}
}

func TestCodeBlockFilenameRendersHead(t *testing.T) {
	h := string(CodeBlock(CodeBlockConfig{Filename: "main.go", Code: "x"}))
	for _, want := range []string{
		`data-fui-comp="ui-code-block"`,
		"ui-code-block--framed",
		"ui-code-block__head",
		"ui-code-block__file",
		"main.go",
		"ui-code-block__body",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("framed CodeBlock missing %q\n%s", want, h)
		}
	}
}

func TestCodeBlockShowCopyTargetsItsOwnBody(t *testing.T) {
	h := string(CodeBlock(CodeBlockConfig{Filename: "main.go", Code: "x", ShowCopy: true}))
	// The copy button (composed ui.CopyButton) must point at the body pre's id.
	if !strings.Contains(h, "data-fui-copy-text-from") {
		t.Fatalf("ShowCopy should emit a copy button:\n%s", h)
	}
	target := regexp.MustCompile(`data-fui-copy-text-from="#([^"]+)"`).FindStringSubmatch(h)
	if target == nil {
		t.Fatalf("could not find copy target:\n%s", h)
	}
	if !strings.Contains(h, `id="`+target[1]+`"`) {
		t.Errorf("copy target #%s has no matching element id\n%s", target[1], h)
	}
}

func TestCodeBlockLineNumbersWrapLines(t *testing.T) {
	h := string(CodeBlock(CodeBlockConfig{
		Filename:    "main.go",
		LineNumbers: true,
		Lines: []render.HTML{
			render.Text("line one"),
			render.Text("line two"),
		},
	}))
	if !strings.Contains(h, "ui-code-block--numbered") {
		t.Errorf("LineNumbers should add the numbered modifier:\n%s", h)
	}
	if n := strings.Count(h, "ui-code-block__line"); n < 2 {
		t.Errorf("each line should be wrapped (want >=2, got %d):\n%s", n, h)
	}
	for _, want := range []string{"line one", "line two"} {
		if !strings.Contains(h, want) {
			t.Errorf("missing line content %q\n%s", want, h)
		}
	}
}

// Lines precedence: when Lines is set, Code is ignored.
func TestCodeBlockLinesOverrideCode(t *testing.T) {
	h := string(CodeBlock(CodeBlockConfig{
		Filename: "f",
		Code:     "RAWCODE",
		Lines:    []render.HTML{render.Text("LINE")},
	}))
	if strings.Contains(h, "RAWCODE") {
		t.Errorf("Lines should take precedence over Code:\n%s", h)
	}
	if !strings.Contains(h, "LINE") {
		t.Errorf("Lines content missing:\n%s", h)
	}
}

// Scroll adds the scroll modifier so a long file's body is height-capped and
// scrolls vertically. It must force the framed container (the body cap only
// applies to the framed variant).
func TestCodeBlockScrollAddsModifierAndFrames(t *testing.T) {
	h := string(CodeBlock(CodeBlockConfig{Filename: "big.yml", Code: "x", Scroll: true}))
	if !strings.Contains(h, "ui-code-block--scroll") {
		t.Errorf("Scroll should add the scroll modifier:\n%s", h)
	}
	if !strings.Contains(h, "ui-code-block--framed") {
		t.Errorf("Scroll should force the framed container:\n%s", h)
	}
}
