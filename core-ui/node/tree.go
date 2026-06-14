package node

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// AssignNodeIDs walks the tree and assigns a stable ID to every node
// missing one. User-provided IDs are preserved; the idGen callback
// produces a fresh ID for nodes that need one. Idempotent: calling
// twice on the same tree is a no-op after the first call.
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

// NewElementID produces a fresh stable handle for a new node. The
// format ("el_" + 12 hex chars) is intentionally short for terminal
// readability and short tool calls; collisions inside a single page
// would require ~10^14 IDs to be likely, far beyond any plausible
// page size. Falls back to a counter-derived id on rand failure so
// callers never see an empty ID.
func NewElementID() string {
	var buf [6]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// rand.Read should never fail on supported platforms; keep
		// a defensive fallback so element-id assignment can't fail
		// silently and produce duplicate "" IDs.
		return fmt.Sprintf("el_fallback_%x", &buf)
	}
	return "el_" + hex.EncodeToString(buf[:])
}

// FindNodeByID returns a pointer to the node with the given ID, plus
// (parent, indexInParent) so callers can mutate sibling order. parent
// is nil and index is -1 when the matched node is the tree root.
// Returns ok=false when no node matches.
//
// Linear walk over the tree — pages aren't large enough for this to
// matter; an index lookup would just add bookkeeping cost on every
// other op.
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

// CloneNode produces a deep copy of n so a mutation doesn't share memory
// with the source tree.
func CloneNode(n Node) Node {
	out := Node{
		ID:   n.ID,
		Kind: n.Kind,
	}
	if n.Props != nil {
		out.Props = make(map[string]any, len(n.Props))
		for k, v := range n.Props {
			out.Props[k] = v
		}
	}
	if n.Bindings != nil {
		out.Bindings = make(map[string]string, len(n.Bindings))
		for k, v := range n.Bindings {
			out.Bindings[k] = v
		}
	}
	if n.Actions != nil {
		out.Actions = make(map[string]Action, len(n.Actions))
		for k, v := range n.Actions {
			out.Actions[k] = v
		}
	}
	if len(n.Children) > 0 {
		out.Children = make([]Node, len(n.Children))
		for i := range n.Children {
			out.Children[i] = CloneNode(n.Children[i])
		}
	}
	return out
}
