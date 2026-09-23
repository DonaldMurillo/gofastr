package headless

import (
	"strings"
	"testing"
)

func renderTree(p JSONTreeProps) string { return string(JSONTree(p, nil)) }

func TestJSONTreeDeterministicSortedKeys(t *testing.T) {
	// Two renders of the same map must be byte-identical: a map's
	// range order is random, and a tree whose bytes change per render
	// defeats every cache and golden.
	a := renderTree(JSONTreeProps{Value: map[string]any{
		"zeta": 1, "alpha": 2, "mid": map[string]any{"y": 1, "a": 2},
	}})
	b := renderTree(JSONTreeProps{Value: map[string]any{
		"mid": map[string]any{"a": 2, "y": 1}, "alpha": 2, "zeta": 1,
	}})
	if a != b {
		t.Errorf("two renders of the same value differ:\n%s\n%s", a, b)
	}
	if strings.Index(a, `"alpha"`) > strings.Index(a, `"mid"`) || strings.Index(a, `"mid"`) > strings.Index(a, `"zeta"`) {
		t.Errorf("object keys are not sorted:\n%s", a)
	}
}

func TestJSONTreeRendersNativeDetailsAndWords(t *testing.T) {
	h := renderTree(JSONTreeProps{Value: map[string]any{
		"tags":  []any{"fast"},
		"none":  nil,
		"empty": []any{},
	}})
	for _, want := range []string{
		`<details open`, `<summary`, `Object`, `Array`,
		`<li>`, "null", "[]",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("tree missing %q:\n%s", want, h)
		}
	}
}

func TestJSONTreeOpenDepthAndTruncation(t *testing.T) {
	deep := renderTree(JSONTreeProps{Value: map[string]any{
		"in": map[string]any{"leaf": 1},
	}, OpenDepth: 0})
	if want := 1; strings.Count(deep, "<details open") != want {
		t.Errorf("OpenDepth 0 should open exactly the root, got %d:\n%s", strings.Count(deep, "<details open"), deep)
	}
	all := renderTree(JSONTreeProps{Value: map[string]any{
		"in": map[string]any{"leaf": 1},
	}, OpenDepth: -1})
	if strings.Count(all, "<details open") != 2 {
		t.Errorf("OpenDepth -1 should open everything:\n%s", all)
	}
	trunc := renderTree(JSONTreeProps{Value: map[string]any{
		"s": "abcdefghijk",
	}, MaxStringLen: 5})
	if !strings.Contains(trunc, `abcde…`) || strings.Contains(trunc, `fghijk`) {
		t.Errorf("a long string must truncate with the mark:\n%s", trunc)
	}
}

func TestJSONTreeRefusesUnmarshalableValues(t *testing.T) {
	cases := []struct {
		name string
		v    any
	}{
		{"channel", make(chan int)},
		{"func", func() {}},
	}
	for _, tc := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: rendering should have been refused", tc.name)
				}
			}()
			JSONTree(JSONTreeProps{Value: tc.v}, nil)
		}()
	}
}
