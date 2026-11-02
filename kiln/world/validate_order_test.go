package world

import "testing"

// A node with two forbidden props is refused naming the same one on every
// call: ValidatePageTree ranged the props map, so the refusal named
// whichever key the iteration reached first.
func TestValidatePageTreeRefusalIsDeterministic(t *testing.T) {
	n := Node{Kind: "div", Props: map[string]any{"class": "a", "style": "b", "onclick": "c", "onload": "d"}}
	first := ValidatePageTree(n)
	if first == nil {
		t.Fatal("styling props accepted")
	}
	for range 64 {
		if err := ValidatePageTree(n); err == nil || err.Error() != first.Error() {
			t.Fatalf("refusal changed between calls: %q then %v", first, err)
		}
	}
}
