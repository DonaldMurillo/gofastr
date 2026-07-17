package uinodev1

import "testing"

// TestValidateAcceptsMinimal checks the simplest valid tree decodes.
func TestValidateAcceptsMinimal(t *testing.T) {
	tree, err := Validate([]byte(`{
		"component": "heading",
		"props": {"level": 1, "text": "Welcome"}
	}`), DefaultLimits())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tree.Root.Component != CompHeading {
		t.Fatalf("component = %q, want heading", tree.Root.Component)
	}
	hp, ok := tree.Root.Props.(HeadingProps)
	if !ok {
		t.Fatalf("props type = %T, want HeadingProps", tree.Root.Props)
	}
	if hp.Level != 1 || hp.Text != "Welcome" {
		t.Fatalf("props = %+v, want {1, Welcome}", hp)
	}
	if len(tree.Root.Children) != 0 {
		t.Fatalf("children = %d, want 0", len(tree.Root.Children))
	}
	if tree.Root.ActionRef != "" {
		t.Fatalf("action_ref = %q, want empty", tree.Root.ActionRef)
	}
}

// TestValidateAcceptsAllComponents drives every closed-enum component
// with valid props. If a new component is added without a positive test,
// this test will fail (it covers the enum exhaustively).
func TestValidateAcceptsAllComponents(t *testing.T) {
	cases := []struct {
		name string
		json string
		comp Component
	}{
		{"stack", `{"component":"stack","props":{"direction":"vertical","gap":"md"},"children":[]}`, CompStack},
		{"cluster", `{"component":"cluster","props":{"gap":"sm"}}`, CompCluster},
		{"grid", `{"component":"grid","props":{"columns":3,"gap":"md"}}`, CompGrid},
		{"section", `{"component":"section","props":{"title":"S","subtitle":"sub"}}`, CompSection},
		{"card", `{"component":"card","props":{"title":"C","elevation":"low"}}`, CompCard},
		{"divider", `{"component":"divider"}`, CompDivider},
		{"heading", `{"component":"heading","props":{"level":2,"text":"Hi"}}`, CompHeading},
		{"paragraph", `{"component":"paragraph","props":{"text":"body"}}`, CompParagraph},
		{"text", `{"component":"text","props":{"text":"plain"}}`, CompText},
		{"strong", `{"component":"strong","props":{"text":"bold"}}`, CompStrong},
		{"em", `{"component":"em","props":{"text":"italic"}}`, CompEm},
		{"code", `{"component":"code","props":{"text":"x++"}}`, CompCode},
		{"small", `{"component":"small","props":{"text":"fine"}}`, CompSmall},
		{"badge", `{"component":"badge","props":{"text":"New","tone":"positive"}}`, CompBadge},
		{"detail-list", `{"component":"detail-list","props":{"items":[{"label":"L","value":"V"}]}}`, CompDetailList},
		{"key-value", `{"component":"key-value","props":{"items":[{"key":"K","value":"V"}]}}`, CompKeyValue},
		{"stat-card", `{"component":"stat-card","props":{"label":"L","value":"42","trend":"up"}}`, CompStatCard},
		{"data-table", `{"component":"data-table","props":{"columns":[{"key":"a","label":"A"}],"rows":[{"cells":[{"text":"1"}]}]}}`, CompDataTable},
		{"button", `{"component":"button","props":{"label":"Save","variant":"primary"},"action_ref":"save"}`, CompButton},
		{"link-action", `{"component":"link","props":{"text":"Act"},"action_ref":"act"}`, CompLink},
		{"image", `{"component":"image","props":{"src":"/img/logo.png","alt":"Logo"}}`, CompImage},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tree, err := Validate([]byte(c.json), DefaultLimits())
			if err != nil {
				t.Fatalf("Validate(%s): unexpected error: %v\ncase json: %s", c.name, err, c.json)
			}
			if tree.Root.Component != c.comp {
				t.Fatalf("component = %q, want %q", tree.Root.Component, c.comp)
			}
			if tree.Root.Props == nil {
				t.Fatalf("props is nil")
			}
		})
	}
}

// TestValidateAcceptsNestedTree checks a multi-level layout tree.
func TestValidateAcceptsNestedTree(t *testing.T) {
	tree, err := Validate([]byte(`{
		"component": "section",
		"props": {"title": "Dashboard"},
		"children": [
			{"component": "heading", "props": {"level": 2, "text": "Stats"}},
			{"component": "card", "props": {"title": "Revenue"},
			 "children": [
				{"component": "stat-card", "props": {"label": "MRR", "value": "$1k", "trend": "up"}}
			 ]}
		]
	}`), DefaultLimits())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tree.Root.Component != CompSection {
		t.Fatalf("root = %q, want section", tree.Root.Component)
	}
	if len(tree.Root.Children) != 2 {
		t.Fatalf("root children = %d, want 2", len(tree.Root.Children))
	}
	card := tree.Root.Children[1]
	if len(card.Children) != 1 {
		t.Fatalf("card children = %d, want 1", len(card.Children))
	}
	if card.Children[0].Component != CompStatCard {
		t.Fatalf("grandchild = %q, want stat-card", card.Children[0].Component)
	}
}

// TestValidateUsesDefaultsOnZeroLimits checks that a zero-value Limits
// gets the documented defaults, so the simplest call site is still safe.
func TestValidateUsesDefaultsOnZeroLimits(t *testing.T) {
	_, err := Validate([]byte(`{"component":"divider"}`), Limits{})
	if err != nil {
		t.Fatalf("zero Limits should default; got: %v", err)
	}
}

// TestValidateRejectsHeadingLevelOutOfRange checks the 1-6 range bound.
func TestValidateRejectsHeadingLevelOutOfRange(t *testing.T) {
	for _, level := range []int{0, 7, -1, 100} {
		j := `{"component":"heading","props":{"level":` + itoa(level) + `,"text":"x"}}`
		if _, err := Validate([]byte(j), DefaultLimits()); err == nil {
			t.Fatalf("level %d should reject, got nil", level)
		}
	}
	for _, level := range []int{1, 2, 3, 4, 5, 6} {
		j := `{"component":"heading","props":{"level":` + itoa(level) + `,"text":"x"}}`
		if _, err := Validate([]byte(j), DefaultLimits()); err != nil {
			t.Fatalf("level %d should accept, got: %v", level, err)
		}
	}
}

// TestValidateRejectsButtonWithoutActionRef checks the interactive rule.
func TestValidateRejectsButtonWithoutActionRef(t *testing.T) {
	_, err := Validate([]byte(`{"component":"button","props":{"label":"x"}}`), DefaultLimits())
	if err == nil {
		t.Fatalf("button without action_ref must reject")
	}
}

// TestValidateRejectsLinkWithoutToOrActionRef checks the link rule.
func TestValidateRejectsLinkWithoutToOrActionRef(t *testing.T) {
	cases := []string{
		`{"component":"link","props":{"text":"x"}}`,                            // neither
		`{"component":"link","props":{"text":"x","to":"/a"},"action_ref":"b"}`, // both
	}
	for _, j := range cases {
		if _, err := Validate([]byte(j), DefaultLimits()); err == nil {
			t.Fatalf("link without exactly one of to/action_ref must reject: %s", j)
		}
	}
}

// TestValidateRejectsActionRefOnWrongComponent checks that only button/link
// may carry an action_ref.
func TestValidateRejectsActionRefOnWrongComponent(t *testing.T) {
	_, err := Validate([]byte(`{"component":"divider","action_ref":"x"}`), DefaultLimits())
	if err == nil {
		t.Fatalf("action_ref on divider must reject")
	}
}

// TestValidateRejectsChildrenOnTextComponent checks the child policy.
func TestValidateRejectsChildrenOnTextComponent(t *testing.T) {
	_, err := Validate([]byte(`{
		"component": "heading",
		"props": {"level": 1, "text": "x"},
		"children": [{"component":"divider"}]
	}`), DefaultLimits())
	if err == nil {
		t.Fatalf("heading with children must reject")
	}
}

// itoa is a tiny strconv.Itoa replacement to keep imports minimal here.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
