package headless

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// JSONTree's MaxStringLen compared and sliced BYTES, so a non-ASCII
// string was cut inside a rune and the tree rendered invalid UTF-8.
func TestJSONTreeTruncatesOnRuneBoundary(t *testing.T) {
	h := string(JSONTree(JSONTreeProps{Value: map[string]any{"name": "ééééé"}, MaxStringLen: 3}, nil))
	if !utf8.ValidString(h) {
		t.Fatalf("JSONTree output is not valid UTF-8: %q", h)
	}
	if !strings.Contains(h, `&quot;ééé…&quot;`) {
		t.Fatalf("MaxStringLen 3 did not keep three characters and the mark: %q", h)
	}
	if h := string(JSONTree(JSONTreeProps{Value: "ééé", MaxStringLen: 3}, nil)); strings.Contains(h, "…") {
		t.Fatalf("a string at the cap is truncated: %q", h)
	}
}
