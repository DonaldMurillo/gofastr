package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

func TestJSONViewerRendersPrimitives(t *testing.T) {
	h := string(JSONViewer(JSONViewerConfig{Value: "hello"}))
	if !strings.Contains(h, "&quot;hello&quot;") {
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
	if !classTokenPresent(h, "fui-json-viewer__key") {
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
	if n := strings.Count(closed, `open=""`); n != 1 {
		t.Errorf("OpenDepth=0 should open exactly the root, got %d:\n%s", n, closed)
	}
	openAll := string(JSONViewer(JSONViewerConfig{Value: deep, OpenDepth: -1}))
	if strings.Count(openAll, `open=""`) < 2 {
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

func TestJSONViewerExtraAttrsOnRoot(t *testing.T) {
	h := JSONViewer(JSONViewerConfig{
		Value:      map[string]any{"k": 1},
		ExtraAttrs: map[string]string{"data-test": "hook"},
	})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("json viewer root missing data-test:\n%s", root)
	}
}

func TestJSONViewerScalarAndColonPartsCarryClasses(t *testing.T) {
	h := string(JSONViewer(JSONViewerConfig{Value: map[string]any{
		"s": "text", "n": 1, "b": true, "nil": nil, "e": map[string]any{},
	}}))
	// The typed scalar parts are what the sheet's colour rules select;
	// when the primitive renders them all as the generic text part
	// the string colour and the colon's space silently die (review
	// item 21) while every existing test stays green.
	for _, want := range []string{
		`fui-json-viewer__str`,
		`fui-json-viewer__num`,
		`fui-json-viewer__bool`,
		`fui-json-viewer__null`,
		`fui-json-viewer__empty`,
		`fui-json-viewer__colon`,
		`fui-json-viewer__type`,
		`fui-json-viewer__count`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("the rendered tree lost the selectable class %q:\n%s", want, h)
		}
	}
}

func TestJSONViewerCSSColoursStringsAndSpacesColons(t *testing.T) {
	css := jsonViewerCSS(style.Theme{})
	if !strings.Contains(css, `.fui-json-viewer__str { color:`) {
		t.Errorf("the string colour rule is gone:\n%s", css)
	}
	if !strings.Contains(css, ".fui-json-viewer__colon {") || !strings.Contains(css, "margin-inline-end") {
		t.Errorf("the colon rule is gone — the space after each colon rides its margin:\n%s", css)
	}
}
