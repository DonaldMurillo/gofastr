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
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/auth"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/multiplex"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control/resources"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
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

	// Claims, when set, scopes what this connection may do: which
	// sessions it may drive and which command verbs it may send.
	//
	// The HTTP transport verifies the bearer token and then hands the
	// VERIFIED claims here. It used to verify and discard them, so a
	// token minted for session A drove session B simply by naming B in
	// the tools/call arguments -- the client picks that string. rest.go
	// enforces AllowsSession and AllowsCommand on the same verbs; nil
	// here means an unauthenticated transport (stdio) where the process
	// boundary is the check.
	Claims *auth.Claims

	in  io.Reader
	out io.Writer
	mu  sync.Mutex

	// inflight holds the cancel func of every running request, so a
	// notifications/cancelled naming one can reach it. stdio owns its
	// table; the HTTP transport hands every POST the handler's shared
	// table, because an MCP client sends the cancel as its own POST.
	// owner scopes this connection's entries: a cancel finds only
	// requests with the same owner.
	inflight *inflightTable
	owner    inflightOwner
}

// inflightOwner names who may cancel a request: the bearer token's
// principal (claims.JTI) and the MCP session. Both halves count. The
// client picks Mcp-Session-Id, so the session alone would let one caller
// cancel another's requests.
type inflightOwner struct {
	principal string
	session   string
}

// inflightKey is one running request: its owner and its compacted
// JSON-RPC id.
type inflightKey struct {
	owner inflightOwner
	id    string
}

// inflightTable maps running requests to their cancel funcs and counts
// them per owner, so the maxInFlight cap holds across HTTP POSTs too.
type inflightTable struct {
	mu      sync.Mutex
	cancels map[inflightKey]context.CancelCauseFunc
	count   map[inflightOwner]int
}

func newInflightTable() *inflightTable {
	return &inflightTable{
		cancels: make(map[inflightKey]context.CancelCauseFunc),
		count:   make(map[inflightOwner]int),
	}
}

// errIDInFlight and errTooManyInFlight are track's refusals.
var (
	errIDInFlight      = errors.New("request id already in flight")
	errTooManyInFlight = errors.New("too many requests in flight")
)

// track registers a request's cancel func under owner and id. It refuses
// an id the owner already has running, and a request past the owner's
// maxInFlight.
func (t *inflightTable) track(ctx context.Context, owner inflightOwner, id json.RawMessage) (context.Context, func(), error) {
	key := inflightKey{owner: owner, id: requestKey(id)}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, busy := t.cancels[key]; busy {
		return nil, nil, errIDInFlight
	}
	if t.count[owner] >= maxInFlight {
		return nil, nil, errTooManyInFlight
	}
	reqCtx, cancel := context.WithCancelCause(ctx)
	t.cancels[key] = cancel
	t.count[owner]++
	return reqCtx, func() {
		t.mu.Lock()
		delete(t.cancels, key)
		if t.count[owner]--; t.count[owner] <= 0 {
			delete(t.count, owner)
		}
		t.mu.Unlock()
		cancel(nil)
	}, nil
}

// lookup returns the cancel func of owner's running request id, or nil.
func (t *inflightTable) lookup(owner inflightOwner, id json.RawMessage) context.CancelCauseFunc {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cancels[inflightKey{owner: owner, id: requestKey(id)}]
}

// maxInFlight bounds the requests one connection runs at once, and the
// requests one owner (token principal plus MCP session) holds across
// connections. The read loop waits for a free slot past it, which pushes
// back on the client instead of spawning without limit; a POST that finds
// its owner's share taken by other POSTs is refused.
const maxInFlight = 32

// errClientCancelled is the cancel cause for a request the client
// withdrew with notifications/cancelled.
var errClientCancelled = errors.New("mcpserver: request cancelled by the client")

// New constructs a Server.
func New(mux *multiplex.Mux, catalog *resources.Catalog) *Server {
	return &Server{
		Mux:           mux,
		Catalog:       catalog,
		IdentityClass: control.IdentityAgent,
		inflight:      newInflightTable(),
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

func (s *Server) Serve(ctx context.Context) error {
	if s.RequiredToken != "" {
		// Constant-time: a byte-at-a-time == on a credential leaks how
		// many leading bytes matched through response timing.
		got := os.Getenv("GOFASTR_HARNESS_TOKEN")
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.RequiredToken)) != 1 {
			return errors.New("mcpserver: GOFASTR_HARNESS_TOKEN missing or wrong")
		}
	}
	scanner := bufio.NewScanner(s.in)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	// Each request runs on its own goroutine. Handling them inline let
	// one wait=turn call hold the read loop for the whole turn, so the
	// harness.cancel_turn or notifications/cancelled meant to stop it
	// was never read. Serve returns once every request has answered.
	var wg sync.WaitGroup
	defer wg.Wait()
	slots := make(chan struct{}, maxInFlight)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		req, ok := s.decodeRequest(line)
		if !ok {
			continue
		}
		// Notifications (no ID) run inline: they are quick and carry
		// no response, and a cancel must not wait behind a full slot
		// table.
		if len(req.ID) == 0 {
			s.handleNotification(req)
			continue
		}
		// The slot comes first, so this connection waits for room
		// rather than being refused; the table's own cap then only
		// refuses what other connections of the same owner hold.
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		reqCtx, release, err := s.inflight.track(ctx, s.owner, req.ID)
		if err != nil {
			<-slots
			s.writeErr(req.ID, -32600, err.Error())
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			defer release()
			s.route(reqCtx, req)
		}()
	}
	return scanner.Err()
}

// requestKey canonicalises a JSON-RPC id so `1` and ` 1` name the same
// request.
func requestKey(id json.RawMessage) string {
	var b bytes.Buffer
	if err := json.Compact(&b, id); err != nil {
		return string(id)
	}
	return b.String()
}

// handleNotification runs one ID-less message. notifications/cancelled
// cancels the named in-flight request; everything else
// (notifications/initialized, …) needs no action.
func (s *Server) handleNotification(req rpcRequest) {
	if req.Method != "notifications/cancelled" {
		return
	}
	var params struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if err := unmarshalMCPObject(req.Params, &params); err != nil || len(params.RequestID) == 0 {
		return
	}
	if cancel := s.inflight.lookup(s.owner, params.RequestID); cancel != nil {
		//gofastr:allow(recovercallback) the table holds only cancel funcs inflightTable.track made with context.WithCancelCause; they do not panic and no host code is stored there
		cancel(errClientCancelled)
	}
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

// unmarshalMCPObject decodes a client-supplied JSON object (a JSON-RPC
// params blob or tool arguments) under the no-ambiguity key rule at
// EVERY nesting depth — duplicate and case-folded keys are refused,
// because stdlib json resolves them last-wins and the request must not
// execute under a body any first-read intermediary parsed differently
// — while keeping the MCP tolerance for unknown fields (§
// Unknown-field policy: additive evolution). handler.CheckObjectKeys
// is the walk, the same any-depth rule core/mcp's HandleRequest
// chokepoint runs on req.Params (protocol.go:63-77, pinned by
// TestParamsDuplicateKeysRefused); the value decode itself stays
// plain stdlib.
func unmarshalMCPObject(data []byte, dst any) error {
	if err := handler.CheckObjectKeys(data, handler.FoldJSONKey); err != nil {
		return err
	}
	//gofastr:allow(GOFASTR1407) CheckObjectKeys above refused duplicate and case-folded keys at every depth; the plain decode keeps MCP's unknown-field tolerance
	return json.Unmarshal(data, dst)
}

// decodeRequest parses one line. A line that does not decode is answered
// with an error and ok is false.
func (s *Server) decodeRequest(line []byte) (rpcRequest, bool) {
	var req rpcRequest
	// The envelope is framing, not an evolution surface: every key must
	// exactly match a json tag and no key may repeat (UnmarshalStrict,
	// which also walks every nesting level). Params/args inside it go
	// through unmarshalMCPObject, which keeps the MCP tolerance for
	// unknown fields.
	if err := handler.UnmarshalStrict(line, &req); err != nil {
		// -32602, the code core/mcp's transports answer for a strict
		// decode refusal (transport.go:82-92, :290-299): the bytes are
		// well-formed JSON whose envelope shape this server refuses,
		// which is an invalid-params finding, not a parse error.
		s.write(rpcResponse{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: -32602, Message: "invalid JSON: " + err.Error()},
		})
		return rpcRequest{}, false
	}
	return req, true
}

// route answers one request that carries an id.
func (s *Server) route(ctx context.Context, req rpcRequest) {
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
		s.handleResourcesRead(ctx, req)
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
				"resources": map[string]any{},
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
	// Session-lifecycle verbs (create/attach/detach) are intentionally
	// NOT advertised: the mux only arbitrates per-turn commands and the
	// harness composition layer owns session creation, this server has
	// no path to them, and MCP clients are per-call ephemeral, so there
	// is nothing durable to attach/detach. tools/list must equal what
	// tools/call actually dispatches (see TestMCPToolsListDispatchParity).
	tools := []map[string]any{
		mkTool("harness.list_sessions", "List active and stored harness sessions.",
			`{"type":"object","properties":{}}`),
		mkTool("harness.run_agent_with_shell_access",
			"Run the inner harness agent. The agent has access to Bash, Read, Write, WebFetch tools: allowlisting this is allowlisting LLM-mediated RCE.",
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
	if err := unmarshalMCPObject(req.Params, &params); err != nil {
		s.writeErr(req.ID, -32602, "bad params: "+err.Error())
		return
	}
	switch params.Name {
	case "harness.list_sessions":
		s.write(rpcResponse{ID: req.ID, Result: map[string]any{
			"content": []any{textContent(jsonString(s.listSessions()))},
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
	if err := unmarshalMCPObject(args, &inArgs); err != nil {
		s.writeErr(reqID, -32602, err.Error())
		return
	}
	sess, err := ids.ParseSession(inArgs.SessionID)
	if err != nil {
		s.writeErr(reqID, -32602, err.Error())
		return
	}
	if err := s.authorize("SendInput", sess); err != nil {
		s.writeErr(reqID, -32600, err.Error())
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
		case <-ctx.Done():
			s.abandonTurn(ctx, cli, sess)
			return
		case env, ok := <-sub:
			if !ok {
				if ctx.Err() != nil {
					s.abandonTurn(ctx, cli, sess)
					return
				}
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

// abandonTurn runs when a wait=turn call's context ends before the turn
// does. A call the client withdrew with notifications/cancelled takes its
// turn down with it, the way an interrupted tool call stops its work;
// the MCP spec has the server send no response for it. A context that
// ended for any other reason (the connection closed) leaves the turn
// running, as before.
func (s *Server) abandonTurn(ctx context.Context, cli control.Client, sess ids.SessionID) {
	if !errors.Is(context.Cause(ctx), errClientCancelled) {
		return
	}
	_ = s.Mux.Dispatch(context.WithoutCancel(ctx), cli, control.CancelTurn{SessionID: sess})
}

// dispatchCommand handles simple Command verbs by unmarshalling the
// MCP-shape arguments to the control.Command type, then dispatching.
func (s *Server) dispatchCommand(ctx context.Context, reqID json.RawMessage, args json.RawMessage, verb string) {
	cmd, err := decodeCommandFromMCPArgs(verb, args)
	if err != nil {
		s.writeErr(reqID, -32602, err.Error())
		return
	}
	if err := s.authorize(verb, commandSession(cmd)); err != nil {
		s.writeErr(reqID, -32600, err.Error())
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

// listSessions lists the live sessions the connection's token may
// attach to; a nil Claims (stdio) lists them all.
func (s *Server) listSessions() []resources.SessionInfo {
	if s.Claims == nil {
		return s.Catalog.ListSessions()
	}
	return resources.SessionsAllowed(s.Catalog.ListSessions(), *s.Claims)
}

// authorize applies the connection's token scope to one command. A nil
// Claims means the transport carries no token (stdio), where the process
// boundary is the check.
func (s *Server) authorize(verb string, sess ids.SessionID) error {
	if s.Claims == nil {
		return nil
	}
	if !s.Claims.AllowsCommand(verb) {
		return fmt.Errorf("token is not scoped for %s", verb)
	}
	if sess != "" && !s.Claims.AllowsSession(sess) {
		return fmt.Errorf("token is not scoped for session %s", sess)
	}
	return nil
}

// commandSession pulls the target session out of a decoded command.
// Commands carry it as a field rather than through the Command
// interface, so this is where the type switch lives; a verb with no
// session returns "" and is authorized on its kind alone.
func commandSession(cmd control.Command) ids.SessionID {
	switch v := cmd.(type) {
	case control.SendInput:
		return v.SessionID
	case control.CancelTurn:
		return v.SessionID
	case control.AnswerPermission:
		return v.SessionID
	case control.SetModel:
		return v.SessionID
	case control.EnterPlanMode:
		return v.SessionID
	case control.ExitPlanMode:
		return v.SessionID
	}
	return ""
}

func decodeCommandFromMCPArgs(verb string, args json.RawMessage) (control.Command, error) {
	switch verb {
	case "CancelTurn":
		var v struct {
			SessionID string `json:"sessionId"`
		}
		if err := unmarshalMCPObject(args, &v); err != nil {
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
		if err := unmarshalMCPObject(args, &v); err != nil {
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
		if err := unmarshalMCPObject(args, &v); err != nil {
			return nil, err
		}
		s, _ := ids.ParseSession(v.SessionID)
		return control.SetModel{SessionID: s, Model: v.Model}, nil
	case "EnterPlanMode":
		var v struct {
			SessionID string `json:"sessionId"`
		}
		if err := unmarshalMCPObject(args, &v); err != nil {
			return nil, err
		}
		s, _ := ids.ParseSession(v.SessionID)
		return control.EnterPlanMode{SessionID: s}, nil
	case "ExitPlanMode":
		var v struct {
			SessionID string `json:"sessionId"`
			Approve   bool   `json:"approve"`
		}
		if err := unmarshalMCPObject(args, &v); err != nil {
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

func (s *Server) handleResourcesRead(ctx context.Context, req rpcRequest) {
	var params struct {
		URI string `json:"uri"`
	}
	if err := unmarshalMCPObject(req.Params, &params); err != nil {
		s.writeErr(req.ID, -32602, err.Error())
		return
	}
	if sess, since, ok, err := parseEventsURI(params.URI); ok {
		if err != nil {
			s.writeErr(req.ID, -32602, err.Error())
			return
		}
		s.readEvents(ctx, req, params.URI, sess, since)
		return
	}
	var body any
	switch params.URI {
	case "harness/v1://sessions":
		body = s.listSessions()
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

// maxEventsPerRead caps one read of the events resource. A client
// that gets a full page reads again with since set to the last id.
const maxEventsPerRead = 1000

// parseEventsURI matches harness/v1://session/{id}/events[?since=N].
// ok reports whether the URI names the events resource at all; err is
// set when it does but the id or since is malformed.
func parseEventsURI(uri string) (sess ids.SessionID, since uint64, ok bool, err error) {
	rest, found := strings.CutPrefix(uri, "harness/v1://session/")
	if !found {
		return "", 0, false, nil
	}
	path, query, _ := strings.Cut(rest, "?")
	idPart, found := strings.CutSuffix(path, "/events")
	if !found {
		return "", 0, false, nil
	}
	if sess, err = ids.ParseSession(idPart); err != nil {
		return "", 0, true, fmt.Errorf("invalid session id in %s", uri)
	}
	if query == "" {
		return sess, 0, true, nil
	}
	vals, err := url.ParseQuery(query)
	if err != nil || len(vals) != 1 || len(vals["since"]) != 1 {
		return "", 0, true, fmt.Errorf("events URI takes one since=<id> parameter")
	}
	if since, err = strconv.ParseUint(vals["since"][0], 10, 64); err != nil {
		return "", 0, true, fmt.Errorf("since must be an event id")
	}
	return sess, since, true, nil
}

// readEvents answers a read of a session's events resource with every
// event after since, from the bus window and the session log, in
// order. A range neither holds comes back as a StreamGap.
func (s *Server) readEvents(ctx context.Context, req rpcRequest, uri string, sess ids.SessionID, since uint64) {
	if s.Claims != nil && !s.Claims.AllowsSession(sess) {
		s.writeErr(req.ID, -32602, "token is not scoped for session "+string(sess))
		return
	}
	eng := s.Mux.EngineFor(sess)
	if eng == nil {
		s.writeErr(req.ID, -32602, "unknown session: "+string(sess))
		return
	}
	events := eng.Bus.Backlog(ctx, since, maxEventsPerRead)
	if events == nil {
		events = []control.EventEnvelope{}
	}
	s.write(rpcResponse{ID: req.ID, Result: map[string]any{
		"contents": []map[string]any{
			{"uri": uri, "mimeType": "application/json", "text": jsonString(events)},
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
	// MCP clients read events through the events resource
	// (readEvents), not via Client.Subscribe; return a closed channel.
	ch := make(chan control.EventEnvelope)
	close(ch)
	return ch
}
func (c *mcpClient) Send(_ context.Context, _ control.Command) error { return nil }
func (c *mcpClient) Close() error                                    { return nil }
