package uinodev1

import (
	"strings"
	"testing"
)

// TestValidateRejectsForgedDataFuiProps proves the core repair for
// design §9: a third party cannot forge trusted-runtime attributes by
// smuggling data-fui-* keys into any component's props. DisallowUnknown-
// Fields rejects every unknown key whole-tree. Subtests cover a sample
// of components; the property holds for every closed-enum component
// because none of their prop structs has a data-* field.
func TestValidateRejectsForgedDataFuiProps(t *testing.T) {
	cases := []struct {
		name string
		json string
	}{
		{"heading data-fui-rpc", `{"component":"heading","props":{"level":1,"text":"x","data-fui-rpc":"/auth/logout"}}`},
		{"heading data-fui-island", `{"component":"heading","props":{"level":1,"text":"x","data-fui-island":"42"}}`},
		{"button data-fui-rpc", `{"component":"button","props":{"label":"x","data-fui-rpc":"/evil"},"action_ref":"a"}`},
		{"link data-fui-args", `{"component":"link","props":{"text":"x","to":"/a","data-fui-args":"evil"}}`},
		{"card data-fui-confirm", `{"component":"card","props":{"title":"x","data-fui-confirm":"evil"}}`},
		{"divider data-fui-anything", `{"component":"divider","data-fui-anything":"evil"}`},
		{"data-foo arbitrary", `{"component":"heading","props":{"level":1,"text":"x","data-foo":"evil"}}`},
		{"id attempt", `{"component":"divider","id":"evil"}`},
		{"class attempt", `{"component":"divider","class":"evil"}`},
		{"style attempt", `{"component":"divider","style":"evil"}`},
		{"aria-hidden attempt", `{"component":"divider","aria-hidden":"true"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Validate([]byte(c.json), DefaultLimits())
			if err == nil {
				t.Fatalf("forged-attr case must reject; accepted: %s", c.json)
			}
			// The error should mention decode / unknown fields, not silently pass.
			msg := err.Error()
			if !strings.Contains(msg, "decode") && !strings.Contains(msg, "unknown") {
				t.Fatalf("error should reference decode/unknown, got: %v", err)
			}
		})
	}
}

// TestValidateRejectsOnHandlerProps proves that on* inline-event-handler
// keys are rejected (DisallowUnknownFields makes them unrepresentable,
// not merely dropped as noderender does).
func TestValidateRejectsOnHandlerProps(t *testing.T) {
	cases := []string{
		`{"component":"button","props":{"label":"x","onclick":"evil()"},"action_ref":"a"}`,
		`{"component":"heading","props":{"level":1,"text":"x","onmouseover":"evil()"}}`,
		`{"component":"divider","onload":"evil()"}`,
		`{"component":"link","props":{"text":"x","to":"/a","onfocus":"evil()"}}`,
	}
	for _, j := range cases {
		if _, err := Validate([]byte(j), DefaultLimits()); err == nil {
			t.Fatalf("on* handler must reject: %s", j)
		}
	}
}

// TestValidateRejectsJavascriptURL proves the URL guard catches the most
// dangerous scheme — javascript: — at the link.to prop.
func TestValidateRejectsJavascriptURL(t *testing.T) {
	cases := []string{
		`{"component":"link","props":{"text":"x","to":"javascript:alert(1)"}}`,
		`{"component":"link","props":{"text":"x","to":"JaVaScRiPt:alert(1)"}}`,
		`{"component":"link","props":{"text":"x","to":"javascript:/*"}}`,
	}
	for _, j := range cases {
		_, err := Validate([]byte(j), DefaultLimits())
		if err == nil {
			t.Fatalf("javascript: URL must reject: %s", j)
		}
		if !strings.Contains(err.Error(), "url") {
			t.Fatalf("error should reference url guard, got: %v", err)
		}
	}
}

// TestValidateRejectsDataSchemeURL covers the data: scheme (the one CSP
// explicitly permits via img-src data:, which is why a semantic guard
// is required and not CSP alone — design §9).
func TestValidateRejectsDataSchemeURL(t *testing.T) {
	cases := []string{
		`{"component":"link","props":{"text":"x","to":"data:text/html,<script>alert(1)</script>"}}`,
		`{"component":"link","props":{"text":"x","to":"data:image/png;base64,abc"}}`,
	}
	for _, j := range cases {
		if _, err := Validate([]byte(j), DefaultLimits()); err == nil {
			t.Fatalf("data: URL must reject: %s", j)
		}
	}
}

// TestValidateRejectsSchemeRelativeURL covers "//host" — the off-origin
// scheme-relative form a naive "starts-with-/" check would miss.
func TestValidateRejectsSchemeRelativeURL(t *testing.T) {
	cases := []string{
		`{"component":"link","props":{"text":"x","to":"//evil.com/x"}}`,
		`{"component":"link","props":{"text":"x","to":"//evil.com"}}`,
	}
	for _, j := range cases {
		if _, err := Validate([]byte(j), DefaultLimits()); err == nil {
			t.Fatalf("scheme-relative URL must reject: %s", j)
		}
	}
}

// TestValidateRejectsBackslashURL covers the magic-backslash browser bug
// (browsers coerce "\" to "/" in some URL contexts, defeating the
// scheme-relative check).
func TestValidateRejectsBackslashURL(t *testing.T) {
	cases := []string{
		`{"component":"link","props":{"text":"x","to":"/\\evil.com"}}`,
		`{"component":"link","props":{"text":"x","to":"/path\\to"}}`,
		`{"component":"link","props":{"text":"x","to":"/\\javascript:alert(1)"}}`,
	}
	for _, j := range cases {
		if _, err := Validate([]byte(j), DefaultLimits()); err == nil {
			t.Fatalf("backslash URL must reject: %s", j)
		}
	}
}

// TestValidateRejectsOffOriginURL covers absolute http(s):// URLs to
// hosts other than the host origin.
func TestValidateRejectsOffOriginURL(t *testing.T) {
	cases := []string{
		`{"component":"link","props":{"text":"x","to":"https://example.com"}}`,
		`{"component":"link","props":{"text":"x","to":"http://evil.com/x"}}`,
		`{"component":"link","props":{"text":"x","to":"https://localhost:3000"}}`,
	}
	for _, j := range cases {
		if _, err := Validate([]byte(j), DefaultLimits()); err == nil {
			t.Fatalf("off-origin absolute URL must reject: %s", j)
		}
	}
}

// TestValidateRejectsDepthBomb proves the depth cap fires at exactly
// MaxDepth+1. DefaultMaxDepth is 32; a 33-deep chain must reject.
func TestValidateRejectsDepthBomb(t *testing.T) {
	// Build a 33-deep chain of nested cards.
	depth33 := nestedJSON(33)
	if _, err := Validate([]byte(depth33), DefaultLimits()); err == nil {
		t.Fatalf("depth-33 chain must reject")
	}
	// A depth-32 chain is accepted.
	depth32 := nestedJSON(32)
	if _, err := Validate([]byte(depth32), DefaultLimits()); err != nil {
		t.Fatalf("depth-32 chain must accept, got: %v", err)
	}
}

// nestedJSON builds a deeply-nested card JSON of the requested depth.
// depth=1 is a single card; depth=N wraps depth N-1 in another card.
func nestedJSON(depth int) string {
	if depth < 1 {
		depth = 1
	}
	s := `{"component":"card","props":{"title":"leaf"}}`
	for i := 1; i < depth; i++ {
		s = `{"component":"card","props":{"title":"x"},"children":[` + s + `]}`
	}
	return s
}

// TestValidateRejectsNodeCountBomb proves MaxNodes+1 nodes reject.
// DefaultMaxNodes is 500; 501 nodes reject. We use a balanced tree so
// the per-node child cap (128) is not what fires first — 1 root with
// 4 stack children, each holding N leaf dividers. Total = 1 + 4 + 4N.
//
//	N=124 → 501 nodes (reject); N=123 → 497 nodes (accept).
func TestValidateRejectsNodeCountBomb(t *testing.T) {
	reject := balancedJSON(124) // 1 + 4 + 4*124 = 501 nodes
	if _, err := Validate([]byte(reject), DefaultLimits()); err == nil {
		t.Fatalf("501-node tree must reject")
	}
	accept := balancedJSON(123) // 1 + 4 + 4*123 = 497 nodes
	if _, err := Validate([]byte(accept), DefaultLimits()); err != nil {
		t.Fatalf("497-node tree must accept, got: %v", err)
	}
}

// balancedJSON builds a root stack with 4 stack children, each holding
// `leavesPerChild` dividers. Total nodes = 1 + 4 + 4*leavesPerChild.
// Each node has at most max(4, leavesPerChild) ≤ 128 children, so the
// per-node child cap does not fire and the only relevant cap is node
// count.
func balancedJSON(leavesPerChild int) string {
	if leavesPerChild < 0 {
		leavesPerChild = 0
	}
	var inner strings.Builder
	for i := 0; i < leavesPerChild; i++ {
		if i > 0 {
			inner.WriteByte(',')
		}
		inner.WriteString(`{"component":"divider"}`)
	}
	var child strings.Builder
	for i := 0; i < 4; i++ {
		if i > 0 {
			child.WriteByte(',')
		}
		child.WriteString(`{"component":"stack","props":{},"children":[`)
		child.WriteString(inner.String())
		child.WriteString(`]}`)
	}
	return `{"component":"stack","props":{},"children":[` + child.String() + `]}`
}

// flatChildrenJSON builds a root stack with n-1 children, for n total nodes.
func flatChildrenJSON(totalNodes int) string {
	if totalNodes < 1 {
		totalNodes = 1
	}
	children := totalNodes - 1
	var b strings.Builder
	b.WriteString(`{"component":"stack","props":{},"children":[`)
	for i := 0; i < children; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"component":"divider"}`)
	}
	b.WriteString(`]}`)
	return b.String()
}

// TestValidateRejectsFatProp proves the per-prop string cap fires at
// MaxPropString+1 bytes. Default is 4 KiB.
func TestValidateRejectsFatProp(t *testing.T) {
	// 4097 chars in heading.text.
	fat := strings.Repeat("x", 4097)
	j := `{"component":"heading","props":{"level":1,"text":"` + fat + `"}}`
	if _, err := Validate([]byte(j), DefaultLimits()); err == nil {
		t.Fatalf("4097-byte prop must reject")
	}
	// Exactly 4096 is accepted.
	ok := strings.Repeat("x", 4096)
	jOK := `{"component":"heading","props":{"level":1,"text":"` + ok + `"}}`
	if _, err := Validate([]byte(jOK), DefaultLimits()); err != nil {
		t.Fatalf("4096-byte prop must accept, got: %v", err)
	}
}

// TestValidateRejectsTotalTextBomb proves the total-text cap fires at
// MaxTotalText+1 bytes. Default is 256 KiB. We use 65 headings with
// 4096 bytes each = 266240 bytes > 262144, but each prop stays under
// the per-prop cap so this exercise is purely the total cap.
func TestValidateRejectsTotalTextBomb(t *testing.T) {
	// 65 * 4096 = 266240 > 262144 (default MaxTotalText).
	// 65 nodes + 1 root = 66 nodes, well under MaxNodes=500.
	// 65 children + 1 root, depth 2, well under MaxDepth=32.
	chunk := strings.Repeat("x", 4096)
	var b strings.Builder
	b.WriteString(`{"component":"stack","props":{},"children":[`)
	for i := 0; i < 65; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"component":"heading","props":{"level":1,"text":"`)
		b.WriteString(chunk)
		b.WriteString(`"}}`)
	}
	b.WriteString(`]}`)
	if _, err := Validate([]byte(b.String()), DefaultLimits()); err == nil {
		t.Fatalf("total-text bomb must reject")
	}
}

// TestValidateRejectsChildrenBomb proves the per-node child cap fires at
// MaxChildrenPerNode+1. Default is 128; 129 children of one node reject.
func TestValidateRejectsChildrenBomb(t *testing.T) {
	// 129 children — but only if total nodes stays under MaxNodes=500.
	// 129 < 500, so this isolates the children-per-node cap.
	bomb := flatChildrenJSON(130) // root + 129 children = 130 nodes
	if _, err := Validate([]byte(bomb), DefaultLimits()); err == nil {
		t.Fatalf("129-children node must reject")
	}
	// Exactly 128 children is accepted (root + 128 = 129 nodes, OK).
	ok := flatChildrenJSON(129)
	if _, err := Validate([]byte(ok), DefaultLimits()); err != nil {
		t.Fatalf("128-children node must accept, got: %v", err)
	}
}

// TestValidateRejectsComponentSpoofing proves case-variant and HTML-tag
// names that look interactive do not match the closed enum.
func TestValidateRejectsComponentSpoofing(t *testing.T) {
	cases := []string{
		`{"component":"Button","props":{"label":"x"}}`,  // PascalCase
		`{"component":"BUTTON","props":{"label":"x"}}`,  // uppercase
		`{"component":"button ","props":{"label":"x"}}`, // trailing space
		`{"component":" button","props":{"label":"x"}}`, // leading space
		`{"component":"script","props":{}}`,             // HTML tag
		`{"component":"img","props":{}}`,                // HTML tag
		`{"component":"iframe","props":{}}`,             // HTML tag
		`{"component":"a","props":{}}`,                  // HTML tag
		`{"component":"div","props":{}}`,                // HTML tag
		`{"component":"","props":{}}`,                   // empty
		`{"component":"heading2","props":{}}`,           // near-miss
	}
	for _, j := range cases {
		_, err := Validate([]byte(j), DefaultLimits())
		if err == nil {
			t.Fatalf("spoofed component must reject: %s", j)
		}
		if !strings.Contains(err.Error(), "component") && !strings.Contains(err.Error(), "unknown") {
			t.Fatalf("error should reference component/unknown, got: %v", err)
		}
	}
}

// TestValidateRejectsEmptyTree covers empty / non-object roots.
func TestValidateRejectsEmptyTree(t *testing.T) {
	cases := []string{
		``,
		`   `,
		`\n\t`,
		`null`,
		`[]`,
		`42`,
		`"hello"`,
		`true`,
	}
	for _, j := range cases {
		if _, err := Validate([]byte(j), DefaultLimits()); err == nil {
			t.Fatalf("empty/non-object root must reject: %q", j)
		}
	}
}

// TestValidateRejectsNullNodes covers explicit null children.
func TestValidateRejectsNullNodes(t *testing.T) {
	cases := []string{
		`{"component":"stack","props":{},"children":[null]}`,
		`{"component":"stack","props":{},"children":[{"component":"divider"},null]}`,
	}
	for _, j := range cases {
		if _, err := Validate([]byte(j), DefaultLimits()); err == nil {
			t.Fatalf("null child must reject: %s", j)
		}
	}
}

// TestValidateRejectsDuplicateKeys covers JSON duplicate-key smuggling.
// Go's encoding/json silently uses the last value on dup keys; we reject.
func TestValidateRejectsDuplicateKeys(t *testing.T) {
	cases := []string{
		// Smuggle script by repeating component key.
		`{"component":"heading","component":"script","props":{"level":1,"text":"x"}}`,
		// Smuggle data-fui-rpc by repeating a prop key.
		`{"component":"heading","props":{"level":1,"level":2,"text":"x"}}`,
		// Dup at nested level.
		`{"component":"stack","props":{},"children":[{"component":"divider"},{"component":"divider"}],"children":[]}`,
	}
	for _, j := range cases {
		if _, err := Validate([]byte(j), DefaultLimits()); err == nil {
			t.Fatalf("duplicate keys must reject: %s", j)
		}
	}
}

// TestValidateRejectsSmuggledActions covers theBindings/Actions smuggling
// at the node level — they are not in the shadow struct, so Disallow-
// UnknownFields rejects them.
func TestValidateRejectsSmuggledActions(t *testing.T) {
	cases := []string{
		`{"component":"divider","actions":{"kind":"create_entity","params":{}}}`,
		`{"component":"button","props":{"label":"x"},"actions":{"kind":"emit_event"},"action_ref":"a"}`,
		`{"component":"heading","props":{"level":1,"text":"x"},"actions":{}}`,
	}
	for _, j := range cases {
		_, err := Validate([]byte(j), DefaultLimits())
		if err == nil {
			t.Fatalf("smuggled actions must reject: %s", j)
		}
		if !strings.Contains(err.Error(), "decode") && !strings.Contains(err.Error(), "unknown") {
			t.Fatalf("error should reference decode/unknown, got: %v", err)
		}
	}
}

// TestValidateRejectsSmuggledBindings covers Bindings smuggling.
func TestValidateRejectsSmuggledBindings(t *testing.T) {
	cases := []string{
		`{"component":"divider","bindings":{"x":"signal"}}`,
		`{"component":"button","props":{"label":"x"},"bindings":{"x":"y"},"action_ref":"a"}`,
	}
	for _, j := range cases {
		_, err := Validate([]byte(j), DefaultLimits())
		if err == nil {
			t.Fatalf("smuggled bindings must reject: %s", j)
		}
	}
}

// TestValidateRejectsTrailingJSON covers framing discipline — one object.
func TestValidateRejectsTrailingJSON(t *testing.T) {
	cases := []string{
		`{"component":"divider"}{}`,
		`{"component":"divider"} 42`,
		`{"component":"divider"} garbage`,
	}
	for _, j := range cases {
		if _, err := Validate([]byte(j), DefaultLimits()); err == nil {
			t.Fatalf("trailing bytes must reject: %s", j)
		}
	}
}

// TestValidateRejectsInputBytesCap proves the raw-input cap fires.
func TestValidateRejectsInputBytesCap(t *testing.T) {
	// 1 KiB cap; payload is >1 KiB.
	lim := Limits{MaxInputBytes: 1024}
	big := strings.Repeat(" ", 1025)
	// Even an otherwise-valid tree rejects when the raw bytes overflow.
	j := `{"component":"divider","ignored":"` + big + `"}`
	// Note: this also has an unknown field "ignored" but the byte cap
	// fires first (it's the earliest check).
	if _, err := Validate([]byte(j), lim); err == nil {
		t.Fatalf(">MaxInputBytes payload must reject")
	}
}

// TestValidateRejectsImageBadScheme mirrors the link.to URL guard but on
// the image.src prop — proves the host-relative guard covers media too
// (design §9 names image.src as a host-relative URL prop).
func TestValidateRejectsImageBadScheme(t *testing.T) {
	cases := []string{
		`{"component":"image","props":{"src":"javascript:alert(1)","alt":"x"}}`,
		`{"component":"image","props":{"src":"data:image/png;base64,abc","alt":"x"}}`,
		`{"component":"image","props":{"src":"//evil.com/x.png","alt":"x"}}`,
		`{"component":"image","props":{"src":"/\\evil.com/x.png","alt":"x"}}`,
		`{"component":"image","props":{"src":"https://example.com/x.png","alt":"x"}}`,
		`{"component":"image","props":{"src":"vbscript:msgbox","alt":"x"}}`,
		`{"component":"image","props":{"src":"blob:abc","alt":"x"}}`,
		`{"component":"image","props":{"src":"file:///etc/passwd","alt":"x"}}`,
	}
	for _, j := range cases {
		_, err := Validate([]byte(j), DefaultLimits())
		if err == nil {
			t.Fatalf("image bad-scheme must reject: %s", j)
		}
		if !strings.Contains(err.Error(), "url") {
			t.Fatalf("error should reference url guard, got: %v", err)
		}
	}
}

// TestValidateRejectsImageMissingFields proves image.src and image.alt
// are both required (Alt non-empty — modules must describe images).
func TestValidateRejectsImageMissingFields(t *testing.T) {
	cases := []string{
		`{"component":"image","props":{"alt":"x"}}`,               // missing src
		`{"component":"image","props":{"src":"/x.png"}}`,          // missing alt
		`{"component":"image","props":{"src":"/x.png","alt":""}}`, // empty alt
	}
	for _, j := range cases {
		if _, err := Validate([]byte(j), DefaultLimits()); err == nil {
			t.Fatalf("image with missing/empty required field must reject: %s", j)
		}
	}
}

// TestValidateAcceptsImageHostRelative proves a valid host-relative src
// with non-empty alt passes the validator (the renderer trusts this).
func TestValidateAcceptsImageHostRelative(t *testing.T) {
	tree, err := Validate([]byte(`{"component":"image","props":{"src":"/assets/logo.png","alt":"Acme logo"}}`), DefaultLimits())
	if err != nil {
		t.Fatalf("valid image must accept, got: %v", err)
	}
	if tree.Root.Component != CompImage {
		t.Fatalf("component = %q, want image", tree.Root.Component)
	}
	ip := tree.Root.Props.(ImageProps)
	if ip.Src != "/assets/logo.png" || ip.Alt != "Acme logo" {
		t.Fatalf("ImageProps = %+v", ip)
	}
}
