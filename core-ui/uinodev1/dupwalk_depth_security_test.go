package uinodev1

import (
	"strings"
	"testing"
)

// nestedArrays is {"x":[[…]]} with the object at depth 1, so depth n
// holds n-1 arrays.
func nestedArrays(n int) []byte {
	return []byte(`{"x":` + strings.Repeat("[", n-1) + strings.Repeat("]", n-1) + `}`)
}

// The duplicate-key walk recurses once per nesting level and runs before
// decodeNode enforces MaxDepth, so its depth bound is encoding/json's.
// On Go 1.27 (encoding/json backed by json/v2) json.Decoder.Token
// refuses nesting past 10000; Go 1.24's tracked it with no limit, and a
// 1 MiB run of brackets cost about a million frames (2026-10-27 C5).
// This pins the bound the walk relies on: a toolchain without it fails
// here.
func TestDupWalkDepthBound(t *testing.T) {
	const jsonMaxDepth = 10000
	if err := rejectDuplicateKeys(nestedArrays(jsonMaxDepth)); err != nil {
		t.Fatalf("depth %d: %v, want the walk to pass it", jsonMaxDepth, err)
	}
	for name, doc := range map[string][]byte{
		"arrays":    nestedArrays(jsonMaxDepth + 1),
		"objects":   []byte(strings.Repeat(`{"x":`, jsonMaxDepth+1) + "1" + strings.Repeat("}", jsonMaxDepth+1)),
		"input cap": nestedArrays(DefaultLimits().MaxInputBytes/2 - 8),
	} {
		if err := rejectDuplicateKeys(doc); err == nil || !strings.Contains(err.Error(), "exceeded max depth") {
			t.Errorf("%s: %v, want encoding/json's depth refusal", name, err)
		}
	}
}
