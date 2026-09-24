package headless

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The tree: a WAI-ARIA treeview whose rows are server-rendered and
// whose whole keyboard contract — the roving tabindex (one treeitem
// carries tabindex=0, every other carries -1), the arrows, Home and
// End, type-ahead, expand and collapse — is bound by the registered
// headless-tree module on the data-hui-tree marker. The no-script
// contract is the rendered tree itself: every leaf link is a real
// anchor, every branch's children are real markup (a branch that
// lazy-loads keeps a hidden group the kernel's rpc primitive fills on
// first expand, exactly as far as the pattern went). The module's
// keyboard path drives the same toggle button a click drives, so any
// lazy-load wiring on it fires either way.

// Tree parts. The row holds the toggle and the label; the group is
// the child list a branch reveals.
const (
	PartTreeItem   Part = "tree-item"
	PartTreeRow    Part = "tree-row"
	PartTreeToggle Part = "tree-toggle"
	PartTreeGroup  Part = "tree-group"
)

// TreeNode is one entry in the tree.
type TreeNode struct {
	// ID is unique within the tree and becomes the treeitem's element
	// id. Required, and a bare id: letters, digits, `-`, `_` — a
	// space, quote, `#` or markup refuses at render. Unlike
	// SortableItem.Key this is not data: the id pairs with
	// href="#<id>" fragments and signal names, so slugify stored
	// values before they reach the tree.
	ID string
	// Label is the row's visible text. Required.
	Label string
	// Href, when set, makes the row's label a link. A branch with
	// Href keeps its toggle; a dangerous href degrades to a plain
	// label rather than a clickable vector.
	Href string
	// Children are statically-known descendants. Empty for leaves.
	Children []TreeNode
	// LazyPath, when set and Children is empty, makes this a branch
	// whose children load on first expand: the toggle carries the
	// kernel's rpc wiring against LazyPath and the child group is
	// bound to the signal the response swaps in. Children wins when
	// both are set — the children are already there.
	LazyPath string
	// Expanded forces the branch open on first paint.
	Expanded bool
	// Selected sets aria-selected="true" on the treeitem.
	Selected bool
}

type TreeProps struct {
	// Label is the aria-label on the role="tree" wrapper. Required.
	Label string
	// Nodes are the root-level entries. Required and non-empty: a
	// tree with nothing in it is a landmark that says nothing.
	Nodes []TreeNode
	// LazySignalPrefix names the signal namespace the lazy branches
	// bind their child groups to (each lazy branch's group carries
	// data-fui-signal="<prefix>-<node-id>"). Required when any node
	// uses LazyPath, ignored otherwise.
	LazySignalPrefix string

	// ID is the tree wrapper's element id. Required.
	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs on the root and the rows. No part is fillable: the
	// tree is the contract.
	Parts Parts
}

// Tree renders the treeview.
func Tree(p TreeProps, s Classes) render.HTML {
	if p.ID == "" {
		panic("headless: Tree requires ID — the treeitem ids and the lazy signal names hang off the tree's own identity; pass one")
	}
	checkLabel("Tree", "Label", p.Label)
	if len(p.Nodes) == 0 {
		panic("headless: Tree requires at least one Node — an empty tree is a landmark that names nothing; render nothing at all instead")
	}
	if treeNeedsSignals(p.Nodes) && p.LazySignalPrefix == "" {
		panic("headless: Tree requires LazySignalPrefix when any Node uses LazyPath — the lazily loaded group has to be bound to a signal the RPC response can swap")
	}
	ids := make([]string, 0, 8)
	treeCheckNodes("Tree", p.Nodes, &ids)
	checkNoDuplicateIDs("Tree", ids)
	b := p.Parts.Box(s)
	own := Merge(Safe(p.ExtraAttrs, "aria-label", "id"), Attrs(map[string]string{
		"id":         p.ID,
		"aria-label": p.Label,
		"role":       "tree",
	}))
	Mark(own, "data-hui-tree")

	rendered := make([]render.HTML, len(p.Nodes))
	for i, n := range p.Nodes {
		rendered[i] = treeNode(b, n, 1, i+1, len(p.Nodes), p.LazySignalPrefix, i == 0)
	}
	return b.El("ul", PartRoot, own, rendered...)
}

// treeCheckNodes walks the node tree refusing blank ids and labels
// and collecting ids for the duplicate check.
func treeCheckNodes(component string, nodes []TreeNode, ids *[]string) {
	for _, n := range nodes {
		checkFragmentID(component+" node", "ID", n.ID)
		checkLabel(component+" node "+n.ID, "Label", n.Label)
		*ids = append(*ids, n.ID)
		treeCheckNodes(component, n.Children, ids)
	}
}

// treeNeedsSignals reports whether any node (at any depth) lazy-loads.
func treeNeedsSignals(nodes []TreeNode) bool {
	for _, n := range nodes {
		if n.LazyPath != "" {
			return true
		}
		if treeNeedsSignals(n.Children) {
			return true
		}
	}
	return false
}

// treeNode renders one treeitem. firstFocusable carries the roving
// tabindex: the first root row is the tree's keyboard entry point.
func treeNode(b Box, n TreeNode, level, pos, setSize int, signalPrefix string, firstFocusable bool) render.HTML {
	isBranch := len(n.Children) > 0 || n.LazyPath != ""

	itemAttrs := Attrs(map[string]string{
		"id":            n.ID,
		"role":          "treeitem",
		"aria-level":    strconv.Itoa(level),
		"aria-posinset": strconv.Itoa(pos),
		"aria-setsize":  strconv.Itoa(setSize),
	})
	if firstFocusable {
		itemAttrs["tabindex"] = "0"
	} else {
		itemAttrs["tabindex"] = "-1"
	}
	if isBranch {
		itemAttrs["aria-expanded"] = strconv.FormatBool(n.Expanded)
	}
	if n.Selected {
		itemAttrs["aria-selected"] = "true"
	}

	// The row: the toggle (branches only) beside the label.
	row := []render.HTML{}
	if isBranch {
		toggleAttrs := Attrs(map[string]string{
			"type": "button",
		})
		Mark(toggleAttrs, "data-hui-tree-toggle")
		// The toggle is decorative — the treeitem's aria-expanded is
		// the state a reader gets — so it is aria-hidden and out of
		// the tab order; the module returns focus to the row.
		toggleAttrs["aria-hidden"] = "true"
		toggleAttrs["tabindex"] = "-1"
		if n.LazyPath != "" && len(n.Children) == 0 {
			// Lazy: clicking the toggle fires the kernel's rpc
			// primitive; the response populates the child group
			// through the signal swap.
			Mark(toggleAttrs, "data-fui-rpc")
			toggleAttrs["data-fui-rpc-method"] = "POST"
			toggleAttrs["data-fui-rpc-signal"] = signalPrefix + "-" + n.ID
			toggleAttrs["data-fui-rpc"] = n.LazyPath
		}
		row = append(row, b.El("button", PartTreeToggle, toggleAttrs, render.Text("▶")))
	}
	// A dangerous Href is dropped and the node degrades to a plain
	// label rather than a clickable XSS vector.
	if href := urlsafe.CleanAnchor(n.Href); href != "" {
		row = append(row, b.El("a", PartLabel, Attrs(map[string]string{"href": href}),
			render.Text(scrubControlBytes(n.Label))))
	} else {
		row = append(row, b.El("span", PartLabel, nil, render.Text(scrubControlBytes(n.Label))))
	}

	body := []render.HTML{b.El("div", PartTreeRow, nil, row...)}
	if isBranch {
		groupAttrs := Attrs(map[string]string{"role": "group"})
		if !n.Expanded {
			Mark(groupAttrs, "hidden")
		}
		if n.LazyPath != "" && len(n.Children) == 0 {
			groupAttrs["data-fui-signal"] = signalPrefix + "-" + n.ID
			groupAttrs["data-fui-signal-mode"] = "html"
		}
		childRendered := make([]render.HTML, len(n.Children))
		for i, c := range n.Children {
			childRendered[i] = treeNode(b, c, level+1, i+1, len(n.Children), signalPrefix, false)
		}
		body = append(body, b.El("ul", PartTreeGroup, groupAttrs, childRendered...))
	}

	return b.El("li", PartTreeItem, itemAttrs, body...)
}

func init() {
	Register(Spec{
		Name:    "Tree",
		Anatomy: []Part{PartRoot, PartTreeItem, PartTreeRow, PartTreeToggle, PartTreeGroup, PartLabel},
		Hooks:   []string{"data-hui-tree", "data-hui-tree-toggle"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Tree(TreeProps{
				ID: "files", Label: "Project files",
				Nodes: []TreeNode{
					{ID: "src", Label: "src", Expanded: true, Children: []TreeNode{
						{ID: "src-main", Label: "main.go", Href: "#main"},
						{ID: "src-util", Label: "util.go", Href: "#util"},
					}},
					{ID: "vendor", Label: "vendor", LazyPath: "/tree/vendor"},
				},
				LazySignalPrefix: "files-tree", Parts: parts,
			}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "a tree with a selected leaf",
				Why:  "the roving tabindex puts the keyboard on the first row and aria-selected says which leaf is the current one — the whole keyboard contract is the module's, on hooks the markup carries",
				HTML: Tree(TreeProps{ID: "t", Label: "File system", Nodes: []TreeNode{
					{ID: "a", Label: "A", Href: "/a"},
					{ID: "b", Label: "B"},
					{ID: "c", Label: "C", Selected: true},
				}}, s),
			}, {
				Name: "an expanded branch",
				Why:  "children are real markup the server rendered, so a reader without script sees the whole branch — the module only moves focus and flips aria-expanded",
				HTML: Tree(TreeProps{ID: "fs", Label: "FS", Nodes: []TreeNode{
					{ID: "src", Label: "src", Expanded: true, Children: []TreeNode{
						{ID: "src-main", Label: "main.go", Href: "#main"},
						{ID: "src-util", Label: "util.go", Href: "#util"},
					}},
					{ID: "docs", Label: "docs", Children: []TreeNode{
						{ID: "docs-readme", Label: "README.md", Href: "#readme"},
					}},
				}}, s),
			}, {
				Name: "a lazy branch",
				Why:  "a branch whose children are not known at render keeps a hidden group the kernel's rpc primitive fills on first expand — the same contract the pattern shipped, on the framework's own wiring",
				HTML: Tree(TreeProps{ID: "lz", Label: "Lazy", LazySignalPrefix: "lz", Nodes: []TreeNode{
					{ID: "vendor", Label: "vendor", LazyPath: "/tree/vendor"},
				}}, s),
			}}
		},
	})
}
