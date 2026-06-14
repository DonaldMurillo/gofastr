package node

import (
	"encoding/json"
	"testing"
)

func TestAssignNodeIDsPreservesAndFills(t *testing.T) {
	root := &Node{Kind: "div", Children: []Node{
		{ID: "keep", Kind: "span"},
		{Kind: "span"},
	}}
	n := 0
	gen := func() string { n++; return "gen" }
	AssignNodeIDs(root, gen)
	if root.ID == "" {
		t.Fatal("root should get an ID")
	}
	if root.Children[0].ID != "keep" {
		t.Errorf("user ID overwritten: %q", root.Children[0].ID)
	}
	if root.Children[1].ID != "gen" {
		t.Errorf("missing ID not filled: %q", root.Children[1].ID)
	}
	// Idempotent: a second pass assigns nothing new.
	before := n
	AssignNodeIDs(root, gen)
	if n != before {
		t.Errorf("AssignNodeIDs not idempotent: gen called %d extra times", n-before)
	}
}

func TestNewElementIDFormat(t *testing.T) {
	id := NewElementID()
	if len(id) != len("el_")+12 || id[:3] != "el_" {
		t.Errorf("unexpected id format: %q", id)
	}
	if NewElementID() == id {
		t.Error("two ids collided")
	}
}

func TestFindNodeByID(t *testing.T) {
	root := &Node{ID: "r", Kind: "div", Children: []Node{
		{ID: "a", Kind: "span"},
		{ID: "b", Kind: "span", Children: []Node{{ID: "c", Kind: "em"}}},
	}}
	if tn, _, _, ok := FindNodeByID(root, "r"); !ok || tn != root {
		t.Error("root not found")
	}
	tn, parent, idx, ok := FindNodeByID(root, "c")
	if !ok || tn.ID != "c" || parent.ID != "b" || idx != 0 {
		t.Errorf("nested find wrong: ok=%v id=%v parent=%v idx=%d", ok, tn, parent, idx)
	}
	if _, _, _, ok := FindNodeByID(root, "missing"); ok {
		t.Error("missing id reported found")
	}
}

func TestCloneNodeDeepCopy(t *testing.T) {
	src := Node{
		Kind:     "div",
		Props:    map[string]any{"class": "x"},
		Bindings: map[string]string{"text": "$v"},
		Actions:  map[string]Action{"click": {Kind: ActionNoop}},
		Children: []Node{{Kind: "span", Props: map[string]any{"text": "hi"}}},
	}
	cp := CloneNode(src)
	cp.Props["class"] = "mutated"
	cp.Children[0].Props["text"] = "bye"
	if src.Props["class"] != "x" {
		t.Error("clone shares Props map with source")
	}
	if src.Children[0].Props["text"] != "hi" {
		t.Error("clone shares child Props with source")
	}
}

func TestNodeJSONRoundTrip(t *testing.T) {
	src := Node{Kind: "button", Props: map[string]any{"text": "Save"}, Actions: map[string]Action{"click": {Kind: ActionEmitEvent, Params: map[string]any{"topic": "saved"}}}}
	b, err := json.Marshal(src)
	if err != nil {
		t.Fatal(err)
	}
	var got Node
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Kind != "button" || got.Actions["click"].Kind != ActionEmitEvent {
		t.Errorf("round-trip lost data: %+v", got)
	}
}
