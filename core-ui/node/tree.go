package node

import (
	"crypto/rand"
	"encoding/hex"
	"maps"
)

// AssignNodeIDs gives every node without an ID one from idGen.
func AssignNodeIDs(n *Node, idGen func() string) {
	if n == nil {
		return
	}
	if n.ID == "" {
		n.ID = idGen()
	}
	for i := range n.Children {
		AssignNodeIDs(&n.Children[i], idGen)
	}
}

// NewElementID returns a fresh element ID, "el_" and 12 hex characters.
func NewElementID() string {
	var buf [6]byte
	rand.Read(buf[:]) // never returns an error since Go 1.24
	return "el_" + hex.EncodeToString(buf[:])
}

// FindNodeByID returns the node with the given ID, its parent and its
// index in the parent; nil and -1 for the root.
func FindNodeByID(root *Node, id string) (target *Node, parent *Node, index int, ok bool) {
	if root == nil || id == "" {
		return nil, nil, -1, false
	}
	if root.ID == id {
		return root, nil, -1, true
	}
	return findNodeByIDRec(root, id)
}

func findNodeByIDRec(parent *Node, id string) (*Node, *Node, int, bool) {
	for i := range parent.Children {
		child := &parent.Children[i]
		if child.ID == id {
			return child, parent, i, true
		}
		if t, p, idx, ok := findNodeByIDRec(child, id); ok {
			return t, p, idx, true
		}
	}
	return nil, nil, -1, false
}

// CloneNode returns a deep copy of n.
func CloneNode(n Node) Node {
	out := Node{
		ID:   n.ID,
		Kind: n.Kind,
	}
	if n.Props != nil {
		out.Props = make(map[string]any, len(n.Props))
		maps.Copy(out.Props, n.Props)
	}
	if n.Bindings != nil {
		out.Bindings = make(map[string]string, len(n.Bindings))
		maps.Copy(out.Bindings, n.Bindings)
	}
	if n.Actions != nil {
		out.Actions = make(map[string]Action, len(n.Actions))
		maps.Copy(out.Actions, n.Actions)
	}
	if len(n.Children) > 0 {
		out.Children = make([]Node, len(n.Children))
		for i := range n.Children {
			out.Children[i] = CloneNode(n.Children[i])
		}
	}
	return out
}
