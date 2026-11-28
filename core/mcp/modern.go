package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
)

// Error codes the 2026-07-28 revision reserves for the protocol.
const (
	// ErrHeaderMismatch answers an HTTP request whose MCP headers are missing, malformed or disagree with its body.
	ErrHeaderMismatch = -32020
	// ErrMissingRequiredClientCapability answers a request that needs a client capability its `_meta` did not declare.
	ErrMissingRequiredClientCapability = -32021
	// ErrUnsupportedProtocolVersion answers a request whose `_meta` names a revision the server does not speak.
	ErrUnsupportedProtocolVersion = -32022
)

// Reserved `_meta` keys of the 2026-07-28 revision.
const (
	MetaProtocolVersion    = "io.modelcontextprotocol/protocolVersion"
	MetaClientInfo         = "io.modelcontextprotocol/clientInfo"
	MetaClientCapabilities = "io.modelcontextprotocol/clientCapabilities"
	MetaServerInfo         = "io.modelcontextprotocol/serverInfo"
)

const maxEchoedVersionBytes = 64

type requestFacts struct {
	modern  bool
	version string
	name    string
	hasName bool
}

func readRequestFacts(req Request) (requestFacts, *RPCError) {
	var facts requestFacts
	if len(bytes.TrimSpace(req.Params)) == 0 {
		return facts, nil
	}
	var p struct {
		Meta json.RawMessage `json:"_meta"`
		Name json.RawMessage `json:"name"`
		URI  json.RawMessage `json:"uri"`
	}
	if json.Unmarshal(req.Params, &p) != nil {
		return facts, nil
	}
	nameField := p.Name
	if req.Method == "resources/read" {
		nameField = p.URI
	}
	if len(nameField) > 0 && json.Unmarshal(nameField, &facts.name) == nil {
		facts.hasName = true
	}
	if len(p.Meta) == 0 || bytes.Equal(bytes.TrimSpace(p.Meta), []byte("null")) {
		return facts, nil
	}
	var meta map[string]json.RawMessage
	if json.Unmarshal(p.Meta, &meta) != nil {
		return facts, nil
	}
	raw, ok := meta[MetaProtocolVersion]
	if !ok {
		return facts, nil
	}
	if json.Unmarshal(raw, &facts.version) != nil {
		return facts, &RPCError{Code: ErrInvalidParams, Message: "invalid params: _meta " + MetaProtocolVersion + " must be a string"}
	}
	facts.modern = true
	return facts, nil
}

func unsupportedVersionError(requested string) *RPCError {
	if len(requested) > maxEchoedVersionBytes {
		requested = requested[:maxEchoedVersionBytes]
	}
	message := "Unsupported protocol version"
	if slices.Contains(handshakeProtocolVersions, requested) {
		message = "Unsupported protocol version: " + requested + " is negotiated with initialize, not declared per request"
	}
	return &RPCError{
		Code:    ErrUnsupportedProtocolVersion,
		Message: message,
		Data:    map[string]any{"supported": append([]string(nil), modernProtocolVersions...), "requested": requested},
	}
}

type modernKey struct{}

func isModern(ctx context.Context) bool {
	v, _ := ctx.Value(modernKey{}).(bool)
	return v
}

var removedInModern = map[string]bool{
	"initialize":            true,
	"ping":                  true,
	"resources/subscribe":   true,
	"resources/unsubscribe": true,
}

var cacheableMethods = map[string]bool{
	"server/discover":          true,
	"tools/list":               true,
	"prompts/list":             true,
	"resources/list":           true,
	"resources/templates/list": true,
	"resources/read":           true,
}

func (s *Server) handleModern(ctx context.Context, req Request, facts requestFacts) Response {
	if facts.modern && !slices.Contains(modernProtocolVersions, facts.version) {
		return Response{JSONRPC: "2.0", ID: req.ID, Error: unsupportedVersionError(facts.version)}
	}
	ctx = context.WithValue(ctx, modernKey{}, true)
	var resp Response
	switch {
	case req.Method == "server/discover":
		resp = s.handleDiscover(ctx, req)
	case req.Method == "subscriptions/listen":
		resp = newErrorResponse(req.ID, ErrMethodNotFound, "subscriptions/listen needs a streaming transport: mount ServeSSE")
	case removedInModern[req.Method]:
		resp = newErrorResponse(req.ID, ErrMethodNotFound, fmt.Sprintf("method %q not found", req.Method))
	default:
		resp = s.dispatch(ctx, req)
	}
	resp = s.decorateModern(req.Method, resp)
	resp.modern = true
	return resp
}

func (s *Server) handleDiscover(ctx context.Context, req Request) Response {
	s.mu.RLock()
	instructions := s.instructions
	s.mu.RUnlock()
	result := map[string]any{
		"supportedVersions": SupportedProtocolVersions(),
		"capabilities":      s.modernCapabilities(ctx),
	}
	if instructions != "" {
		result["instructions"] = instructions
	}
	return newSuccessResponse(req.ID, result)
}

func (s *Server) modernCapabilities(ctx context.Context) map[string]any {
	listen := listenAvailable(ctx)
	feature := func(extra ...string) map[string]any {
		m := map[string]any{}
		if listen {
			for _, k := range extra {
				m[k] = true
			}
		}
		return m
	}
	capabilities := map[string]any{"tools": feature("listChanged")}
	if s.hasResources() || s.hasTemplates() {
		capabilities["resources"] = feature("listChanged", "subscribe")
	}
	if s.hasPrompts() {
		capabilities["prompts"] = feature("listChanged")
	}
	return capabilities
}

func (s *Server) decorateModern(method string, resp Response) Response {
	if resp.Error != nil {
		if method == "resources/read" && resp.Error.Code == ErrResourceNotFound {
			e := *resp.Error
			e.Code = ErrInvalidParams
			resp.Error = &e
		}
		return resp
	}
	b, err := json.Marshal(resp.Result)
	if err != nil {
		return resp
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var result map[string]any
	if dec.Decode(&result) != nil || result == nil {
		result = map[string]any{}
	}
	result["resultType"] = "complete"
	meta, _ := result["_meta"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
	}
	s.mu.RLock()
	meta[MetaServerInfo] = s.serverInfoLocked()
	s.mu.RUnlock()
	result["_meta"] = meta
	if cacheableMethods[method] {
		result["ttlMs"] = 0
		result["cacheScope"] = "private"
	}
	resp.Result = result
	return resp
}
