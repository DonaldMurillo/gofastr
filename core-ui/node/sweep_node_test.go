package node

import "testing"

func TestFindNodeByIDRootHasNoParent(t *testing.T) {
	root := &Node{ID: "r", Kind: "div", Children: []Node{{ID: "a", Kind: "span"}, {Kind: "em"}}}
	got, parent, idx, ok := FindNodeByID(root, "r")
	if !ok || got != root || parent != nil || idx != -1 {
		t.Fatalf("FindNodeByID(root) = %v, %v, %d, %v; want root, nil, -1, true", got, parent, idx, ok)
	}
	if _, _, idx, ok := FindNodeByID(root, ""); ok || idx != -1 {
		t.Fatalf("an empty id matched: idx=%d ok=%v", idx, ok)
	}
}
