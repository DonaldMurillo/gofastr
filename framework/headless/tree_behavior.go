package headless

import (
	_ "embed"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed tree.js
var treeJS string

// TreeBehaviorName is the runtime module that binds the tree's
// data-hui-* hooks: the roving tabindex, the arrow/Home/End/type-ahead
// keyboard contract, and the expand/collapse that drives the same
// toggle button a click drives. It replaces the retired core-ui/runtime
// tree module.
const TreeBehaviorName = "headless-tree"

// The marker: the tree root. A lazy branch's toggle carries the
// kernel's data-fui-rpc wiring, so the rpc primitive is a requirement
// — the loader has it registered before this module evaluates, and
// the keyboard path's toggle.click() fires it exactly as a pointer
// click would.
var _ = uiregistry.RegisterBehavior(TreeBehaviorName, treeJS,
	uiregistry.Markers("[data-hui-tree]"),
	uiregistry.Requires("rpc"))
