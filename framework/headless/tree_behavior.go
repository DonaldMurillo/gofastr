package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed tree.js
var treeJS string

// TreeBehaviorName is the runtime module that binds the tree's data-hui-* hooks.
const TreeBehaviorName = "headless-tree"

var _ = uiregistry.RegisterBehavior(TreeBehaviorName, treeJS,
	uiregistry.Markers("[data-hui-tree]"),
	uiregistry.Requires("rpc"))
