package framework

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/moduleproto"
)

// Module tool namespacing (design §5.1): every tool a process module
// exposes is registered into the host's *mcp.Server under
// `module.<name>.<tool>` so two modules cannot collide and every call is
// attributable to its owning module in the audit log.
const moduleToolPrefix = "module."

// moduleToolSep is the dot separating the module name from the tool id in
// the namespaced MCP tool name (`module.<name>.<tool>`).
const moduleToolSep = "."

// moduleToolNamespace returns the MCP tool name for one module tool. The
// leading `module.` prefix is the carve-out the composite call gate and
// ModuleFor dispatch on; it is reserved so an in-process tool cannot
// shadow a module tool (or vice versa).
func moduleToolNamespace(moduleName, toolID string) string {
	return moduleToolPrefix + moduleName + moduleToolSep + toolID
}

// splitModuleTool reverses [moduleToolNamespace]. ok is false unless the
// name is exactly `module.<name>.<tool>` with non-empty halves. Used by
// [ModuleToolRegistry.ModuleFor] and the composite call gate.
func splitModuleTool(name string) (moduleName, toolID string, ok bool) {
	if !strings.HasPrefix(name, moduleToolPrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(name, moduleToolPrefix)
	idx := strings.IndexByte(rest, moduleToolSep[0])
	if idx <= 0 || idx == len(rest)-1 {
		return "", "", false
	}
	return rest[:idx], rest[idx+1:], true
}

// ModuleToolDigest is the canonical SHA-256 of one module tool, computed
// over the same canonical JSON the install tool hashes when it mints a
// [ToolDigest]. The handshake byte-compares this against
// [ToolDigest.SHA256]; the install tool and tests call this so the bytes
// agree. The canonical form is the JSON encoding of {id,name,description,
// input_schema} in that field order — deterministic because struct field
// order is stable and the child echoes the same shape verbatim.
func ModuleToolDigest(tool moduleproto.Tool) string {
	type canon struct {
		ID          string          `json:"id"`
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
	}
	c := canon{
		ID:          tool.ID,
		Name:        tool.Name,
		Description: tool.Description,
		InputSchema: tool.InputSchema,
	}
	raw, err := json.Marshal(c)
	if err != nil {
		// json.Marshal of a fixed struct only fails on a non-encodable
		// InputSchema; treat that as a zero-digest (handshake will reject
		// the mismatch rather than silently accept it).
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// errModuleToolUnavailable is the retryable temp-unavailable error a
// module-tool MCP handler returns when the module is enabled but its child
// is not currently serving (Starting / Crashed / DrainingUpgrade / lease
// failing). Distinct from a capability denial: the tool stays LISTED (the
// call gate passes for enabled modules) and the agent may retry. Mirrors
// the proxy handler's 503 + Retry-After for HTTP routes (design §8 table,
// "MCP tool" row).
var errModuleToolUnavailable = errors.New("processmodule: module tool temporarily unavailable")

// ErrModuleToolNotReady is returned by [dispatchToolCall] when a tool is
// invoked whose module has no live Ready child. It is the retryable
// companion to the disabled-omit gate; the handler maps it to
// errModuleToolUnavailable before returning to the MCP server.
var ErrModuleToolNotReady = errors.New("processmodule: module tool not ready")

// ToolRegistrar is the supervisor's hook into the host MCP server: the
// supervisor verifies the tool surface at handshake (design §4.7 step 5)
// and hands the verified [moduleproto.Tool] slice to RegisterTools, which
// installs each under its namespaced id. The implementation is
// idempotent across respawns: the MCP handler it stamps in looks up the
// CURRENT live peer at call time, so a restarted child needs no
// re-registration. UnregisterTools is kept for symmetry; v1 leaves the
// tool registered and gates visibility through the call gate (core/mcp
// has no tool-removal API), so it is a tracking-only no-op documented as
// such.
type ToolRegistrar interface {
	RegisterTools(moduleName string, tools []moduleproto.Tool) error
	UnregisterTools(moduleName string) error
}

// ModuleToolRegistry implements [ToolRegistrar] against the host's
// *mcp.Server. One registry per App, shared across every process module;
// constructed in [App.RegisterProcessModule] and injected as the
// supervisor's ToolRegistrar. It also answers the composite call gate so
// disabled-module tools are omitted from tools/list (design §8).
type ModuleToolRegistry struct {
	server *mcp.Server
	sup    *ProcessModuleSupervisor

	mu       sync.Mutex
	byModule map[string]map[string]struct{} // module → set of namespaced tool names
	owner    map[string]string              // namespaced tool name → module
}

// NewModuleToolRegistry constructs a registry over server + sup. Either
// may be nil for tests that only exercise [RegisterTools]'s bookkeeping
// (a nil server makes RegisterTools track but not install; a nil sup
// makes dispatch return errModuleToolUnavailable).
func NewModuleToolRegistry(server *mcp.Server, sup *ProcessModuleSupervisor) *ModuleToolRegistry {
	return &ModuleToolRegistry{
		server:   server,
		sup:      sup,
		byModule: make(map[string]map[string]struct{}),
		owner:    make(map[string]string),
	}
}

// ModuleFor reports which module owns a namespaced tool name (or "" / ok=false
// for a non-module tool). The composite call gate consults this to route
// module tools to [GateForModule] and leave everything else on the
// existing in-process gate.
func (r *ModuleToolRegistry) ModuleFor(toolName string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	mod, ok := r.owner[toolName]
	return mod, ok
}

// GateForModule is the module-tool half of the composite call gate.
// Disabled module → non-nil error (tool OMITTED from tools/list and
// refused by tools/call, indistinguishable from uninstalled — design §8
// "not-enabled → omitted+refused"). Enabled module → nil (tool LISTED;
// the handler enforces the Ready layer and returns a retryable error when
// the child is not yet serving). This split mirrors the route gate's
// Enabled-only 404 vs the proxy handler's 503.
func (r *ModuleToolRegistry) GateForModule(moduleName string) error {
	if r.sup == nil {
		// No supervisor wired: treat as disabled so the tool is omitted.
		return errors.New("processmodule: no supervisor for module tool")
	}
	sl := r.sup.Slot(moduleName)
	if sl == nil {
		return errors.New("processmodule: module not registered")
	}
	snap := sl.snapshot()
	if !snap.enabled {
		return errors.New("processmodule: module disabled")
	}
	return nil
}

// RegisterTools installs each tool under `module.<name>.<tool>` after the
// supervisor has verified the tool list against the descriptor digests.
// Idempotent across respawns: a tool already tracked for THIS module is
// left in place (its MCP handler dispatches dynamically, so the stale
// handler is still correct). A name already owned by a DIFFERENT module,
// or shadowed by a non-module tool already in the server, is a hard
// collision and returns an error.
func (r *ModuleToolRegistry) RegisterTools(moduleName string, tools []moduleproto.Tool) error {
	if moduleName == "" {
		return errors.New("processmodule: RegisterTools: empty module name")
	}
	r.mu.Lock()
	if r.byModule[moduleName] == nil {
		r.byModule[moduleName] = make(map[string]struct{}, len(tools))
	}
	tracked := r.byModule[moduleName]
	type pending struct {
		namespaced string
		tool       moduleproto.Tool
	}
	var toInstall []pending
	for _, t := range tools {
		ns := moduleToolNamespace(moduleName, t.ID)
		if _, already := tracked[ns]; already {
			continue // idempotent: already installed for this module
		}
		if owner, taken := r.owner[ns]; taken && owner != moduleName {
			r.mu.Unlock()
			return fmt.Errorf("processmodule: tool %q collides with module %q", ns, owner)
		}
		toInstall = append(toInstall, pending{namespaced: ns, tool: t})
	}
	r.mu.Unlock()

	for _, p := range toInstall {
		schema := decodeModuleToolSchema(p.tool.InputSchema)
		h := r.newToolHandler(moduleName, p.tool.ID)
		if r.server != nil {
			if err := r.server.RegisterTool(p.namespaced, p.tool.Description, schema, h); err != nil {
				return fmt.Errorf("processmodule: register tool %q: %w", p.namespaced, err)
			}
		}
		r.mu.Lock()
		r.byModule[moduleName][p.namespaced] = struct{}{}
		r.owner[p.namespaced] = moduleName
		r.mu.Unlock()
	}
	return nil
}

// UnregisterTools is a tracking-only no-op in v1: core/mcp has no
// tool-removal API, so the tool stays in the server. Visibility is gated
// by the composite call gate (a disabled/uninstalled module's tools are
// omitted + refused). owner[] is intentionally kept populated so
// GateForModule keeps refusing the tool until the module re-registers;
// clearing it here would let the tool leak back through the in-process
// gate's "no owner → pass" path. Documented as a known limitation for v1.
func (r *ModuleToolRegistry) UnregisterTools(moduleName string) error {
	_ = moduleName
	return nil
}

// Compile-time: ModuleToolRegistry satisfies ToolRegistrar.
var _ ToolRegistrar = (*ModuleToolRegistry)(nil)

// moduleToolCallID mints a host-side correlation id for module.tool.call,
// mirroring [proxyCallID] for module.http.
var moduleToolCallID atomic.Uint64

// newToolHandler builds the mcp.ToolHandler for one namespaced tool. The
// handler resolves the CURRENT live peer at call time (so respawns need
// no re-registration), enforces the Ready layer (enabled-but-not-Ready →
// retryable error), mints a delegation handle from the calling agent's
// context, and forwards module.tool.call through the same capability
// broker as module.http — the child's reverse host.* calls are then
// checked as module-grant ∩ caller-authority exactly like a proxied
// browser request (design §5.1 "same broker as HTTP").
func (r *ModuleToolRegistry) newToolHandler(moduleName, toolID string) mcp.ToolHandler {
	return func(ctx context.Context, params map[string]any) (any, error) {
		if r.sup == nil {
			return nil, errModuleToolUnavailable
		}
		raw, err := r.sup.dispatchToolCall(ctx, moduleName, toolID, params)
		if err != nil {
			if errors.Is(err, ErrModuleToolNotReady) {
				return nil, errModuleToolUnavailable
			}
			return nil, err
		}
		return raw, nil
	}
}

// decodeModuleToolSchema parses a tool's input_schema (json.RawMessage)
// into the map[string]any shape mcp.RegisterTool expects. A nil/empty or
// non-object schema yields an empty map (the tool is still callable with
// arbitrary arguments; the child enforces its own schema).
func decodeModuleToolSchema(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return map[string]any{}
	}
	return schema
}

// dispatchToolCall is the host-side forward of one MCP tool invocation to
// the named module's live child. It is the tool-surface twin of
// [ProcessModuleSupervisor.serveProxy]: same two-layer gate (Enabled is
// already enforced by the call gate; Ready + lease here), same delegation
// mint so reverse host.* calls re-attach the calling agent's authority.
//
// The child's reverse calls are capability-checked by the SAME broker
// (already installed on the peer at spawn) — there is no second permission
// vocabulary for tools (design §5.1 "grants"). The capability intersection
// (module-grant ∩ caller-authority, incl. the CrossOwnerRead carve-out)
// therefore governs a tool's host.* calls identically to an HTTP route
// with the same grants; the forward module.tool.call itself adds no new
// authority — it is the calling agent's authority, delegated.
func (s *ProcessModuleSupervisor) dispatchToolCall(ctx context.Context, moduleName, toolID string, params map[string]any) (json.RawMessage, error) {
	sl := s.Slot(moduleName)
	if sl == nil {
		return nil, ErrModuleToolNotReady
	}
	snap := sl.snapshot()
	if !servingState(snap.state, snap.leaseFailingUnsafe()) {
		return nil, ErrModuleToolNotReady
	}
	peer := snap.peer
	if peer == nil {
		return nil, ErrModuleToolNotReady
	}

	// Mint a delegation handle from the calling agent's context so a
	// reverse host.* call re-attaches THIS caller's authority to the CRUD
	// re-dispatch (design §5.1 ↔ §5). The agent's authority arrives in ctx
	// (roles / policy / user); MintDelegation snapshots them. An anonymous
	// agent (no user) mints an ambient handle — the broker's owner/tenant
	// gates then refuse owner-scoped reads by construction.
	callID := moduleToolCallID.Add(1)
	mintReq := requestFromToolCtx(ctx)
	handle, release := s.broker.MintDelegation(mintReq, callID)
	defer release()

	args, err := json.Marshal(params)
	if err != nil {
		args = nil
	}
	deadline := time.Now().Add(sl.callDeadline())
	callCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	raw, err := peer.Call(callCtx, moduleproto.MethodToolCall, moduleproto.ToolCallParams{
		ToolID:         toolID,
		Arguments:      args,
		Caller:         callerFromCtx(ctx, handle),
		DeadlineUnixMs: deadline.UnixMilli(),
	})
	if err != nil {
		return nil, err
	}
	var res moduleproto.ToolCallResult
	if jErr := json.Unmarshal(raw, &res); jErr != nil {
		return nil, fmt.Errorf("processmodule: decode tool.call result: %w", jErr)
	}
	return res.Result, nil
}

// requestFromToolCtx builds a minimal *http.Request carrying the calling
// agent's context so [Broker.MintDelegation] can snapshot roles + policy
// (it reads them off r.Context()). MCP tool calls arrive with no
// Cookie/Authorization header — the agent's authority is role/policy
// based, not session-cookie based — so cookie/auth stay empty and the
// broker re-attaches via roles+policy, which is the correct model for an
// agent-originated tool invocation.
func requestFromToolCtx(ctx context.Context) *http.Request {
	r := &http.Request{
		Method: http.MethodPost,
		URL:    &url.URL{Path: "/"},
		Header: map[string][]string{},
	}
	return r.WithContext(ctx)
}

// callerFromCtx assembles the moduleproto.Caller block for a tool
// invocation: Subject from the resolved user (diagnostic — the broker
// re-attaches the real identity via the delegation handle, never off the
// echoed subject), Delegation = the minted handle.
func callerFromCtx(ctx context.Context, handle string) moduleproto.Caller {
	c := moduleproto.Caller{Delegation: handle}
	if u, ok := handler.GetUser(ctx); ok && u != nil {
		c.Subject = fmt.Sprint(u)
	}
	return c
}
