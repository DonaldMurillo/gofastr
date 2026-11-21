package noderender

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/node"
)

// This file pins the trust boundary that splits RenderNode (untrusted IR)
// from RenderTrustedNode (first-party IR): the action-attribute family that
// core-ui/runtime/frag/boot.js turns into window.__gofastr.trigger() calls.
// The per-attribute strip/keep tables live in noderender_security_test.go
// (TestIRCannotFireActions, TestTrustedIRKeepsActions,
// TestIRDropsScriptAndURLGadgets); this file holds the dimensions those
// tables do not: trust flowing down the whole tree, and case folding.

// Trust flows down the whole tree (renderNode passes its trusted flag to each
// child), so a child carrying data-action is stripped under RenderNode and kept
// under RenderTrustedNode.
func TestRenderNodeStripsActionAttrsOnNestedChild(t *testing.T) {
	root := func() node.Node {
		return node.Node{Kind: "div", Props: map[string]any{"data-testid": "root"}, Children: []node.Node{
			{Kind: "div", Props: map[string]any{
				"data-action":   "save",
				"data-param-id": "42",
				"data-testid":   "child",
			}},
		}}
	}

	untrusted := string(RenderNode(root()))
	if !strings.Contains(untrusted, `data-testid="child"`) {
		t.Fatalf("child did not render — sentinel lost. Rendered: %s", untrusted)
	}
	if strings.Contains(strings.ToLower(untrusted), "data-action") || strings.Contains(strings.ToLower(untrusted), "data-param") {
		t.Errorf("SECURITY: untrusted RenderNode kept a child action attr. Rendered: %s", untrusted)
	}

	trusted := string(RenderTrustedNode(root()))
	for _, want := range []string{`data-action="save"`, `data-param-id="42"`} {
		if !strings.Contains(trusted, want) {
			t.Errorf("trusted RenderTrustedNode dropped nested child %s. Rendered: %s", want, trusted)
		}
	}
}

// withoutActionAttrs and attrAllowed both fold the key to lowercase before
// matching, so a prop keyed Data-Action / DATA-ACTION is still recognized as
// the action attr, and OnClick is still recognized as an event handler, HTML
// attribute names are case-insensitive to the parser, so the casing must not be
// an escape hatch. Assert the rendered string contains none of those tokens.
func TestRenderNodeStripsActionAttrsCaseInsensitive(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
	}{
		{"mixed-case data-action", "Data-Action"},
		{"uppercase data-action", "DATA-ACTION"},
		{"mixed-case event handler", "OnClick"},
		{"uppercase event handler", "ONMOUSEOVER"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := string(RenderNode(node.Node{Kind: "div", Props: map[string]any{
				tc.key:        "alert(1)",
				"data-testid": "sentinel",
			}}))
			if !strings.Contains(got, `data-testid="sentinel"`) {
				t.Fatalf("element did not render at all — sentinel lost. Rendered: %s", got)
			}
			lowered := strings.ToLower(got)
			for _, token := range []string{strings.ToLower(tc.key), "alert(1)"} {
				if strings.Contains(lowered, token) {
					t.Errorf("SECURITY: case trick leaked %q through untrusted RenderNode. Rendered: %s", token, got)
				}
			}
		})
	}
}
