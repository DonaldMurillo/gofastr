package headless

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Tree parts. The row holds the toggle and the label; the group is a branch's child list.
const (
	PartTreeItem   Part = "tree-item"
	PartTreeRow    Part = "tree-row"
	PartTreeToggle Part = "tree-toggle"
	PartTreeGroup  Part = "tree-group"
)

// TreeNode is one entry in the tree.
type TreeNode struct {
	ID       string     // required, unique in the tree; letters, digits, - and _ only
	Label    string     // required: the row's text
	Href     string     // makes the label a link; an unsafe one renders a plain label
	Children []TreeNode // statically known descendants
	LazyPath string     // with no Children, the children load from here on first expand
	Expanded bool       // open on first paint; a lazy branch ignores it
	Selected bool       // aria-selected="true"
}

// TreeProps configures a tree.
type TreeProps struct {
	Label            string     // required: the tree's aria-label
	Nodes            []TreeNode // required, at least one
	LazySignalPrefix string     // required when a node uses LazyPath: groups bind "<prefix>-<node-id>"

	ID         string // required
	ExtraAttrs html.Attrs

	Parts Parts // nothing is fillable
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
		rendered[i] = treeNode(b, n, 1, i+1, len(p.Nodes), p.LazySignalPrefix, i == 0, true)
	}
	return b.El("ul", PartRoot, own, rendered...)
}

func treeCheckNodes(component string, nodes []TreeNode, ids *[]string) {
	for _, n := range nodes {
		checkFragmentID(component+" node", "ID", n.ID)
		checkLabel(component+" node "+n.ID, "Label", n.Label)
		*ids = append(*ids, n.ID)
		treeCheckNodes(component, n.Children, ids)
	}
}

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

// treeNode renders one treeitem; only the first root row takes tabindex 0, and only root rows carry the internal mark.
func treeNode(b Box, n TreeNode, level, pos, setSize int, signalPrefix string, firstFocusable, mark bool) render.HTML {
	isBranch := len(n.Children) > 0 || n.LazyPath != ""
	lazy := n.LazyPath != "" && len(n.Children) == 0
	expanded := n.Expanded && !lazy

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
		itemAttrs["aria-expanded"] = strconv.FormatBool(expanded)
	}
	if n.Selected {
		itemAttrs["aria-selected"] = "true"
	}

	row := []render.HTML{}
	if isBranch {
		toggleAttrs := Attrs(map[string]string{
			"type": "button",
		})
		Mark(toggleAttrs, "data-hui-tree-toggle")
		toggleAttrs["aria-hidden"] = "true"
		toggleAttrs["tabindex"] = "-1"
		if lazy {
			checkSameOrigin("Tree node "+strconv.Quote(n.ID), "LazyPath", n.LazyPath)
			Mark(toggleAttrs, "data-cui-rpc")
			toggleAttrs["data-cui-rpc-method"] = "POST"
			toggleAttrs["data-cui-rpc-signal"] = signalPrefix + "-" + n.ID
			toggleAttrs["data-cui-rpc"] = n.LazyPath
		}
		row = append(row, b.El("button", PartTreeToggle, toggleAttrs, render.Text("▶")))
	}
	if href := urlsafe.CleanAnchor(n.Href); href != "" {
		row = append(row, b.El("a", PartLabel, Attrs(map[string]string{"href": href, "tabindex": "-1"}),
			render.Text(scrubControlBytes(n.Label))))
	} else {
		row = append(row, b.El("span", PartLabel, nil, render.Text(scrubControlBytes(n.Label))))
	}

	body := []render.HTML{b.El("div", PartTreeRow, nil, row...)}
	if isBranch {
		groupAttrs := Attrs(map[string]string{"role": "group"})
		if !expanded {
			Mark(groupAttrs, "hidden")
		}
		if lazy {
			groupAttrs["data-cui-signal"] = signalPrefix + "-" + n.ID
			groupAttrs["data-cui-signal-mode"] = "html"
		}
		childRendered := make([]render.HTML, len(n.Children))
		for i, c := range n.Children {
			childRendered[i] = treeNode(b, c, level+1, i+1, len(n.Children), signalPrefix, false, false)
		}
		body = append(body, b.El("ul", PartTreeGroup, groupAttrs, childRendered...))
	}

	if mark {
		itemAttrs = Internal(itemAttrs)
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
