package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// PromptMessage is one message of a prompts/get result: the speaker role
// ("user" or "assistant") and a content block. Content reuses the tools/call
// block type (text, image, audio, embedded resource); build it with
// TextContent / ImageContent / AudioContent / ResourceContent.
type PromptMessage struct {
	Role    string  `json:"role"`
	Content Content `json:"content"`
}

// PromptArgument is one argument a prompt accepts, as prompts/list
// advertises it. Required arguments are validated on prompts/get: a request
// missing one is refused with an invalid-params error before the handler
// runs.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// PromptHandler produces a prompt's messages on prompts/get. It runs per
// request, receiving the request context (auth/tenant enriched) and the
// caller-supplied arguments.
type PromptHandler func(ctx context.Context, args map[string]string) ([]PromptMessage, error)

// Prompt is a registered MCP prompt: a named template a client fills with
// arguments and feeds to a model. Prompts are user-selected surfaces (the
// spec imagines them as slash commands), distinct from tools, which the
// model calls.
type Prompt struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`

	handler PromptHandler
	gate    func(ctx context.Context) error
}

// PromptOption customizes a prompt at registration time.
type PromptOption func(*Prompt)

// WithPromptDescription sets a human/agent-readable description, shown in
// prompts/list.
func WithPromptDescription(desc string) PromptOption {
	return func(p *Prompt) { p.Description = desc }
}

// WithPromptArguments declares the arguments the prompt accepts. Each
// argument marked Required is checked on prompts/get: a request omitting it
// is refused with an invalid-params error before the handler runs.
func WithPromptArguments(args ...PromptArgument) PromptOption {
	return func(p *Prompt) { p.Arguments = args }
}

// WithPromptGate attaches a per-caller precondition to a prompt. The gate
// runs on every prompts/get before the handler, and is also evaluated
// during prompts/list so a caller who cannot get the prompt does not see
// it. This is the prompt-side analogue of [WithToolGate] (and
// [WithResourceGate] on the resource side): same predicate on both the
// listing and the get, so a prompt's description and argument list — which
// can name internal concepts the way a tool's inputSchema does — are not
// disclosed to a caller the gate would refuse.
//
// Prefer this over wrapping the handler yourself: battery/auth's
// MCPUser() / MCPRole("admin") are ready-made gates.
func WithPromptGate(gate func(ctx context.Context) error) PromptOption {
	if gate == nil {
		panic("mcp.WithPromptGate: nil gate — a nil precondition would silently allow every caller")
	}
	return func(p *Prompt) { p.gate = gate }
}

// RegisterPrompt adds a prompt to the server. Registering at least one
// prompt makes the server advertise the `prompts` capability in initialize.
// Returns an error on empty name, nil handler, or a duplicate name.
//
// Prompts are NOT covered by the tool call gate (mcp.Gated gates tool
// handlers, not prompts/get). A prompt with sensitive or per-caller content
// should attach [WithPromptGate], which also hides it from prompts/list for
// callers the gate refuses.
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
	defer s.mu.Unlock()
	if s.prompts == nil {
		s.prompts = make(map[string]Prompt)
	}
	if _, exists := s.prompts[name]; exists {
		return fmt.Errorf("mcp: prompt %q already registered", name)
	}
	s.prompts[name] = p
	return nil
}

// hasPrompts reports whether any prompt is registered (drives the prompts
// capability advertisement).
func (s *Server) hasPrompts() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.prompts) > 0
}

// promptsListResult is the result shape for prompts/list. The nextCursor
// key is absent on the final page, and on every page when the whole
// listing fits (the pre-pagination wire shape).
type promptsListResult struct {
	Prompts    []Prompt `json:"prompts"`
	NextCursor string   `json:"nextCursor,omitempty"`
}

// promptsGetParams are the params for a prompts/get request, per the MCP
// spec: a prompt name and an arguments object (string values).
type promptsGetParams struct {
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments,omitempty"`
}

// promptsGetResult is the result shape for prompts/get: an optional
// description and the prompt's messages.
type promptsGetResult struct {
	Description string          `json:"description,omitempty"`
	Messages    []PromptMessage `json:"messages"`
}

// handlePromptsList returns one page of the prompts visible to the
// caller, in name order: a gated prompt (WithPromptGate) is omitted
// rather than listed-and-refused, the same contract tools/list applies
// to WithToolGate. The gate runs BEFORE the page is cut, so pagination
// walks the post-filter set: a gated prompt never surfaces on a page and
// never bends the page sizes or cursor arithmetic that would otherwise
// count it. The name sort keeps pages stable across requests.
func (s *Server) handlePromptsList(ctx context.Context, req Request) Response {
	offset, err := s.listOffset(req, "prompts/list")
	if err != nil {
		return newErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}
	s.mu.RLock()
	list := make([]Prompt, 0, len(s.prompts))
	for _, p := range s.prompts {
		// A prompt the caller cannot get is not listed to them: the
		// description and argument list are the disclosure, same as a
		// tool's inputSchema.
		if p.gate != nil && p.gate(ctx) != nil {
			continue
		}
		list = append(list, p)
	}
	s.mu.RUnlock()
	slices.SortFunc(list, func(a, b Prompt) int { return strings.Compare(a.Name, b.Name) })
	page, next := pageList(s, "prompts/list", list, offset)
	return newSuccessResponse(req.ID, promptsListResult{Prompts: page, NextCursor: next})
}

// handlePromptsGet resolves a prompt by name, validates required arguments,
// and returns its messages. Unknown names and missing required arguments
// are invalid-params errors (the codes the spec assigns them).
func (s *Server) handlePromptsGet(ctx context.Context, req Request) Response {
	if req.Params == nil {
		return newErrorResponse(req.ID, ErrInvalidParams, "missing params")
	}
	var params promptsGetParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return newErrorResponse(req.ID, ErrInvalidParams, "invalid params: "+err.Error())
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

	// The gate runs BEFORE required-argument validation. Validating first
	// answers a refused caller with `missing required argument "x"`, which
	// discloses the argument names of a prompt they cannot access - exactly
	// what prompts/list withholds from them.
	if gErr := s.checkPromptGate(ctx, p); gErr != nil {
		if rpcErr, ok := gErr.(*RPCError); ok {
			return Response{JSONRPC: "2.0", ID: req.ID, Error: rpcErr}
		}
		return newErrorResponse(req.ID, ErrInternalError, gErr.Error())
	}

	// Required-argument validation runs before the handler so a
	// half-filled prompt never renders.
	for _, a := range p.Arguments {
		if a.Required {
			if _, provided := params.Arguments[a.Name]; !provided {
				return newErrorResponse(req.ID, ErrInvalidParams, fmt.Sprintf("missing required argument %q", a.Name))
			}
		}
	}

	messages, err := s.getPromptMessages(ctx, p, params.Arguments)
	if err != nil {
		if rpcErr, ok := err.(*RPCError); ok {
			return Response{JSONRPC: "2.0", ID: req.ID, Error: rpcErr}
		}
		return newErrorResponse(req.ID, ErrInternalError, err.Error())
	}

	return newSuccessResponse(req.ID, promptsGetResult{Description: p.Description, Messages: messages})
}

// getPromptMessages runs a prompt's gate and handler under a recover guard,
// mirroring readResourceContents so a panic in app-supplied code becomes a
// well-formed error instead of unwinding the transport (critical for stdio).
func (s *Server) getPromptMessages(ctx context.Context, p Prompt, args map[string]string) (out []PromptMessage, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			out = nil
			err = &RPCError{Code: ErrInternalError, Message: "internal prompt error"}
		}
	}()
	return p.handler(ctx, args)
}

// checkPromptGate runs a prompt's gate (WithPromptGate) under its own
// recover guard - the same predicate that decided whether the prompt
// appeared in prompts/list. It is separate from getPromptMessages so the
// refusal can happen before argument validation without losing the panic
// protection stdio needs: a panicking gate must become a well-formed error,
// never a transport crash.
func (s *Server) checkPromptGate(ctx context.Context, p Prompt) (err error) {
	if p.gate == nil {
		return nil
	}
	defer func() {
		if rec := recover(); rec != nil {
			err = &RPCError{Code: ErrInternalError, Message: "internal prompt error"}
		}
	}()
	return p.gate(ctx)
}
