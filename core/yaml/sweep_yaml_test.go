package yaml

import "testing"

// TestListItemKeyColumnLineIsSibling pins that a line at the item's key column after an empty first value is the item's next key.
func TestListItemKeyColumnLineIsSibling(t *testing.T) {
	node, err := Parse("- a:\n  b: 1\n")
	if err != nil {
		t.Fatal(err)
	}
	item := node.List[0].Map
	if item["b"] == nil || item["b"].Value != int64(1) {
		t.Fatalf("b = %#v, want the item's sibling key", item["b"])
	}
	if a := item["a"]; a == nil || a.Kind != Map || len(a.Map) != 0 {
		t.Fatalf("a = %#v, want an empty map", a)
	}
}
