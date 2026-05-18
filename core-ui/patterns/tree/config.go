package tree

// Node is one entry in the tree.
type Node struct {
	// ID is unique within the tree. Required.
	ID string

	// Label is the visible text. Required.
	Label string

	// Href, when set, makes the leaf label a link. Mutually
	// exclusive with Children/LazyPath.
	Href string

	// Children are statically-known descendants. Empty for leaves.
	Children []Node

	// LazyPath, when set, makes this a branch whose children load
	// on first expand via an RPC POST. Mutually exclusive with
	// Children — if both are set, Children wins on first render and
	// LazyPath is ignored (the children are already there).
	LazyPath string

	// Expanded forces this branch open on first paint. Default false.
	Expanded bool

	// Selected sets aria-selected="true" on the treeitem.
	Selected bool
}

// Config configures the tree wrapper.
type Config struct {
	// ID is the wrapper id. Required.
	ID string

	// Label is the aria-label on the role="tree" wrapper. Required.
	Label string

	// Nodes are the root-level nodes. Required.
	Nodes []Node

	// SignalPrefix names the signal namespace used for lazy-load
	// signal bindings — each LazyPath branch's child <ul role="group">
	// is bound to data-fui-signal="<prefix>-<node-id>". Required when
	// any node uses LazyPath; ignored otherwise.
	SignalPrefix string

	// Class is added to the tree wrapper.
	Class string
}
