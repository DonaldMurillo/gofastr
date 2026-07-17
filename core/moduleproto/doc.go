// Package moduleproto implements the wire protocol between the GoFastr host
// process and an out-of-process third-party module, as specified by issue #37
// (design §4). It is the single source of truth for framing, the bidirectional
// Frame envelope, request/response correlation, version negotiation, and the
// method catalog's typed value shapes.
//
// The package is deliberately transport- and policy-neutral. It carries wire
// mechanics only:
//
//   - It does NOT import [github.com/DonaldMurillo/gofastr/framework] or any
//     framework subpackage, and it does NOT import core/mcp. The host↔module
//     pipe is a purpose-built JSON-RPC 2.0 dialect, not MCP. (MCP survives only
//     as an optional module *surface*, registered into a separate core/mcp.Server
//     by the supervisor — never on this pipe.)
//   - It enforces NO capability rules. The resource:verb intersection
//     (module-grant ∩ caller-authority) lives in the supervisor above this codec.
//   - The codec reads/writes an [io.Reader]/[io.Writer] pair — not os.Stdin /
//     os.Stdout specifically — so the wire format survives a future v2 socket
//     transport without re-opening the protocol (design §1).
//
// # Why not reuse framework/harness/mcpclient
//
// mcpclient structurally cannot dispatch child-originated requests: its read
// loop unmarshals every inbound line into a response struct with no Method
// field and treats id==0 as a notification sentinel. Once ids flow both ways
// (the host originates module.* requests AND the child originates host.*
// reverse requests over the SAME pipe), those assumptions misroute frames.
// moduleproto builds a fresh, symmetric, full-duplex codec instead.
//
// # The load-bearing property: per-direction ID correlation
//
// Each endpoint owns an independent monotonic id counter starting at 1.
// Host-originated id:7 and child-originated id:7 NEVER collide: a reader
// consults its local pending map ONLY for frames that are responses
// (method == "" && id present). A request with the same numeric id is handled
// by the request branch, which echoes the id back without touching the map.
// This is pinned by an interleaved bidirectional test in peer_test.go.
//
// # Frame envelope (design §4.3)
//
// One bidirectional Frame, discriminated per JSON-RPC 2.0 by the presence of
// method:
//
//	Frame {
//	  jsonrpc: "2.0"          // required; rejected otherwise
//	  id?:     uint64         // present ⇒ request or response; absent ⇒ notification
//	  method?: string         // present ⇒ REQUEST; absent ⇒ RESPONSE
//	  params?: raw            // requests only
//	  result?: raw            // success responses only
//	  error?:  {code,message,data?}
//	}
//
// id:0 is impossible on the wire: id counters start at 1, and UnmarshalJSON
// rejects a decoded id of 0. Notifications omit the id key entirely (no
// omitempty-zero sentinel).
//
// # Method catalog
//
// See [MethodHandshake] and the rest of the method constants for the full
// host→module (module.*) and module→host reverse (host.*) catalog. Params and
// results are pure value types in this package; the host broker above fills in
// capability and delegation semantics.
package moduleproto
