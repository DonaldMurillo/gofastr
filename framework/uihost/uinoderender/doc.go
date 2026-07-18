// Package uinoderender maps a validated ui.node.v1 tree
// ([github.com/DonaldMurillo/gofastr/core-ui/uinodev1.Tree]) to host-owned
// HTML by composing the framework's design-system primitives —
// [github.com/DonaldMurillo/gofastr/framework/ui] and
// [github.com/DonaldMurillo/gofastr/core-ui/html].
//
// This is the host-side Renderer design §9 (issue #37) requires. It is the
// counterpart to the closed validator: where the validator makes
// data-fui-*, on*, Bindings/Actions, and arbitrary attribute props
// UNREPRESENTABLE on the wire, this renderer is what actually assigns
// every id, class, ARIA attribute, visual variant, and data-fui-rpc URL —
// because the module cannot (and must not) influence any of them.
//
// # Trust model
//
// The input [uinodev1.Tree] is ALREADY validated: the closed enum is
// enforced, props are typed scalars, URLs are host-relative, caps hold.
// The renderer does NOT re-validate structure. It DOES fail closed on two
// things the validator deliberately leaves to the host:
//
//   - Any component or prop shape its mapping table does not cover ⇒ a
//     non-nil error, never a debug comment or raw tag (the noderender
//     hole design §9 warns about).
//   - Any ActionRef that does not resolve via the injected
//     [ActionResolver] ⇒ a non-nil error, never a dead or guessed URL.
//
// # Layering
//
// The package lives under framework/uihost because that layer already
// owns the runtime script, the strict CSP, and the trusted-channel
// data-fui-rpc synthesis for first-party components. The renderer is a
// natural sibling: it produces the trusted HTML the runtime hydrates. It
// imports only downward (framework/ui, core-ui/html, core-ui/uinodev1,
// core/render) — no cycle.
package uinoderender
