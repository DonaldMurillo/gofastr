// Package tree provides a TreeView component built on the WAI-ARIA
// tree pattern. The Go-side renders the SSR-visible portion of the
// tree as nested <ul role="group">/<li role="treeitem"> elements. The
// runtime (core-ui/runtime/runtime.js) wires keyboard navigation —
// ArrowUp/Down/Left/Right, Home, End, Enter/Space, type-ahead — and
// drives expand/collapse through existing data-fui-rpc + signal
// machinery.
//
// Children may be:
//
//   - Rendered statically up-front (Children populated).
//   - Lazy-loaded on expand (Children empty, LazyPath set). Pressing
//     ArrowRight or Enter on a collapsed node clicks the node's
//     toggle button (data-fui-tree-toggle), which fires an RPC to
//     LazyPath. The server returns the inner HTML of the child
//     <ul role="group">, swapped in via the signal binding.
package tree
