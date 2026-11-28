package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// PromptMessage is one prompts/get message: a role ("user" or "assistant") and a content block.
type PromptMessage struct {
	Role    string  `json:"role"`
	Content Content `json:"content"`
}

// PromptArgument is an argument a prompt accepts; a Required one missing from prompts/get is refused before the handler runs.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// PromptHandler produces a prompt's messages for the request context and arguments.
type PromptHandler func(ctx context.Context, args map[string]string) ([]PromptMessage, error)

// Prompt is a registered MCP prompt.
type Prompt struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
	Icons       []Icon           `json:"icons,omitempty"`

	handler PromptHandler
	gate    func(ctx context.Context) error
}

// PromptOption customizes a prompt at registration.
type PromptOption func(*Prompt)

// WithPromptDescription sets the description prompts/list shows.
func WithPromptDescription(desc string) PromptOption {
	return func(p *Prompt) { p.Description = desc }
}

// WithPromptArguments declares the arguments the prompt accepts.
func WithPromptArguments(args ...PromptArgument) PromptOption {
	return func(p *Prompt) { p.Arguments = args }
}

// WithPromptGate attaches a per-caller gate that hides the prompt from prompts/list and refuses prompts/get. It panics on a nil gate.
func WithPromptGate(gate func(ctx context.Context) error) PromptOption {
	if gate == nil {
		panic("mcp.WithPromptGate: nil gate — a nil precondition would silently allow every caller")
	}
	return func(p *Prompt) { p.gate = gate }
}

// RegisterPrompt adds a prompt and advertises the prompts capability; it errors on an empty name, a nil handler or a duplicate name.
func (s *Server) RegisterPrompt(name string, handler PromptHandler, opts ...PromptOption) error {
	if name == "" {
		return fmt.Errorf("mcp: prompt name must not be empty")
	}
	if handler == nil {
		return fmt.Errorf("mcp: prompt handler must not be nil")
	}

	p := Prompt{Name: name, handler: handler}
	for _, opt := range opts {
		opt(&p)
	}

	s.mu.Lock()
	if s.prompts == nil {
		s.prompts = make(map[string]Prompt)
	}
	if _, exists := s.prompts[name]; exists {
		s.mu.Unlock()
		return fmt.Errorf("mcp: prompt %q already registered", name)
	}
	s.prompts[name] = p
	s.mu.Unlock()

	s.notifySubscribers(sseNotification{method: "notifications/prompts/list_changed", itemGate: p.gate})
	return nil
}

func (s *Server) hasPrompts() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.prompts) > 0
}

type promptsListResult struct {
	Prompts    []Prompt `json:"prompts"` //gofastr:required-array
	NextCursor string   `json:"nextCursor,omitempty"`
}

type promptsGetParams struct {
	Name      string             `json:"name"`
	Arguments map[string]*string `json:"arguments,omitempty"`
}

type promptsGetResult struct {
	Description string          `json:"description,omitempty"`
	Messages    []PromptMessage `json:"messages"` //gofastr:required-array
}

func (s *Server) handlePromptsList(ctx context.Context, req Request) Response {
	return gatedListResponse(s, ctx, req, "prompts/list",
		func() map[string]Prompt { return s.prompts },
		func(p Prompt) func(context.Context) error { return p.gate },
		func(a, b Prompt) int { return strings.Compare(a.Name, b.Name) },
		func(page []Prompt, next string) any {
			return promptsListResult{Prompts: requiredArray(page), NextCursor: next}
		},
	)
}

func gatedListResponse[T any](
	s *Server,
	ctx context.Context,
	req Request,
	method string,
	registry func() map[string]T,
	gateOf func(T) func(context.Context) error,
	less func(a, b T) int,
	wrap func(page []T, next string) any,
) Response {
	offset, err := s.listOffset(req, method)
	if err != nil {
		return newErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}
	s.mu.RLock()
	all := registry()
	snapshot := make([]T, 0, len(all))
	for _, v := range all {
		snapshot = append(snapshot, v)
	}
	s.mu.RUnlock()
	list := make([]T, 0, len(snapshot))
	for _, v := range snapshot {
		if gateRefused(gateOf(v), ctx) {
			continue
		}
		list = append(list, v)
	}
	slices.SortFunc(list, less)
	page, next := pageList(s, method, list, offset)
	return newSuccessResponse(req.ID, wrap(page, next))
}

func (s *Server) handlePromptsGet(ctx context.Context, req Request) Response {
	if req.Params == nil {
		return newErrorResponse(req.ID, ErrInvalidParams, "missing params")
	}
	var params promptsGetParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return newErrorResponse(req.ID, ErrInvalidParams, "invalid params: "+handler.JSONErrorDetail(err).Error())
	}
	if params.Name == "" {
		return newErrorResponse(req.ID, ErrInvalidParams, "missing prompt name")
	}

	s.mu.RLock()
	p, ok := s.prompts[params.Name]
	s.mu.RUnlock()
	if !ok {
		return newErrorResponse(req.ID, ErrInvalidParams, fmt.Sprintf("prompt %q not found", params.Name))
	}

	if gErr := s.checkPromptGate(ctx, p); gErr != nil {
		return Response{JSONRPC: "2.0", ID: req.ID, Error: maskedRefusalRPC(gErr, &RPCError{
			Code: ErrInvalidParams, Message: fmt.Sprintf("prompt %q not found", params.Name),
		})}
	}

	args := make(map[string]string, len(params.Arguments))
	for _, name := range slices.Sorted(maps.Keys(params.Arguments)) {
		v := params.Arguments[name]
		if v == nil {
			return newErrorResponse(req.ID, ErrInvalidParams, fmt.Sprintf("argument %q must be a string, not null", name))
		}
		args[name] = *v
	}

	for _, a := range p.Arguments {
		if a.Required {
			if _, provided := args[a.Name]; !provided {
				return newErrorResponse(req.ID, ErrInvalidParams, fmt.Sprintf("missing required argument %q", a.Name))
			}
		}
	}

	messages, err := s.getPromptMessages(ctx, p, args)
	if err != nil {
		return handlerErrorResponse(ctx, req, err, "mcp: prompt handler failed", "internal prompt error",
			slog.String("prompt", p.Name))
	}
	if messages == nil {
		messages = []PromptMessage{}
	}
	return newSuccessResponse(req.ID, promptsGetResult{Description: p.Description, Messages: messages})
}

func (s *Server) getPromptMessages(ctx context.Context, p Prompt, args map[string]string) (out []PromptMessage, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("panic in MCP prompt handler recovered", "operation", "prompt handler",
				"prompt", textsafe.SanitizeControlBytes(p.Name), "panic", textsafe.Recovered(rec), "stack", string(debug.Stack()))
			out = nil
			err = &RPCError{Code: ErrInternalError, Message: "internal prompt error"}
		}
	}()
	return p.handler(ctx, args)
}

func handlerErrorResponse(ctx context.Context, req Request, err error, logMsg, genericMsg string, attrs ...slog.Attr) Response {
	if rpcErr, ok := callerFacingError(ctx, err); ok {
		return Response{JSONRPC: "2.0", ID: req.ID, Error: rpcErr}
	}
	attrs = append(attrs, slog.String("err", err.Error()))
	slog.Default().LogAttrs(context.Background(), slog.LevelError, logMsg, attrs...)
	return newErrorResponse(req.ID, ErrInternalError, genericMsg)
}

func (s *Server) checkPromptGate(ctx context.Context, p Prompt) (err error) {
	if p.gate == nil {
		return nil
	}
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("panic in MCP prompt gate recovered", "operation", "prompt gate",
				"prompt", textsafe.SanitizeControlBytes(p.Name), "panic", textsafe.Recovered(rec), "stack", string(debug.Stack()))
			err = &RPCError{Code: ErrInternalError, Message: "internal prompt error"}
		}
	}()
	return p.gate(ctx)
}
