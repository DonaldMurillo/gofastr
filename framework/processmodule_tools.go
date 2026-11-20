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
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/moduleproto"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
)

const moduleToolPrefix = "module."

const moduleToolSep = "."

func moduleToolNamespace(moduleName, toolID string) string {
	return moduleToolPrefix + moduleName + moduleToolSep + toolID
}

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

// ModuleToolDigest is the hex SHA-256 of the tool's canonical JSON
// ({id,name,description,input_schema}, then annotations when set), the value
// [ToolDigest.SHA256] pins. It returns "" when the tool cannot be encoded.
func ModuleToolDigest(tool moduleproto.Tool) string {
	type canon struct {
		ID          string                       `json:"id"`
		Name        string                       `json:"name"`
		Description string                       `json:"description"`
		InputSchema json.RawMessage              `json:"input_schema"`
		Annotations *moduleproto.ToolAnnotations `json:"annotations,omitempty"`
	}
	c := canon{
		ID:          tool.ID,
		Name:        tool.Name,
		Description: tool.Description,
		InputSchema: tool.InputSchema,
		Annotations: tool.Annotations,
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

var errModuleToolUnavailable = errors.New("processmodule: module tool temporarily unavailable")

// ErrModuleToolNotReady is returned when a module tool is called while the
// module has no live Ready child.
var ErrModuleToolNotReady = errors.New("processmodule: module tool not ready")

// ToolRegistrar installs a module's verified tools under their namespaced
// names on the host MCP server.
type ToolRegistrar interface {
	RegisterTools(moduleName string, tools []moduleproto.Tool) error
	UnregisterTools(moduleName string) error
}

// ModuleToolRegistry implements [ToolRegistrar] against the host's
// *mcp.Server and answers the call gate for module tools.
type ModuleToolRegistry struct {
	server *mcp.Server
	sup    *ProcessModuleSupervisor

	mu       sync.Mutex
	byModule map[string]map[string]struct{}
	owner    map[string]string
}

// NewModuleToolRegistry constructs a registry over server and sup. A nil
// server tracks tools without installing them; a nil sup makes every call
// unavailable.
func NewModuleToolRegistry(server *mcp.Server, sup *ProcessModuleSupervisor) *ModuleToolRegistry {
	return &ModuleToolRegistry{
		server:   server,
		sup:      sup,
		byModule: make(map[string]map[string]struct{}),
		owner:    make(map[string]string),
	}
}

// ModuleFor reports which module owns a namespaced tool name.
func (r *ModuleToolRegistry) ModuleFor(toolName string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	mod, ok := r.owner[toolName]
	return mod, ok
}

// GateForModule returns an error, which omits and refuses the tool, unless
// the module is registered and enabled.
func (r *ModuleToolRegistry) GateForModule(moduleName string) error {
	if r.sup == nil {
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

// RegisterTools installs each tool under `module.<name>.<tool>`. A tool
// already installed for this module is left in place; a name owned by
// another module or by a non-module tool is an error.
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
			continue
		}
		if owner, taken := r.owner[ns]; taken && owner != moduleName {
			r.mu.Unlock()
			return fmt.Errorf("processmodule: tool %q collides with module %q", ns, owner)
		}
		toInstall = append(toInstall, pending{namespaced: ns, tool: t})
	}
	r.mu.Unlock()

	for _, p := range toInstall {
		if err := validModuleToolAnnotations(p.tool.Annotations); err != nil {
			return fmt.Errorf("processmodule: tool %q: %w", p.namespaced, err)
		}
	}
	for _, p := range toInstall {
		schema := decodeModuleToolSchema(p.tool.InputSchema)
		h := r.newToolHandler(moduleName, p.tool.ID)
		var opts []mcp.ToolOption
		if a := p.tool.Annotations; a != nil {
			opts = append(opts, mcp.WithAnnotations(mcp.Annotations{
				Title:           a.Title,
				ReadOnlyHint:    a.ReadOnlyHint,
				DestructiveHint: a.DestructiveHint,
				IdempotentHint:  a.IdempotentHint,
				OpenWorldHint:   a.OpenWorldHint,
			}))
		}
		if r.server != nil {
			if err := r.server.RegisterTool(p.namespaced, p.tool.Description, schema, h, opts...); err != nil {
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

// UnregisterTools does nothing: core/mcp cannot remove a tool, and the call
// gate hides a disabled module's tools.
func (r *ModuleToolRegistry) UnregisterTools(moduleName string) error {
	_ = moduleName
	return nil
}

var _ ToolRegistrar = (*ModuleToolRegistry)(nil)

var moduleToolCallID atomic.Uint64

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

const maxModuleToolTitleBytes = 128

func validModuleToolAnnotations(a *moduleproto.ToolAnnotations) error {
	if a == nil {
		return nil
	}
	if len(a.Title) > maxModuleToolTitleBytes {
		return fmt.Errorf("annotations title is longer than %d bytes", maxModuleToolTitleBytes)
	}
	if !utf8.ValidString(a.Title) || textsafe.ContainsUnsafe(a.Title) {
		return errors.New("annotations title holds invalid UTF-8, a control character or an invisible character")
	}
	return nil
}

func decodeModuleToolSchema(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var schema map[string]any
	if err := jsonexact.Unmarshal(raw, &schema); err != nil {
		return map[string]any{}
	}
	return schema
}

func (s *ProcessModuleSupervisor) dispatchToolCall(ctx context.Context, moduleName, toolID string, params map[string]any) (json.RawMessage, error) {
	sl := s.Slot(moduleName)
	if sl == nil {
		return nil, ErrModuleToolNotReady
	}
	snap := sl.snapshot()
	if !servingState(snap.state, snap.leaseFailing) {
		return nil, ErrModuleToolNotReady
	}
	peer := snap.peer
	if peer == nil {
		return nil, ErrModuleToolNotReady
	}

	callID := moduleToolCallID.Add(1)
	mintReq := requestFromToolCtx(withDelegationModule(ctx, moduleName))
	handle, release, err := s.mintDelegationSafely(mintReq, callID)
	if err != nil {
		return nil, err
	}
	defer s.releaseDelegationSafely(release)
	args, err := json.Marshal(params)
	if err != nil {
		args = nil
	}
	deadline := time.Now().Add(sl.callDeadline())
	callCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	raw, err := peer.CallWithID(callCtx, moduleproto.MethodToolCall, moduleproto.ToolCallParams{
		ToolID:         toolID,
		Arguments:      args,
		Caller:         callerFromCtx(ctx, handle),
		DeadlineUnixMs: deadline.UnixMilli(),
	}, func(frameID uint64) {
		go s.watchCancel(peer, frameID, callCtx)
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

func (s *ProcessModuleSupervisor) mintDelegationSafely(r *http.Request, callID uint64) (handle string, release func(), err error) {
	defer func() {
		if p := recover(); p != nil {
			handle, release = "", func() {}
			err = fmt.Errorf("processmodule: broker MintDelegation panic: %v\n%s", textsafe.Recovered(p), debug.Stack())
		}
	}()
	handle, release = s.broker.MintDelegation(r, callID)
	return handle, release, nil
}

func (s *ProcessModuleSupervisor) releaseDelegationSafely(release func()) {
	defer textsafe.LogPanic(context.Background(), "processmodule: delegation release panicked")
	release()
}

func requestFromToolCtx(ctx context.Context) *http.Request {
	r := &http.Request{
		Method: http.MethodPost,
		URL:    &url.URL{Path: "/"},
		Header: map[string][]string{},
	}
	return r.WithContext(ctx)
}

func callerFromCtx(ctx context.Context, handle string) moduleproto.Caller {
	c := moduleproto.Caller{Delegation: handle}
	if u, ok := handler.GetUser(ctx); ok && u != nil {
		c.Subject = fmt.Sprint(u)
	}
	return c
}
