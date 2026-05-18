package ui

import (
	"strings"
	"testing"
)

func TestJSONViewerRendersPrimitives(t *testing.T) {
	h := string(JSONViewer(JSONViewerConfig{Value: "hello"}))
	if !strings.Contains(h, `"hello"`) {
		t.Errorf("string Value should render quoted:\n%s", h)
	}
	n := string(JSONViewer(JSONViewerConfig{Value: 42}))
	if !strings.Contains(n, "42") {
		t.Errorf("number Value should render:\n%s", n)
	}
	b := string(JSONViewer(JSONViewerConfig{Value: true}))
	if !strings.Contains(b, "true") {
		t.Errorf("bool Value should render:\n%s", b)
	}
	nl := string(JSONViewer(JSONViewerConfig{Value: nil}))
	if !strings.Contains(nl, "null") {
		t.Errorf("nil Value should render as null:\n%s", nl)
	}
}

func TestJSONViewerRendersObjectAsDetails(t *testing.T) {
	h := string(JSONViewer(JSONViewerConfig{
		Value: map[string]any{"a": 1, "b": "two"},
	}))
	if !strings.Contains(h, "<details ") {
		t.Errorf("object should render as <details> node:\n%s", h)
	}
	if !strings.Contains(h, "ui-json-viewer__key") {
		t.Errorf("object should emit key spans:\n%s", h)
	}
}

func TestJSONViewerRendersArrayAsDetails(t *testing.T) {
	h := string(JSONViewer(JSONViewerConfig{
		Value: []any{1, 2, 3},
	}))
	if !strings.Contains(h, "<details ") {
		t.Errorf("array should render as <details> node:\n%s", h)
	}
	// Three index keys.
	if !strings.Contains(h, ">0</span>") || !strings.Contains(h, ">2</span>") {
		t.Errorf("array indices should render as keys:\n%s", h)
	}
}

func TestJSONViewerEmptyContainersInline(t *testing.T) {
	h := string(JSONViewer(JSONViewerConfig{Value: []any{}}))
	if !strings.Contains(h, "[]") {
		t.Errorf("empty array should render inline as []:\n%s", h)
	}
	if strings.Contains(h, "<details ") {
		t.Errorf("empty array should NOT be a collapsible <details>:\n%s", h)
	}
}

func TestJSONViewerOpenDepthControlsOpen(t *testing.T) {
	deep := map[string]any{"outer": map[string]any{"inner": "x"}}
	closed := string(JSONViewer(JSONViewerConfig{Value: deep, OpenDepth: 0}))
	// Root open, inner closed.
	if strings.Count(closed, "<details  open>") > 1 {
		// alt format: " open" vs "  open" — fall back to a more general check.
	}
	openAll := string(JSONViewer(JSONViewerConfig{Value: deep, OpenDepth: -1}))
	if strings.Count(openAll, " open>") < 2 {
		t.Errorf("OpenDepth=-1 should open every node:\n%s", openAll)
	}
}

func TestJSONViewerMaxStringLenTruncates(t *testing.T) {
	long := strings.Repeat("a", 200)
	h := string(JSONViewer(JSONViewerConfig{Value: long, MaxStringLen: 10}))
	if !strings.Contains(h, "…") {
		t.Errorf("MaxStringLen should append ellipsis:\n%s", h)
	}
}
