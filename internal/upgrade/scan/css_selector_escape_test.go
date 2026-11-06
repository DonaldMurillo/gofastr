package scan

import "testing"

func TestCSSSelectorDecodesIdents(t *testing.T) {
	for _, selector := range []string{`.fui-pos-center > .fui-slot`, `.fui\2d pos-center > .fui\-slot`} {
		t.Run(selector, func(t *testing.T) {
			css := `.fui-pos-center > .fui\-slot {}
.fui-pos-center > .fui\2d slot {}
.fui\2d pos-center > .fui-slot {}
.other > .fui\2d slot {}
.fui-pos-center > .fui\-slot-extra {}
`
			note := selectorNote(selector)
			result := mustRun(t, cssWorkspace(t, map[string]string{"app.css": css}), note)
			if len(result.Hits[note]) != 3 {
				t.Fatalf("escaped selector hits = %+v, want three", result.Hits[note])
			}
			for _, hit := range result.Hits[note] {
				if hit.Line > 3 || hit.Col != 1 {
					t.Errorf("wrong source position: %+v", hit)
				}
			}
		})
	}
}

func TestCSSSelectorDecodesTagAndAttr(t *testing.T) {
	css := `s\65 ction[data\2d old] > .retir\65 d {}
section[data-old] > .retired-more {}
section[data-new] > .retired {}
`
	note := selectorNote("section[data-old] > .retired")
	result := mustRun(t, cssWorkspace(t, map[string]string{"app.css": css}), note)
	if hits := result.Hits[note]; len(hits) != 1 || hits[0].Line != 1 || hits[0].Col != 1 {
		t.Fatalf("escaped tag/attribute hits = %+v, want line 1", hits)
	}
}

func TestCSSSelectorKeepsTokenIdentity(t *testing.T) {
	for _, test := range []struct {
		name, selector, css string
	}{
		{"child", `.fui-pos-center > .fui-slot`, `.fui-pos-center\>.fui-slot {}`},
		{"adjacent", `.old + .slot`, `.old\+.slot {}`},
		{"sibling", `.old ~ .slot`, `.old\7e.slot {}`},
		{"list", `.old,.slot`, `.old\,.slot {}`},
		{"class", `.old.slot`, `.old\.slot {}`},
		{"literal-class", `.old\.slot`, `.old.slot {}`},
		{"partition", `.old\..slot`, `.old.\.slot {}`},
		{"id", `#panel.old`, `#panel\.old {}`},
		{"function", `:is(.old)`, `:is\(.old) {}`},
		{"descendant", `.old\> .slot`, `.old\>.slot {}`},
		{"unicode-suffix", `.old`, `.oldé {}`},
		{"escaped-suffix", `.old`, `.old\> {}`},
		{"literal-suffix", `.old\>`, `.old\>suffix {}`},
		{"numeric-token", `[role=\31]`, `[role=1] {}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			note := selectorNote(test.selector)
			result := mustRun(t, cssWorkspace(t, map[string]string{"app.css": test.css}), note)
			if hits := result.Hits[note]; len(hits) != 0 {
				t.Fatalf("literal identifier became selector syntax: %+v", hits)
			}
		})
	}
}

func TestCSSSelectorEscapedNamesMatch(t *testing.T) {
	for _, test := range []struct {
		selector, css string
	}{
		{`.old\> .slot`, `.old\3e  .slot {}`},
		{`.old\.slot`, `.old\2e slot {}`},
		{`#panel.old`, `#p\61 nel.old {}`},
		{`:is(.old)`, `:i\73(.old) {}`},
		{`.\31 23`, `.\00003123 {}`},
	} {
		t.Run(test.selector, func(t *testing.T) {
			note := selectorNote(test.selector)
			result := mustRun(t, cssWorkspace(t, map[string]string{"app.css": test.css}), note)
			if hits := result.Hits[note]; len(hits) != 1 || hits[0].Line != 1 || hits[0].Col != 1 {
				t.Fatalf("equivalent escaped selector hits = %+v, want line 1", hits)
			}
		})
	}
}
