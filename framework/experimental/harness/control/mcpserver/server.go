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

// Server answers newline-delimited MCP JSON-RPC on an io.Reader/io.Writer pair, stdin and stdout by default.
type Server struct {
	Mux     *multiplex.Mux
	Catalog *resources.Catalog

	// IdentityClass is the class every command from this connection carries; New sets IdentityAgent.
	IdentityClass control.IdentityClass
	// RequiredToken, when set, must equal $GOFASTR_HARNESS_TOKEN or Serve refuses to start.
	RequiredToken string
	// Claims scope the sessions and commands this connection may use; nil allows all.
	Claims *auth.Claims

	in  io.Reader
	out io.Writer
	mu  sync.Mutex

	inflight *inflightTable
	owner    inflightOwner
}

type inflightOwner struct {
	principal string
	session   string
}

type inflightKey struct {
	owner inflightOwner
	id    string
}

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

var (
	errIDInFlight      = errors.New("request id already in flight")
	errTooManyInFlight = errors.New("too many requests in flight")
)

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

func (t *inflightTable) lookup(owner inflightOwner, id json.RawMessage) context.CancelCauseFunc {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cancels[inflightKey{owner: owner, id: requestKey(id)}]
}

const maxInFlight = 32

var errClientCancelled = errors.New("mcpserver: request cancelled by the client")

// New returns a stdio Server whose clients are agent-class.
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

// WithIO replaces the reader and writer.
func (s *Server) WithIO(in io.Reader, out io.Writer) *Server {
	s.in = in
	s.out = out
	return s
}

// Serve answers requests until the reader ends, each on its own goroutine, and waits for every answer.
func (s *Server) Serve(ctx context.Context) error {
	if s.RequiredToken != "" {
		got := os.Getenv("GOFASTR_HARNESS_TOKEN")
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.RequiredToken)) != 1 {
			return errors.New("mcpserver: GOFASTR_HARNESS_TOKEN missing or wrong")
		}
	}
	scanner := bufio.NewScanner(s.in)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
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
		if len(req.ID) == 0 {
			s.handleNotification(req)
			continue
		}
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

func requestKey(id json.RawMessage) string {
	var b bytes.Buffer
	if err := json.Compact(&b, id); err != nil {
		return string(id)
	}
	return b.String()
}

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

func unmarshalMCPObject(data []byte, dst any) error {
	if err := handler.CheckObjectKeys(data, handler.FoldJSONKey); err != nil {
		return err
	}
	//gofastr:allow(GOFASTR1407) CheckObjectKeys above refused duplicate and case-folded keys at every depth; the plain decode keeps MCP's unknown-field tolerance
	return json.Unmarshal(data, dst)
}

func (s *Server) decodeRequest(line []byte) (rpcRequest, bool) {
	var req rpcRequest
	if err := handler.UnmarshalStrict(line, &req); err != nil {
		s.write(rpcResponse{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: -32602, Message: "invalid JSON: " + err.Error()},
		})
		return rpcRequest{}, false
	}
	return req, true
}

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
	body, err := json.Marshal(resp)
	if err != nil {
		body, _ = json.Marshal(rpcResponse{ //gofastr:allow(discardeddecode) a fixed error under a wire-decoded RawMessage id holds nothing that can fail to encode
			JSONRPC: "2.0", ID: resp.ID,
			Error: &rpcError{Code: -32603, Message: "internal error: the result could not be encoded"},
		})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.out.Write(body)
	_, _ = s.out.Write([]byte{'\n'})
}

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

func (s *Server) handleToolsList(req rpcRequest) {
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
		text, err := jsonString(s.listSessions())
		if err != nil {
			s.writeErr(req.ID, -32603, "internal error: the result could not be encoded")
			return
		}
		s.write(rpcResponse{ID: req.ID, Result: map[string]any{
			"content": []any{textContent(text)},
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

func (s *Server) abandonTurn(ctx context.Context, cli control.Client, sess ids.SessionID) {
	if !errors.Is(context.Cause(ctx), errClientCancelled) {
		return
	}
	_ = s.Mux.Dispatch(context.WithoutCancel(ctx), cli, control.CancelTurn{SessionID: sess})
}

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

func (s *Server) listSessions() []resources.SessionInfo {
	if s.Claims == nil {
		return s.Catalog.ListSessions()
	}
	return resources.SessionsAllowed(s.Catalog.ListSessions(), *s.Claims)
}

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
	var v struct {
		SessionID string `json:"sessionId"`
		CallID    string `json:"callId"`
		Decision  string `json:"decision"`
		Scope     string `json:"scope"`
		Model     string `json:"model"`
		Approve   bool   `json:"approve"`
	}
	if err := unmarshalMCPObject(args, &v); err != nil {
		return nil, err
	}
	sess, err := ids.ParseSession(v.SessionID)
	if err != nil {
		return nil, err
	}
	switch verb {
	case "CancelTurn":
		return control.CancelTurn{SessionID: sess}, nil
	case "AnswerPermission":
		call, err := ids.ParseCall(v.CallID)
		if err != nil {
			return nil, err
		}
		return control.AnswerPermission{
			SessionID: sess, CallID: call,
			Decision: control.Decision(v.Decision),
			Scope:    control.PermitScope(v.Scope),
		}, nil
	case "SetModel":
		return control.SetModel{SessionID: sess, Model: v.Model}, nil
	case "EnterPlanMode":
		return control.EnterPlanMode{SessionID: sess}, nil
	case "ExitPlanMode":
		return control.ExitPlanMode{SessionID: sess, Approve: v.Approve}, nil
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
	case "harness/v1://profiles":
		body = s.Catalog.ListProfiles()
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
	text, err := jsonString(body)
	if err != nil {
		s.writeErr(req.ID, -32603, "internal error: the result could not be encoded")
		return
	}
	s.write(rpcResponse{ID: req.ID, Result: map[string]any{
		"contents": []map[string]any{
			{"uri": params.URI, "mimeType": "application/json", "text": text},
		},
	}})
}

const maxEventsPerRead = 1000

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
	text, err := jsonString(events)
	if err != nil {
		s.writeErr(req.ID, -32603, "internal error: the result could not be encoded")
		return
	}
	s.write(rpcResponse{ID: req.ID, Result: map[string]any{
		"contents": []map[string]any{
			{"uri": uri, "mimeType": "application/json", "text": text},
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

func jsonString(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

type mcpClient struct {
	id    ids.ClientID
	class control.IdentityClass
	subN  atomic.Int64
}

func (c *mcpClient) ID() ids.ClientID                     { return c.id }
func (c *mcpClient) IdentityClass() control.IdentityClass { return c.class }
func (c *mcpClient) Subscribe(_ context.Context) <-chan control.EventEnvelope {
	ch := make(chan control.EventEnvelope)
	close(ch)
	return ch
}
func (c *mcpClient) Send(_ context.Context, _ control.Command) error { return nil }
func (c *mcpClient) Close() error                                    { return nil }
