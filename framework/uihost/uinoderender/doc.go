// Package uinoderender renders a validated ui.node.v1 tree
// ([github.com/DonaldMurillo/gofastr/core-ui/uinodev1.Tree]) as host-owned
// HTML built from framework/ui and core-ui/html primitives. The host, not
// the module, assigns every id, class, ARIA attribute, variant and
// data-cui-rpc URL; an unmapped shape or an unresolved ActionRef is an
// error, never markup.
package uinoderender
