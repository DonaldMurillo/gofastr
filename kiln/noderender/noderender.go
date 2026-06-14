// Package noderender re-exports the first-party node renderer that now
// lives in core-ui/noderender. The node IR + renderer were moved down
// into core-ui so first-party consumers (the blueprint codegen) don't
// have to import the Kiln namespace; Kiln consumes them like any other
// caller. This shim keeps existing kiln-internal imports
// (kiln/render, examples) working unchanged.
package noderender

import corenoderender "github.com/DonaldMurillo/gofastr/core-ui/noderender"

// RenderNode walks a node tree and emits HTML. See
// core-ui/noderender.RenderNode. Accepts world.Node because world.Node is
// a type alias for core-ui/node.Node.
var RenderNode = corenoderender.RenderNode
