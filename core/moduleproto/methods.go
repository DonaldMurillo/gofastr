package moduleproto

import "encoding/json"

// Method name constants for the moduleproto catalog (design §4.4).
//
// Host → module (requests):
//
//   - [MethodHandshake]: initialize the connection, negotiate proto, cross-check digests.
//   - [MethodHTTP]: proxy a single HTTP request to the module's route surface.
//   - [MethodReady]: one-time warmup probe (distinct from liveness).
//   - [MethodHealth]: ongoing liveness ping (idle-only).
//   - [MethodDrain]: ask the child to finish in-flight work.
//   - [MethodToolList]: optional MCP tool surface — list tools.
//   - [MethodToolCall]: optional MCP tool surface — invoke a tool.
//
// Module → host (reverse requests, each capability-checked by the supervisor's broker):
//
//   - [MethodHostEntityQuery/Create/Update/Delete]: host data-broker calls.
//   - [MethodHostSearchQuery]: host search-broker call.
//   - [MethodHostEventEmit]: host event-broker call.
//
// Notifications (no id):
//
//   - [MethodCancel]: host → module per-call deadline cancellation.
const (
	// Host → module.
	MethodHandshake = "module.handshake"
	MethodHTTP      = "module.http"
	MethodReady     = "module.ready"
	MethodHealth    = "module.health"
	MethodDrain     = "module.drain"
	MethodToolList  = "module.tool.list"
	MethodToolCall  = "module.tool.call"

	// Module → host (reverse).
	MethodHostEntityQuery  = "host.entity.query"
	MethodHostEntityCreate = "host.entity.create"
	MethodHostEntityUpdate = "host.entity.update"
	MethodHostEntityDelete = "host.entity.delete"
	MethodHostSearchQuery  = "host.search.query"
	MethodHostEventEmit    = "host.event.emit"

	// Notifications.
	MethodCancel = "module.cancel"
)

// Limits is the per-child resource envelope the host sets at handshake
// (design §4.4 module.handshake params). All zeros means "host defaults apply."
type Limits struct {
	// FrameBytes is the negotiated max_frame_bytes. Default 1 MiB
	// (see [DefaultMaxFrameBytes]). Must be ≤ the codec's structural
	// scanner cap (4 MiB); the codec rejects a larger negotiated value at
	// construction.
	FrameBytes int `json:"frame_bytes"`
	// DeadlineMs is the per-call deadline ceiling for module.* requests.
	// The descriptor may lower it; never raise. Default 10 000 (10 s).
	DeadlineMs int `json:"deadline_ms"`
	// Inflight is the maximum simultaneously in-flight module.* requests
	// the host will issue. Default 32. Over-cap is a clean call-local
	// error, not a protocol fault.
	Inflight int `json:"inflight"`
}

// DefaultLimits is the v1 default Limits the host applies absent descriptor
// narrowing.
var DefaultLimits = Limits{
	FrameBytes: DefaultMaxFrameBytes,
	DeadlineMs: 10_000,
	Inflight:   DefaultMaxInflight,
}

// HandshakeExpected carries the caller-supplied values the host expects the
// child to round-trip in module.handshake (design §4.7 steps 1-3). Every field
// here is an OPAQUE caller-supplied value — this package verifies the
// round-trip and emits a terminal [HandshakeMismatchError] on divergence; it
// does NOT decide whether the expected values are themselves trustworthy
// (that is the supervisor's policy — design §5 decision B).
type HandshakeExpected struct {
	// Name is the module's operator-approved name (descriptor-supplied).
	Name string `json:"name"`
	// Version is the module's operator-approved semantic version.
	Version string `json:"version"`
	// ArtifactSHA256 is the SHA-256 of the approved executable, hex-encoded.
	ArtifactSHA256 string `json:"artifact_sha256"`
	// SurfaceSHA256 is the digest of the approved surface descriptor
	// (routes + tool list + requested permissions). Mismatch with the
	// child's echoed value is terminal.
	SurfaceSHA256 string `json:"surface_sha256"`
	// DesiredGeneration is the monotonic desired-state counter persisted in
	// ProcessModuleStore. It is READ at spawn (not minted here); the child
	// echoes it back in identity. A mismatch means the child is stale.
	DesiredGeneration uint64 `json:"desired_generation"`
	// InstanceID is the random per-spawn liveness nonce the host minted for
	// THIS spawn (§4.7 step 1). Rejects stale/duplicate connections from a
	// prior spawn. Never persisted, never monotonic.
	InstanceID string `json:"instance_id"`
}

// HandshakeParams is the params shape for module.handshake (design §4.4).
// The host is the only legitimate originator; the child cannot alter these.
type HandshakeParams struct {
	Expected HandshakeExpected `json:"expected"`
	Grants   []string          `json:"grants"`
	Limits   Limits            `json:"limits"`
	Features []string          `json:"features,omitempty"`
	Critical []string          `json:"critical,omitempty"`
	Proto    ProtoRange        `json:"proto"`
}

// Identity is the child's echoed identity in module.handshake's result. The
// host compares every field against [HandshakeExpected] — instance_id and
// desired_generation MUST round-trip exactly (they are the spawn-freshness and
// generation-staleness anchors; §4.4 vocabulary note).
type Identity struct {
	Name              string `json:"name"`
	Version           string `json:"version"`
	InstanceID        string `json:"instance_id"`
	DesiredGeneration uint64 `json:"desired_generation"`
}

// HandshakeResult is the result shape for module.handshake (design §4.4).
type HandshakeResult struct {
	// Proto is the child's advertised proto range. The negotiated version
	// is computed by the host via [Negotiate] and returned alongside.
	Proto ProtoRange `json:"proto"`
	// Identity is the child's echoed identity — round-trips the host's
	// [HandshakeExpected] values.
	Identity Identity `json:"identity"`
	// SurfaceSHA256 is the child's view of its surface digest; the host
	// compares it to [HandshakeExpected].SurfaceSHA256.
	SurfaceSHA256 string `json:"surface_sha256"`
	// Features is the feature set the child ACKs.
	Features []string `json:"features"`
	// Ready is the child's initial ready state (typically false; the host
	// polls module.ready separately to gate on warmup completion).
	Ready bool `json:"ready"`
}

// HTTPMethod is the canonical method on a module.http request. We re-declare
// a string type rather than importing net/http to keep this package stdlib-only
// at the type level (the supervisor marshals real http.MethodX values into it).
type HTTPMethod string

// HTTPRequestParams carries a single proxied HTTP request to the module
// (design §4.4 module.http params). The host is responsible for stripping
// request body to the allowlisted headers, base64-encoding the body, and
// attaching the caller's delegation handle.
type HTTPRequestParams struct {
	// RequestID is a host-minted correlation id for THIS proxied request.
	// It is independent of the JSON-RPC id (which lives at the envelope
	// level) and is what module.cancel references to abort in-flight work.
	RequestID string `json:"request_id"`
	// RouteID is the installed route's descriptor id.
	RouteID string `json:"route_id"`
	// Method is the HTTP method (GET/POST/...).
	Method HTTPMethod `json:"method"`
	// PathParams are the parsed route path parameters.
	PathParams map[string]string `json:"path_params,omitempty"`
	// Query is the parsed query string parameters.
	Query map[string]string `json:"query,omitempty"`
	// Headers is the allowlist-filtered subset of inbound headers.
	Headers map[string]string `json:"headers,omitempty"`
	// BodyB64 is the raw request body, base64-encoded. Empty string for
	// bodies with no content.
	BodyB64 string `json:"body_b64,omitempty"`
	// Caller carries the resolved end-user context the host delegates to
	// the child for this call.
	Caller Caller `json:"caller"`
	// DeadlineUnixMs is the absolute deadline for this call, in Unix
	// milliseconds. The child must abort by this time regardless of any
	// module.cancel notification.
	DeadlineUnixMs int64 `json:"deadline_unix_ms"`
}

// Caller is the resolved end-user context attached to a proxied call (and
// echoed by the child on reverse host.* requests so the host re-attaches the
// right request context to internal re-dispatch). The Delegation handle is an
// in-memory, replica-local opaque string minted by the host (§5: v1 needs no
// signed token; the handle never leaves shared memory).
type Caller struct {
	// Subject is the resolved caller subject (user id, service id, …).
	Subject string `json:"subject,omitempty"`
	// Tenant is the resolved tenant id, if any.
	Tenant string `json:"tenant,omitempty"`
	// Delegation is the in-memory handle the host uses to re-attach the
	// originating request's context to a reverse host.* call.
	Delegation string `json:"delegation,omitempty"`
}

// HTTPResponseBodyKind enumerates the three kinds of body a module.http
// response may carry (design §4.4). kind:"ui.node.v1" is the closed UI node
// tree — validated, mapped, rendered, and hydrated by the host (design §9);
// the module NEVER emits raw HTML/CSS/JS.
type HTTPResponseBodyKind string

const (
	// BodyKindJSON is a JSON-typed response body.
	BodyKindJSON HTTPResponseBodyKind = "json"
	// BodyKindText is a plain-text response body.
	BodyKindText HTTPResponseBodyKind = "text"
	// BodyKindUINodeV1 is a closed ui.node.v1 tree. The host validates it
	// against the allowlist before rendering; see design §9.
	BodyKindUINodeV1 HTTPResponseBodyKind = "ui.node.v1"
)

// HTTPResponseBody is the body of a module.http response. The host buffers the
// entire response before committing any headers (the buffered-503 guarantee,
// design §8) — no streaming in v1.
type HTTPResponseBody struct {
	Kind  HTTPResponseBodyKind `json:"kind"`
	Value json.RawMessage      `json:"value"`
}

// HTTPResponseResult is the result shape for module.http (design §4.4).
type HTTPResponseResult struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    HTTPResponseBody  `json:"body"`
}

// ReadyParams is empty — module.ready takes no params (design §4.4).
type ReadyParams struct{}

// ReadyResult is the result of module.ready. ready:true means the child has
// completed warmup and may serve proxied traffic.
type ReadyResult struct {
	Ready  bool   `json:"ready"`
	Detail string `json:"detail,omitempty"`
}

// HealthParams is empty — module.health takes no params.
type HealthParams struct{}

// HealthResult is the result of module.health. ok:false means the child is
// alive but unhealthy; inflight reports current in-flight request count.
type HealthResult struct {
	OK       bool `json:"ok"`
	Inflight int  `json:"inflight"`
}

// DrainParams asks the child to wind down in-flight work (design §4.4
// module.drain). The host's drain sequence issues this before killing the child.
type DrainParams struct {
	Reason     string `json:"reason"`
	DeadlineMs int    `json:"deadline_ms"`
}

// DrainResult reports the in-flight count at drain completion.
type DrainResult struct {
	Inflight int `json:"inflight"`
}

// Tool is one entry in the module.tool.list result. The host registers these
// into its existing core/mcp.Server under a per-module prefix (design §5.1).
// At handshake the host verifies byte-equality with the descriptor's tool
// digests — the child cannot add, rename, or reshape at runtime.
type Tool struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

// ToolListResult is the result of module.tool.list.
type ToolListResult struct {
	Tools []Tool `json:"tools"`
}

// ToolCallParams invokes one tool by id (design §4.4 module.tool.call).
type ToolCallParams struct {
	ToolID         string          `json:"tool_id"`
	Arguments      json.RawMessage `json:"arguments,omitempty"`
	Caller         Caller          `json:"caller"`
	DeadlineUnixMs int64           `json:"deadline_unix_ms"`
}

// ToolCallResult is the result of module.tool.call. The host re-validates any
// ui.node.v1 content in Result exactly as it would for module.http.
type ToolCallResult struct {
	Result json.RawMessage `json:"result"`
}

// EntityQueryParams is the module→host reverse call host.entity.query.
// Filter/Sort/Select are json.RawMessage because their shape is the host's
// existing DSL (framework/filter, framework/dsl) — and this package must NOT
// import any framework/* leaf. The supervisor's broker parses these with the
// host's real query machinery on the receive side.
type EntityQueryParams struct {
	Entity string          `json:"entity"`
	Select json.RawMessage `json:"select,omitempty"`
	Filter json.RawMessage `json:"filter,omitempty"`
	Sort   json.RawMessage `json:"sort,omitempty"`
	Limit  int             `json:"limit,omitempty"`
	Offset int             `json:"offset,omitempty"`
	Caller Caller          `json:"caller"`
}

// EntityQueryResult is the result of host.entity.query.
type EntityQueryResult struct {
	Rows  json.RawMessage `json:"rows"`
	Total int             `json:"total,omitempty"`
}

// EntityMutationParams is the shape for host.entity.create / update / delete.
// For create/update, Payload is the row(s) to write (host-side schema). For
// delete, Filter/IDs select the target. The host's broker re-dispatches these
// through the CRUD requireScope chokepoint under the derived permission.
type EntityMutationParams struct {
	Entity  string          `json:"entity"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Filter  json.RawMessage `json:"filter,omitempty"`
	IDs     []string        `json:"ids,omitempty"`
	Caller  Caller          `json:"caller"`
}

// EntityMutationResult reports what changed.
type EntityMutationResult struct {
	Affected int             `json:"affected,omitempty"`
	Rows     json.RawMessage `json:"rows,omitempty"`
}

// SearchQueryParams is the module→host reverse call host.search.query.
type SearchQueryParams struct {
	Query  string          `json:"query"`
	Filter json.RawMessage `json:"filter,omitempty"`
	Limit  int             `json:"limit,omitempty"`
	Caller Caller          `json:"caller"`
}

// SearchQueryResult is the result of host.search.query.
type SearchQueryResult struct {
	Results json.RawMessage `json:"results"`
	Total   int             `json:"total,omitempty"`
}

// EventEmitParams is the module→host reverse call host.event.emit. The
// required permission is derived from <topic>:emit by the broker (design §4.4).
type EventEmitParams struct {
	Topic   string          `json:"topic"`
	Payload json.RawMessage `json:"payload"`
	Caller  Caller          `json:"caller"`
}

// EventEmitResult is the (currently empty) result of host.event.emit.
type EventEmitResult struct{}

// CancelParams is the body of the module.cancel notification (host → module).
// RequestID is the inbound request's host-side correlation id (the
// HTTPRequestParams.RequestID for a module.http call). The child uses it to
// find and cancel the in-flight handler's context.
type CancelParams struct {
	RequestID string `json:"request_id"`
}
