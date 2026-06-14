package world

import "github.com/DonaldMurillo/gofastr/core-ui/node"

// Node-tree helpers (AssignNodeIDs, NewElementID, FindNodeByID, CloneNode)
// live in core-ui/node and are re-exported from world.go. The Page-level
// helper below stays here because Page is a World-IR concern, not a UI
// primitive.

// ClonePage produces a deep copy of p so a mutation doesn't share
// memory with the live world. Used by update_page_element so the
// patch is applied to a fresh copy that can be journaled atomically;
// the live world then swaps to the new page in one assignment.
func ClonePage(p *Page) *Page {
	if p == nil {
		return nil
	}
	out := *p
	if p.Layout != nil {
		layoutCopy := *p.Layout
		out.Layout = &layoutCopy
	}
	out.Tree = node.CloneNode(p.Tree)
	return &out
}
