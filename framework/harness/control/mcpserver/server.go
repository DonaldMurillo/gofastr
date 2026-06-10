// Package mcpserver exposes the harness engine as an MCP server.
//
// stdio variant lands here (this file); streamable HTTP lives in
// http.go. Per § MCP-server surface, every Command verb is an MCP
// tool, sessions/profiles/providers/tools/skills are resources,
// every loaded skill becomes an MCP prompt, and the
// agent-driving-agent tool is named honestly:
// `harness.run_agent_with_shell_access`.
package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/multiplex"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/resources"
	"github.com/DonaldMurillo/gofastr/framework/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

// Server runs the MCP server protocol against an io.Reader / io.Writer
// pair (stdin/stdout for the stdio variant).
type Server struct {
	Mux     *multiplex.Mux
	Catalog *resources.Catalog

	// IdentityClass is the class assigned to the connecting MCP
	// client. v0.1 uses IdentityAgent for every MCP attach per the
	// architecture doc.
	IdentityClass control.IdentityClass

	// Optional token check: when set, the env var
	// GOFASTR_HARNESS_TOKEN must equal RequiredToken for the
	// connection to be accepted.
	RequiredToken string

	in  io.Reader
	out io.Writer
	mu  sync.Mutex
}

// New constructs a Server.
func New(mux *multiplex.Mux, catalog *resources.Catalog) *Server {
	return &Server{
		Mux:           mux,
		Catalog:       catalog,
		IdentityClass: control.IdentityAgent,
		in:            os.Stdin,
		out:           os.Stdout,
	}
}

// WithIO overrides the reader/writer (used by HTTP wrapper + tests).
func (s *Server) WithIO(in io.Reader, out io.Writer) *Server {
	s.in = in
	s.out = out
	return s
}

// Serve runs until the input closes.
func (s *Server) Serve(ctx context.Context) error {
	if s.RequiredToken != "" {
		if os.Getenv("GOFASTR_HARNESS_TOKEN") != s.RequiredToken {
			return errors.New("mcpserver: GOFASTR_HARNESS_TOKEN missing or wrong")
		}
	}
	scanner := bufio.NewScanner(s.in)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		s.handle(ctx, line)
	}
	return scanner.Err()
}

// ---------- JSON-RPC plumbing ----------

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) handle(ctx context.Context, line []byte) {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		s.write(rpcResponse{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: -32700, Message: "parse error"},
		})
		return
	}
	// Notifications (no ID) — handle "notifications/initialized" silently.
	if len(req.ID) == 0 {
		return
	}
	switch req.Method {
	case "initialize":
		s.handleInitialize(req)
	case "tools/list":
		s.handleToolsList(req)
	case "tools/call":
		s.handleToolsCall(ctx, req)
	case "resources/list":
		s.handleResourcesList(req)
	case "resources/read":
		s.handleResourcesRead(req)
	case "prompts/list":
		s.handlePromptsList(req)
	default:
		s.write(rpcResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &rpcError{Code: -32601, Message: "method not found: " + req.Method},
		})
	}
}

func (s *Server) write(resp rpcResponse) {
	resp.JSONRPC = "2.0"
	s.mu.Lock()
	defer s.mu.Unlock()
	body, _ := json.Marshal(resp)
	_, _ = s.out.Write(body)
	_, _ = s.out.Write([]byte{'\n'})
}

// ---------- Method handlers ----------

func (s *Server) handleInitialize(req rpcRequest) {
	s.write(rpcResponse{
		ID: req.ID,
		Result: map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools":     map[string]any{},
				"resources": map[string]any{"subscribe": true},
				"prompts":   map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "gofastr-harness",
				"version": "0.1.0",
			},
		},
	})
}

// tools/list returns the Command-verb tools the harness exposes.
func (s *Server) handleToolsList(req rpcRequest) {
	tools := []map[string]any{
		mkTool("harness.create_session", "Create a new harness session.",
			`{"type":"object","properties":{"profile":{"type":"string"}},"required":["profile"]}`),
		mkTool("harness.list_sessions", "List active and stored harness sessions.",
			`{"type":"object","properties":{}}`),
		mkTool("harness.attach_session", "Attach to an existing session by ID.",
			`{"type":"object","properties":{"sessionId":{"type":"string"}},"required":["sessionId"]}`),
		mkTool("harness.detach_session", "Detach (non-destructive).",
			`{"type":"object","properties":{"sessionId":{"type":"string"}},"required":["sessionId"]}`),
		mkTool("harness.run_agent_with_shell_access",
			"Run the inner harness agent. The agent has access to Bash, Read, Write, WebFetch tools — allowlisting this is allowlisting LLM-mediated RCE.",
			`{"type":"object","properties":{"sessionId":{"type":"string"},"prompt":{"type":"string"},"wait":{"type":"string","enum":["turn","none"]}},"required":["sessionId","prompt"]}`),
		mkTool("harness.cancel_turn", "Cancel the in-flight turn.",
			`{"type":"object","properties":{"sessionId":{"type":"string"}},"required":["sessionId"]}`),
		mkTool("harness.answer_permission", "Answer a permission prompt.",
			`{"type":"object","properties":{"sessionId":{"type":"string"},"callId":{"type":"string"},"decision":{"type":"string","enum":["allow","deny"]},"scope":{"type":"string"}},"required":["sessionId","callId","decision"]}`),
		mkTool("harness.set_model", "Set the active model for the session.",
			`{"type":"object","properties":{"sessionId":{"type":"string"},"model":{"type":"string"}},"required":["sessionId","model"]}`),
		mkTool("harness.enter_plan_mode", "Enter plan mode (read-only).",
			`{"type":"object","properties":{"sessionId":{"type":"string"}},"required":["sessionId"]}`),
		mkTool("harness.exit_plan_mode", "Exit plan mode.",
			`{"type":"object","properties":{"sessionId":{"type":"string"},"approve":{"type":"boolean"}},"required":["sessionId"]}`),
	}
	s.write(rpcResponse{ID: req.ID, Result: map[string]any{"tools": tools}})
}

func mkTool(name, desc, schema string) map[string]any {
	return map[string]any{
		"name":        name,
		"description": desc,
		"inputSchema": json.RawMessage(schema),
	}
}

// tools/call dispatches to the right Command verb.
func (s *Server) handleToolsCall(ctx context.Context, req rpcRequest) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeErr(req.ID, -32602, "bad params: "+err.Error())
		return
	}
	switch params.Name {
	case "harness.list_sessions":
		s.write(rpcResponse{ID: req.ID, Result: map[string]any{
			"content": []any{textContent(jsonString(s.Catalog.ListSessions()))},
		}})
	case "harness.run_agent_with_shell_access":
		s.runAgentWithShellAccess(ctx, req.ID, params.Arguments)
	case "harness.cancel_turn":
		s.dispatchCommand(ctx, req.ID, params.Arguments, "CancelTurn")
	case "harness.answer_permission":
		s.dispatchCommand(ctx, req.ID, params.Arguments, "AnswerPermission")
	case "harness.set_model":
		s.dispatchCommand(ctx, req.ID, params.Arguments, "SetModel")
	case "harness.enter_plan_mode":
		s.dispatchCommand(ctx, req.ID, params.Arguments, "EnterPlanMode")
	case "harness.exit_plan_mode":
		s.dispatchCommand(ctx, req.ID, params.Arguments, "ExitPlanMode")
	default:
		s.writeErr(req.ID, -32601, "unknown tool: "+params.Name)
	}
}

// runAgentWithShellAccess is the high-value tool. It dispatches a
// SendInput and (when wait=turn) blocks until TurnEnded, returning
// the final assistant text + cost summary.
func (s *Server) runAgentWithShellAccess(ctx context.Context, reqID json.RawMessage, args json.RawMessage) {
	var inArgs struct {
		SessionID string `json:"sessionId"`
		Prompt    string `json:"prompt"`
		Wait      string `json:"wait"`
	}
	if err := json.Unmarshal(args, &inArgs); err != nil {
		s.writeErr(reqID, -32602, err.Error())
		return
	}
	sess, err := ids.ParseSession(inArgs.SessionID)
	if err != nil {
		s.writeErr(reqID, -32602, err.Error())
		return
	}
	eng := s.Mux.EngineFor(sess)
	if eng == nil {
		s.writeErr(reqID, -32602, "unknown session "+inArgs.SessionID)
		return
	}
	cli := &mcpClient{id: ids.NewClientID(), class: s.IdentityClass}
	if err := s.Mux.Attach(sess, cli); err != nil {
		s.writeErr(reqID, -32603, err.Error())
		return
	}
	defer s.Mux.Detach(sess, cli.ID())

	// Subscribe before dispatching to catch the streaming events.
	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	sub := eng.Bus.Subscribe(subCtx)

	if err := s.Mux.Dispatch(ctx, cli, control.SendInput{
		SessionID: sess,
		Content:   engine.SimpleInput(inArgs.Prompt),
	}); err != nil {
		s.writeErr(reqID, -32603, err.Error())
		return
	}

	if inArgs.Wait != "turn" {
		s.write(rpcResponse{ID: reqID, Result: map[string]any{
			"content": []any{textContent("Dispatched (wait=none). Subscribe to harness/v1://session/" + inArgs.SessionID + "/events for the stream.")},
		}})
		return
	}

	// Collect text until TurnEnded.
	var text strings.Builder
	var cost float64
	var toolCalls []string
	turns := 0
	deadline := time.After(5 * time.Minute)
	for {
		select {
		case <-deadline:
			s.writeErr(reqID, -32603, "turn timed out after 5 minutes")
			return
		case env, ok := <-sub:
			if !ok {
				goto done
			}
			ev, _ := control.DecodeEvent(env)
			switch v := ev.(type) {
			case control.TextDelta:
				text.WriteString(v.Text)
			case control.ToolCallStarted:
				toolCalls = append(toolCalls, v.Tool)
			case control.CostIncremented:
				cost += v.USD
			case control.TurnEnded:
				turns++
				goto done
			case control.Error:
				s.writeErr(reqID, -32603, v.Message)
				return
			}
		}
	}
done:
	s.write(rpcResponse{ID: reqID, Result: map[string]any{
		"content": []any{textContent(text.String())},
		"_meta": map[string]any{
			"cost":      cost,
			"turns":     turns,
			"toolCalls": toolCalls,
		},
	}})
}

// dispatchCommand handles simple Command verbs by unmarshalling the
// MCP-shape arguments to the control.Command type, then dispatching.
func (s *Server) dispatchCommand(ctx context.Context, reqID json.RawMessage, args json.RawMessage, verb string) {
	cmd, err := decodeCommandFromMCPArgs(verb, args)
	if err != nil {
		s.writeErr(reqID, -32602, err.Error())
		return
	}
	cli := &mcpClient{id: ids.NewClientID(), class: s.IdentityClass}
	if err := s.Mux.Dispatch(ctx, cli, cmd); err != nil {
		s.writeErr(reqID, -32603, err.Error())
		return
	}
	s.write(rpcResponse{ID: reqID, Result: map[string]any{
		"content": []any{textContent("ok")},
	}})
}

func decodeCommandFromMCPArgs(verb string, args json.RawMessage) (control.Command, error) {
	switch verb {
	case "CancelTurn":
		var v struct {
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal(args, &v); err != nil {
			return nil, err
		}
		s, err := ids.ParseSession(v.SessionID)
		if err != nil {
			return nil, err
		}
		return control.CancelTurn{SessionID: s}, nil
	case "AnswerPermission":
		var v struct {
			SessionID string `json:"sessionId"`
			CallID    string `json:"callId"`
			Decision  string `json:"decision"`
			Scope     string `json:"scope"`
		}
		if err := json.Unmarshal(args, &v); err != nil {
			return nil, err
		}
		s, _ := ids.ParseSession(v.SessionID)
		c, _ := ids.ParseCall(v.CallID)
		return control.AnswerPermission{
			SessionID: s, CallID: c,
			Decision: control.Decision(v.Decision),
			Scope:    control.PermitScope(v.Scope),
		}, nil
	case "SetModel":
		var v struct {
			SessionID string `json:"sessionId"`
			Model     string `json:"model"`
		}
		if err := json.Unmarshal(args, &v); err != nil {
			return nil, err
		}
		s, _ := ids.ParseSession(v.SessionID)
		return control.SetModel{SessionID: s, Model: v.Model}, nil
	case "EnterPlanMode":
		var v struct {
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal(args, &v); err != nil {
			return nil, err
		}
		s, _ := ids.ParseSession(v.SessionID)
		return control.EnterPlanMode{SessionID: s}, nil
	case "ExitPlanMode":
		var v struct {
			SessionID string `json:"sessionId"`
			Approve   bool   `json:"approve"`
		}
		if err := json.Unmarshal(args, &v); err != nil {
			return nil, err
		}
		s, _ := ids.ParseSession(v.SessionID)
		return control.ExitPlanMode{SessionID: s, Approve: v.Approve}, nil
	}
	return nil, fmt.Errorf("unknown verb %q", verb)
}

func (s *Server) handleResourcesList(req rpcRequest) {
	s.write(rpcResponse{ID: req.ID, Result: map[string]any{
		"resources": []map[string]any{
			{"uri": "harness/v1://sessions", "name": "Sessions"},
			{"uri": "harness/v1://profiles", "name": "Profiles"},
			{"uri": "harness/v1://providers", "name": "Providers"},
			{"uri": "harness/v1://tools", "name": "Tools"},
			{"uri": "harness/v1://skills", "name": "Skills"},
		},
	}})
}

func (s *Server) handleResourcesRead(req rpcRequest) {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeErr(req.ID, -32602, err.Error())
		return
	}
	var body any
	switch params.URI {
	case "harness/v1://sessions":
		body = s.Catalog.ListSessions()
	case "harness/v1://providers":
		body = s.Catalog.ListProviders(context.Background())
	case "harness/v1://tools":
		body = s.Catalog.ListTools()
	case "harness/v1://skills":
		body = s.Catalog.ListSkills()
	default:
		s.writeErr(req.ID, -32602, "unknown URI: "+params.URI)
		return
	}
	s.write(rpcResponse{ID: req.ID, Result: map[string]any{
		"contents": []map[string]any{
			{"uri": params.URI, "mimeType": "application/json", "text": jsonString(body)},
		},
	}})
}

func (s *Server) handlePromptsList(req rpcRequest) {
	skills := s.Catalog.ListSkills()
	out := make([]map[string]any, 0, len(skills))
	for _, sk := range skills {
		out = append(out, map[string]any{
			"name":        sk.Name,
			"description": sk.Description,
		})
	}
	s.write(rpcResponse{ID: req.ID, Result: map[string]any{"prompts": out}})
}

func (s *Server) writeErr(id json.RawMessage, code int, msg string) {
	s.write(rpcResponse{
		ID:    id,
		Error: &rpcError{Code: code, Message: msg},
	})
}

func textContent(text string) map[string]any {
	return map[string]any{"type": "text", "text": text}
}

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// ---------- control.Client implementation for MCP-attached clients ----------

type mcpClient struct {
	id    ids.ClientID
	class control.IdentityClass
	subN  atomic.Int64
}

func (c *mcpClient) ID() ids.ClientID                     { return c.id }
func (c *mcpClient) IdentityClass() control.IdentityClass { return c.class }
func (c *mcpClient) Subscribe(_ context.Context) <-chan control.EventEnvelope {
	// MCP clients consume via the resource subscription, not via
	// Client.Subscribe; return a closed channel.
	ch := make(chan control.EventEnvelope)
	close(ch)
	return ch
}
func (c *mcpClient) Send(_ context.Context, _ control.Command) error { return nil }
func (c *mcpClient) Close() error                                    { return nil }
