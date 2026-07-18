// Package uinodev1 implements the closed ui.node.v1 wire type and validator
// for process-isolated third-party modules (design §9, issue #37).
//
// A module returns a JSON tree conforming to ui.node.v1 over the module
// protocol's ui.node.v1 body kind. The host validates it with [Validate]
// BEFORE mapping it to design-system components and rendering.
//
// # Why a distinct wire type (not core-ui/node.Node)
//
// core-ui/noderender is a *denylist* built for first-party IR: arbitrary
// props reach HTML attributes through its extraAttrs passthrough, which
// drops only style, srcdoc, and on* and forwards everything else verbatim.
// That lets a third party forge the exact trusted-runtime attributes
// (data-fui-rpc, data-fui-*) that runtime.js acts on — a trusted-channel
// forgery that CSP does not catch (design §9).
//
// This package's wire type makes the attack UNREPRESENTABLE rather than
// denied: there is no map[string]any prop bag, no Bindings, no Actions,
// and no passthrough. Props are a closed union of per-component structs,
// each carrying only typed scalar fields the host mapping needs. Every
// unknown field rejects the whole tree (via json.DisallowUnknownFields).
//
// # Responsibility split
//
// This package owns:
//   - JSON decode (single bounded walk, with depth/node/child/text caps),
//   - Component enum check (closed allowlist; unknown ⇒ whole-tree reject),
//   - Typed prop validation (per-component struct, DisallowUnknownFields),
//   - URL guard for `to` props (host-relative same-origin paths only),
//   - Action reference shape check (non-empty, ≤128 chars, no whitespace),
//   - Whole-tree fail-closed caps (see [DefaultLimits] / [Limits]).
//
// The HOST RENDERER owns (deliberately out of scope for this package):
//   - Mapping components to framework/ui + core-ui/html primitives,
//   - Assigning every id / class / ARIA attribute itself (modules cannot),
//   - Resolving ActionRef values to real data-fui-rpc URLs against the
//     module descriptor's installed routes (see [Renderer]).
//
// ActionRef values are OPAQUE STRINGS here. The validator checks only their
// shape. Resolution against installed routes is a host-side concern that
// lands in a later wave alongside the [Renderer] implementation, because
// route resolution requires the descriptor + router plumbing that does not
// exist yet in this package's dependency graph.
//
// # Input framing
//
// [Validate] accepts a []byte whose length is bounded by [Limits.MaxInputBytes]
// (default 1 MiB, mirroring core/mcp's maxMCPBodyBytes). Callers receiving
// the tree from an unbounded source (e.g. an io.Reader from the wire) MUST
// cap the reader first — e.g. via io.LimitReader — so a hostile publisher
// cannot exhaust host memory before Validate even runs.
package uinodev1

import "github.com/DonaldMurillo/gofastr/core/render"

// Renderer maps a validated ui.node.v1 tree to host-owned HTML.
//
// The contract a Renderer implementation MUST honor:
//
//   - It maps each [Component] to a framework/ui or core-ui/html primitive,
//     assigning every id, class, ARIA attribute, and visual variant itself.
//     Modules cannot influence these — there are no such fields on any prop
//     struct by design.
//
//   - It resolves every [Node.ActionRef] to a real data-fui-rpc URL by
//     looking the ref up in the module descriptor's installed route table.
//     An ActionRef that does not resolve to an installed route MUST be
//     rejected, never improvised.
//
//   - It MUST reject (return a non-nil error) rather than improvise for any
//     node whose Component or props it does not know how to map. The
//     validator's closed enum guarantees the component set is bounded, but
//     the renderer still owns the mapping table and may further restrict it.
//
// The Renderer interface is defined here so the validator and the renderer
// share a single typed [Tree]. No implementation ships in this package yet;
// it lands with the framework/ui serialization wave (design §9).
type Renderer interface {
	// Render converts a validated tree to HTML. The returned HTML is
	// trusted by the host's renderer pipeline (it produced it); callers
	// must not feed unvalidated input through this interface — always
	// run [Validate] first and pass the resulting [Tree].
	Render(t *Tree) (render.HTML, error)
}
