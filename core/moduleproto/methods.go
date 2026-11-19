package moduleproto

import "encoding/json"

// Method names: module.* the host sends, host.* the child sends back, and
// the module.cancel notification.
const (
	MethodHandshake = "module.handshake"
	MethodHTTP      = "module.http"
	MethodReady     = "module.ready"
	MethodHealth    = "module.health"
	MethodDrain     = "module.drain"
	MethodToolList  = "module.tool.list"
	MethodToolCall  = "module.tool.call"

	MethodHostEntityQuery  = "host.entity.query"
	MethodHostEntityCreate = "host.entity.create"
	MethodHostEntityUpdate = "host.entity.update"
	MethodHostEntityDelete = "host.entity.delete"
	MethodHostSearchQuery  = "host.search.query"
	MethodHostEventEmit    = "host.event.emit"

	MethodCancel = "module.cancel"
)

// Limits is the per-child envelope the host sends at handshake; zero
// fields take the host defaults.
type Limits struct {
	FrameBytes int `json:"frame_bytes"`
	// DeadlineMs is the per-call deadline ceiling for module.* requests.
	DeadlineMs int `json:"deadline_ms"`
	// Inflight is the most module.* requests the host keeps in flight.
	Inflight int `json:"inflight"`
}

// DefaultLimits is the v1 default Limits.
var DefaultLimits = Limits{
	FrameBytes: DefaultMaxFrameBytes,
	DeadlineMs: 10_000,
	Inflight:   DefaultMaxInflight,
}

// HandshakeExpected holds the values the child must echo back in
// module.handshake. ArtifactSHA256 is sent but never compared.
type HandshakeExpected struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	// ArtifactSHA256 is the SHA-256 of the approved executable, hex-encoded.
	ArtifactSHA256 string `json:"artifact_sha256"`
	// SurfaceSHA256 is the digest of the approved routes, tools and grants.
	SurfaceSHA256 string `json:"surface_sha256"`
	// DesiredGeneration is ProcessModuleStore's desired-state counter.
	DesiredGeneration uint64 `json:"desired_generation"`
	// InstanceID is the random nonce the host minted for this spawn.
	InstanceID string `json:"instance_id"`
}

// HandshakeParams is module.handshake's params.
type HandshakeParams struct {
	Expected HandshakeExpected `json:"expected"`
	Grants   []string          `json:"grants"`
	Limits   Limits            `json:"limits"`
	Features []string          `json:"features,omitempty"`
	Critical []string          `json:"critical,omitempty"`
	Proto    ProtoRange        `json:"proto"`
}

// Identity is the child's echoed identity in module.handshake's result.
type Identity struct {
	Name              string `json:"name"`
	Version           string `json:"version"`
	InstanceID        string `json:"instance_id"`
	DesiredGeneration uint64 `json:"desired_generation"`
}

// HandshakeResult is module.handshake's result.
type HandshakeResult struct {
	Proto         ProtoRange `json:"proto"`
	Identity      Identity   `json:"identity"`
	SurfaceSHA256 string     `json:"surface_sha256"`
	Features      []string   `json:"features"`
	Ready         bool       `json:"ready"`
}

// HTTPMethod is an HTTP method on a module.http request.
type HTTPMethod string

// HTTPRequestParams is one proxied HTTP request, module.http's params.
type HTTPRequestParams struct {
	// RequestID is a correlation id for logs; module.cancel does not use it.
	RequestID  string            `json:"request_id"`
	RouteID    string            `json:"route_id"`
	Method     HTTPMethod        `json:"method"`
	PathParams map[string]string `json:"path_params,omitempty"`
	Query      map[string]string `json:"query,omitempty"`
	// Headers is the allowlist-filtered subset of inbound headers.
	Headers map[string]string `json:"headers,omitempty"`
	// BodyB64 is the raw request body, base64-encoded.
	BodyB64 string `json:"body_b64,omitempty"`
	Caller  Caller `json:"caller"`
	// DeadlineUnixMs is the call's absolute deadline in Unix milliseconds.
	DeadlineUnixMs int64 `json:"deadline_unix_ms"`
}

// Caller is the end-user context the host attaches to an outbound call
// (HTTPRequestParams, ToolCallParams). Inbound calls carry CallerRef.
type Caller struct {
	Subject string `json:"subject,omitempty"`
	Tenant  string `json:"tenant,omitempty"`
	// Delegation is the bearer handle the child echoes on its host.* calls.
	Delegation string `json:"delegation,omitempty"`
}

// CallerRef is the caller context on an inbound host.* call: only the
// delegation handle the host resolves the principal from.
type CallerRef struct {
	Delegation string `json:"delegation,omitempty"`
}

// HTTPResponseBodyKind is the kind of a module.http response body.
type HTTPResponseBodyKind string

const (
	BodyKindJSON     HTTPResponseBodyKind = "json"
	BodyKindText     HTTPResponseBodyKind = "text"
	BodyKindUINodeV1 HTTPResponseBodyKind = "ui.node.v1"
)

// HTTPResponseBody is a module.http response body.
type HTTPResponseBody struct {
	Kind  HTTPResponseBodyKind `json:"kind"`
	Value json.RawMessage      `json:"value"`
}

// HTTPResponseResult is module.http's result.
type HTTPResponseResult struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    HTTPResponseBody  `json:"body"`
}

// ReadyParams is module.ready's empty params.
type ReadyParams struct{}

// ReadyResult is module.ready's result; Ready means warmup is done.
type ReadyResult struct {
	Ready  bool   `json:"ready"`
	Detail string `json:"detail,omitempty"`
}

// HealthParams is module.health's empty params.
type HealthParams struct{}

// HealthResult is module.health's result.
type HealthResult struct {
	OK       bool `json:"ok"`
	Inflight int  `json:"inflight"`
}

// DrainParams asks the child to finish in-flight work.
type DrainParams struct {
	Reason     string `json:"reason"`
	DeadlineMs int    `json:"deadline_ms"`
}

// DrainResult reports the in-flight count when the drain ended.
type DrainResult struct {
	Inflight int `json:"inflight"`
}

// Tool is one entry of module.tool.list's result.
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

// ToolCallParams is module.tool.call's params.
type ToolCallParams struct {
	ToolID         string          `json:"tool_id"`
	Arguments      json.RawMessage `json:"arguments,omitempty"`
	Caller         Caller          `json:"caller"`
	DeadlineUnixMs int64           `json:"deadline_unix_ms"`
}

// ToolCallResult is module.tool.call's result.
type ToolCallResult struct {
	Result json.RawMessage `json:"result"`
}

// EntityQueryParams is host.entity.query's params; Select, Filter and Sort
// are the host's own DSL, left raw.
type EntityQueryParams struct {
	Entity string          `json:"entity"`
	Select json.RawMessage `json:"select,omitempty"`
	Filter json.RawMessage `json:"filter,omitempty"`
	Sort   json.RawMessage `json:"sort,omitempty"`
	Limit  int             `json:"limit,omitempty"`
	Offset int             `json:"offset,omitempty"`
	Caller CallerRef       `json:"caller"`
}

// EntityQueryResult is the result of host.entity.query.
type EntityQueryResult struct {
	Rows  json.RawMessage `json:"rows"`
	Total int             `json:"total,omitempty"`
}

// EntityMutationParams is host.entity.create, update and delete's params.
type EntityMutationParams struct {
	Entity  string          `json:"entity"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Filter  json.RawMessage `json:"filter,omitempty"`
	IDs     []string        `json:"ids,omitempty"`
	Caller  CallerRef       `json:"caller"`
}

// EntityMutationResult reports what changed.
type EntityMutationResult struct {
	Affected int             `json:"affected,omitempty"`
	Rows     json.RawMessage `json:"rows,omitempty"`
}

// SearchQueryParams is host.search.query's params.
type SearchQueryParams struct {
	Query  string          `json:"query"`
	Filter json.RawMessage `json:"filter,omitempty"`
	Limit  int             `json:"limit,omitempty"`
	Caller CallerRef       `json:"caller"`
}

// SearchQueryResult is the result of host.search.query.
type SearchQueryResult struct {
	Results json.RawMessage `json:"results"`
	Total   int             `json:"total,omitempty"`
}

// EventEmitParams is host.event.emit's params.
type EventEmitParams struct {
	Topic   string          `json:"topic"`
	Payload json.RawMessage `json:"payload"`
	Caller  CallerRef       `json:"caller"`
}

// EventEmitResult is the (currently empty) result of host.event.emit.
type EventEmitResult struct{}

// CancelParams is module.cancel's params. RequestID is the decimal frame id
// of the request to cancel, as CallWithID reported it, not
// HTTPRequestParams.RequestID.
type CancelParams struct {
	RequestID string `json:"request_id"`
}
