package headless

import "testing"

// Every RPC sink in the package refuses a cross-origin endpoint at
// render (checkSameOrigin): the runtime would silently decline it, or
// carry the request off the page. MenuItem.RPC, the RPCAttrs row's
// "/\host" spelling, TreeNode.LazyPath and SortableList's two paths
// reached the markup unchecked.
func TestRPCSinksAreSameOrigin(t *testing.T) {
	for _, bad := range []string{"https://evil.example/x", "//evil.example/x", `/\evil.example/x`, "x"} {
		mustRefuse(t, "a menu row RPC to "+bad, func() {
			Menu(MenuProps{Label: "M", Items: []MenuItem{{Label: "Go", RPC: bad}}}, nil)
		})
		mustRefuse(t, "a nested menu row RPC to "+bad, func() {
			Menu(MenuProps{Label: "M", Items: []MenuItem{{Label: "More", Children: []MenuItem{{Label: "Go", RPC: bad}}}}}, nil)
		})
		mustRefuse(t, "a menu row RPCAttrs data-cui-rpc of "+bad, func() {
			Menu(MenuProps{Label: "M", Items: []MenuItem{{Label: "Go",
				RPCAttrs: map[string]string{"data-cui-rpc": bad}}}}, nil)
		})
		mustRefuse(t, "a tree lazy branch on "+bad, func() {
			Tree(TreeProps{ID: "t", Label: "T", LazySignalPrefix: "t",
				Nodes: []TreeNode{{ID: "a", Label: "A", LazyPath: bad}}}, nil)
		})
		mustRefuse(t, "a sortable list RPCPath of "+bad, func() {
			SortableList(SortableListProps{Label: "L", RPCPath: bad}, nil)
		})
		mustRefuse(t, "a sortable list ConflictRPC of "+bad, func() {
			SortableList(SortableListProps{Label: "L", RPCPath: "/move", Version: "v1", ConflictRPC: bad}, nil)
		})
	}
	// The same-origin spellings still render.
	Menu(MenuProps{Label: "M", Items: []MenuItem{{Label: "Go", RPC: "/x"},
		{Label: "Too", RPCAttrs: map[string]string{"data-cui-rpc": "/y"}}}}, nil)
	Tree(TreeProps{ID: "t", Label: "T", LazySignalPrefix: "t",
		Nodes: []TreeNode{{ID: "a", Label: "A", LazyPath: "/kids"}}}, nil)
	SortableList(SortableListProps{Label: "L", RPCPath: "/move", Version: "v1", ConflictRPC: "/conflict?c=1"}, nil)
}
