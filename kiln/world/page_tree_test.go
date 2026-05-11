package world

import (
	"strings"
	"testing"
)

func TestAssignNodeIDs_AssignsMissingPreservesExisting(t *testing.T) {
	root := &Node{
		Kind: "div",
		Children: []Node{
			{Kind: "heading", Props: map[string]any{"level": 1, "text": "Hi"}},
			{ID: "preset", Kind: "paragraph"},
			{Kind: "div", Children: []Node{
				{Kind: "link", Props: map[string]any{"href": "/x", "text": "x"}},
			}},
		},
	}
	counter := 0
	gen := func() string {
		counter++
		return "el_test_" + string(rune('a'+counter-1))
	}
	AssignNodeIDs(root, gen)

	if root.ID == "" {
		t.Error("root: expected an ID to be assigned")
	}
	if root.Children[0].ID == "" {
		t.Error("heading: expected an ID")
	}
	if root.Children[1].ID != "preset" {
		t.Errorf("preset id = %q, want preserved %q", root.Children[1].ID, "preset")
	}
	if root.Children[2].Children[0].ID == "" {
		t.Error("nested link: expected an ID assigned recursively")
	}
}

func TestAssignNodeIDs_Idempotent(t *testing.T) {
	root := &Node{
		Kind:     "div",
		Children: []Node{{Kind: "heading"}, {Kind: "paragraph"}},
	}
	gen := func() string { return NewElementID() }
	AssignNodeIDs(root, gen)
	first := snapshotIDs(root)
	AssignNodeIDs(root, gen)
	second := snapshotIDs(root)
	if first != second {
		t.Errorf("AssignNodeIDs is not idempotent — re-running mutated existing IDs.\nfirst:  %s\nsecond: %s", first, second)
	}
}

func snapshotIDs(n *Node) string {
	var b strings.Builder
	var walk func(n *Node)
	walk = func(n *Node) {
		b.WriteString(n.ID)
		b.WriteByte('|')
		for i := range n.Children {
			walk(&n.Children[i])
		}
	}
	walk(n)
	return b.String()
}

func TestNewElementID_FormatAndUniqueness(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := NewElementID()
		if !strings.HasPrefix(id, "el_") {
			t.Errorf("id %q missing el_ prefix", id)
		}
		if len(id) < 6 {
			t.Errorf("id %q suspiciously short", id)
		}
		if seen[id] {
			t.Errorf("collision: %q produced twice in 1000 calls", id)
		}
		seen[id] = true
	}
}

func TestFindNodeByID_Root(t *testing.T) {
	root := &Node{ID: "root", Kind: "div"}
	got, parent, idx, ok := FindNodeByID(root, "root")
	if !ok {
		t.Fatal("expected to find root by id")
	}
	if got != root {
		t.Error("FindNodeByID returned wrong target for root")
	}
	if parent != nil || idx != -1 {
		t.Errorf("root: parent=%v idx=%d, want nil/-1", parent, idx)
	}
}

func TestFindNodeByID_Nested(t *testing.T) {
	root := &Node{ID: "root", Kind: "div", Children: []Node{
		{ID: "a", Kind: "p"},
		{ID: "b", Kind: "div", Children: []Node{
			{ID: "b1", Kind: "link"},
			{ID: "b2", Kind: "link"},
		}},
	}}
	target, parent, idx, ok := FindNodeByID(root, "b2")
	if !ok {
		t.Fatal("expected to find b2")
	}
	if target.ID != "b2" {
		t.Errorf("target.ID = %q, want b2", target.ID)
	}
	if parent == nil || parent.ID != "b" {
		t.Errorf("parent.ID = %v, want b", parent)
	}
	if idx != 1 {
		t.Errorf("idx = %d, want 1", idx)
	}
}

func TestFindNodeByID_NotFound(t *testing.T) {
	root := &Node{ID: "root", Kind: "div"}
	_, _, _, ok := FindNodeByID(root, "nope")
	if ok {
		t.Error("expected not found for missing id")
	}
}

func TestClonePage_DeepCopy(t *testing.T) {
	orig := &Page{
		Path:    "/x",
		Version: 3,
		Tree: Node{
			ID:    "root",
			Kind:  "div",
			Props: map[string]any{"class": "x"},
			Children: []Node{
				{ID: "h", Kind: "heading", Props: map[string]any{"text": "Hi"}},
			},
		},
	}
	clone := ClonePage(orig)
	// Mutate the clone; original must be untouched.
	clone.Tree.Props["class"] = "mutated"
	clone.Tree.Children[0].Props["text"] = "Bye"
	clone.Version = 99

	if orig.Tree.Props["class"] != "x" {
		t.Errorf("original root props mutated through clone: %v", orig.Tree.Props)
	}
	if orig.Tree.Children[0].Props["text"] != "Hi" {
		t.Errorf("original child props mutated through clone: %v", orig.Tree.Children[0].Props)
	}
	if orig.Version != 3 {
		t.Errorf("original version mutated through clone: %d", orig.Version)
	}
}
