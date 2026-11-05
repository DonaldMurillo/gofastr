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
