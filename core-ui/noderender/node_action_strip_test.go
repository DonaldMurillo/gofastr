package noderender

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/node"
)

// This file pins the trust boundary that splits RenderNode (untrusted IR)
// from RenderTrustedNode (first-party IR): the action-attribute family that
// core-ui/runtime/frag/boot.js turns into window.__gofastr.trigger() calls.
//
// withoutActionAttrs runs at the untrusted entry points only and removes
// data-action, data-action-* and data-param-*. The trusted entry points skip
// it, so a developer-authored blueprint can still wire button actions through
// those exact attributes. attrAllowed then independently strips the
// privileged data-fui-* family on BOTH paths, that is asserted here only for
// the untrusted path (the case the task names), since the code strips it
// regardless of trust.
//
// "Sentinel" attributes (class, data-testid) ride along in every case to prove
// the element still rendered and the strip was surgical, not a blanket drop.

// strippedActionAttrs is the closed set withoutActionAttrs removes. data-action
// is the click-handler form; data-action-* selects the event (and
// data-action-mount fires at hydration with no interaction); data-param-* is
// how the IR chooses the trigger's arguments.
var strippedActionAttrs = []string{
	"data-action",
	"data-action-click",
	"data-action-mount",
	"data-action-type",
	"data-param-id",
	"data-param-entity",
}

// RenderNode strips the action family from an element's own props.
func TestRenderNodeStripsActionAttrs(t *testing.T) {
	for _, attr := range strippedActionAttrs {
		t.Run(attr, func(t *testing.T) {
			got := string(RenderNode(node.Node{Kind: "div", Props: map[string]any{
				attr:          "deleteAllRecords",
				"data-testid": "sentinel",
			}}))
			if !strings.Contains(got, `data-testid="sentinel"`) {
				t.Fatalf("element did not render at all — sentinel lost. Rendered: %s", got)
			}
			if strings.Contains(strings.ToLower(got), strings.ToLower(attr)) {
				t.Errorf("SECURITY: untrusted RenderNode emitted action attr %s. Rendered: %s", attr, got)
			}
		})
	}
}

// RenderTrustedNode keeps the same family (emits it as an attribute), that is
// the whole reason the trusted variant exists. If it ever stops, every
// blueprint-generated action goes silently dead.
func TestRenderTrustedNodeKeepsActionAttrs(t *testing.T) {
	for _, attr := range strippedActionAttrs {
		t.Run(attr, func(t *testing.T) {
			got := string(RenderTrustedNode(node.Node{Kind: "div", Props: map[string]any{
				attr: "save",
			}}))
			want := attr + `="save"`
			if !strings.Contains(got, want) {
				t.Errorf("trusted RenderTrustedNode dropped %s. Rendered: %s", want, got)
			}
		})
	}
}

// RenderKind (the single-element entry point) strips the action family exactly
// like RenderNode does, it routes through withoutActionAttrs before
// renderKind.
func TestRenderKindStripsActionAttrs(t *testing.T) {
	for _, attr := range strippedActionAttrs {
		t.Run(attr, func(t *testing.T) {
			got := string(RenderKind("div", map[string]any{
				attr:          "deleteAllRecords",
				"data-testid": "sentinel",
			}, nil))
			if !strings.Contains(got, `data-testid="sentinel"`) {
				t.Fatalf("element did not render at all — sentinel lost. Rendered: %s", got)
			}
			if strings.Contains(strings.ToLower(got), strings.ToLower(attr)) {
				t.Errorf("SECURITY: RenderKind emitted action attr %s. Rendered: %s", attr, got)
			}
		})
	}
}

// RenderTrustedKind is RenderKind's first-party counterpart and keeps the family.
func TestRenderTrustedKindKeepsActionAttrs(t *testing.T) {
	for _, attr := range strippedActionAttrs {
		t.Run(attr, func(t *testing.T) {
			got := string(RenderTrustedKind("div", map[string]any{
				attr: "save",
			}, nil))
			want := attr + `="save"`
			if !strings.Contains(got, want) {
				t.Errorf("trusted RenderTrustedKind dropped %s. Rendered: %s", want, got)
			}
		})
	}
}

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

// The privileged data-fui-* family drives the runtime (signals, RPC, polling,
// navigation). It is stripped on the untrusted path by attrAllowed's
// privilegedDataPrefixes check. Assert at least data-fui-rpc is removed.
func TestRenderNodeStripsPrivilegedDataFuiFamily(t *testing.T) {
	got := string(RenderNode(node.Node{Kind: "div", Props: map[string]any{
		"data-fui-rpc": "https://evil.example/r",
		"data-testid":  "sentinel",
	}}))
	if !strings.Contains(got, `data-testid="sentinel"`) {
		t.Fatalf("element did not render at all — sentinel lost. Rendered: %s", got)
	}
	if strings.Contains(strings.ToLower(got), "data-fui-rpc") {
		t.Errorf("SECURITY: untrusted RenderNode emitted privileged data-fui-rpc. Rendered: %s", got)
	}
}
