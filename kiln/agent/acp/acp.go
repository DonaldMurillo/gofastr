package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/DonaldMurillo/gofastr/kiln/agent"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
)

// Server speaks the JSON-RPC subset of ACP needed to drive Kiln.
// Use Serve(ctx, in, out) to run a stdio session.
type Server struct {
	tools *protocol.Tools

	mu sync.Mutex
}

// New returns a Server bound to a Tools surface.
func New(tools *protocol.Tools) *Server {
	return &Server{tools: tools}
}

// rpcRequest is one JSON-RPC 2.0 request frame.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// rpcResponse is one JSON-RPC 2.0 response frame.
type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Standard JSON-RPC error codes.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// Serve reads newline-delimited JSON-RPC frames from in and writes
// responses to out. It returns when in EOFs or ctx is canceled.
//
// Newline-delimited JSON is the simplest ACP-compatible framing for v1;
// when ACP standardizes its preferred framing, swap this method.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	enc := json.NewEncoder(out)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.writeResp(enc, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: codeParseError, Message: err.Error()}})
			continue
		}
		resp := s.Handle(ctx, req)
		if resp.JSONRPC == "" {
			resp.JSONRPC = "2.0"
		}
		s.writeResp(enc, resp)
	}
	return scanner.Err()
}

func (s *Server) writeResp(enc *json.Encoder, resp rpcResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = enc.Encode(resp)
}

// Handle routes one JSON-RPC request. Exposed so HTTP-based harnesses
// can drive ACP without going through stdio.
func (s *Server) Handle(ctx context.Context, req rpcRequest) rpcResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	case "prompt":
		return s.handlePrompt(ctx, req)
	case "shutdown":
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}
	}
	return rpcResponse{
		JSONRPC: "2.0", ID: req.ID,
		Error: &rpcError{Code: codeMethodNotFound, Message: "method not found: " + req.Method},
	}
}

// handleInitialize advertises Kiln's ACP server capabilities.
func (s *Server) handleInitialize(req rpcRequest) rpcResponse {
	return rpcResponse{
		JSONRPC: "2.0", ID: req.ID,
		Result: map[string]any{
			"protocol_version": "0.1",
			"server": map[string]any{
				"name":    "kiln",
				"version": "0.1.0",
			},
			"capabilities": map[string]any{
				"tools":  map[string]any{"list": true, "call": true},
				"prompt": map[string]any{"streaming": false},
			},
		},
	}
}

// handleToolsList returns the descriptors of every Kiln tool.
func (s *Server) handleToolsList(req rpcRequest) rpcResponse {
	descs := s.tools.List()
	tools := make([]map[string]any, 0, len(descs))
	for _, d := range descs {
		tools = append(tools, map[string]any{
			"name":         d.Name,
			"description":  d.Description,
			"input_schema": d.Schema,
			"destructive":  d.Destructive,
		})
	}
	return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": tools}}
}

// handleToolsCall dispatches a tool call through the shared dispatcher.
func (s *Server) handleToolsCall(ctx context.Context, req rpcRequest) rpcResponse {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: codeInvalidParams, Message: err.Error()}}
	}
	res := agent.Dispatch(ctx, s.tools, agent.ToolCall{Name: p.Name, Args: p.Arguments})
	return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: res}
}

// handlePrompt is a "user said this" notification from the harness. We
// journal it as a chat message — the actual reasoning happens in the
// harness; Kiln is only the tool surface.
func (s *Server) handlePrompt(ctx context.Context, req rpcRequest) rpcResponse {
	var p struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: codeInvalidParams, Message: err.Error()}}
	}
	if p.Text == "" {
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: codeInvalidParams, Message: "text required"}}
	}
	res := s.tools.Chat(ctx, protocol.ChatArgs{Role: "user", Text: p.Text})
	return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: res}
}

// PromptString builds the system prompt the harness should pass to its
// LLM so the model knows about Kiln tools — useful when the harness
// (e.g. Codex) wants to inline Kiln's instructions instead of relying
// on its own system prompt.
func (s *Server) PromptString() string {
	return agent.BuildPrompt(s.tools.Live().Session(), s.tools.List()).String()
}

// PromptError builds a helpful error to feed back when an unsupported
// method is invoked. Kept exported so harnesses can pre-flight.
func PromptError(method string) error {
	return fmt.Errorf("acp: method %q not supported", method)
}
