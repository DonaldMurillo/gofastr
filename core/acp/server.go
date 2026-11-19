package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// ErrSessionNotFound reports a session/load (or session/prompt) for a session
// ID this agent does not know.
var ErrSessionNotFound = errors.New("acp: session not found")

// Agent is the embedder-supplied brain of the ACP server. Methods may be
// called concurrently; the server serializes prompt turns per session.
type Agent interface {
	// Info identifies the agent in the initialize response.
	Info() Implementation

	// NewSession creates one conversation in the absolute directory cwd; an
	// empty Session ID fails session/new with an internal error.
	NewSession(ctx context.Context, cwd string) (Session, error)
}

// Session is one ACP conversation.
type Session interface {
	// ID is the wire sessionId, stable for the session and unique per agent.
	ID() string

	// Prompt runs one prompt turn, streaming through out; it must return
	// promptly once session/cancel cancels ctx.
	Prompt(ctx context.Context, prompt []ContentBlock, out *Client) (string, error)
}

// SessionLoader is the optional second interface an Agent may implement to
// support session/load.
type SessionLoader interface {
	// LoadSession restores a session previously created on this agent and
	// replays its conversation as user/agent message chunks through out
	// BEFORE returning; an unknown ID returns ErrSessionNotFound.
	LoadSession(ctx context.Context, sessionID, cwd string, out *Client) (Session, error)
}

// Options configures a Server. The zero value is usable.
type Options struct {
	// AuthMethods are advertised at initialize; when non-empty, session
	// methods answer ErrAuthRequired until authenticate succeeds.
	AuthMethods []AuthMethod

	// Authenticate runs one protocol-driven authentication attempt for a
	// methodId the client picked from AuthMethods.
	Authenticate func(ctx context.Context, methodID string) error

	// MaxSessions caps the live sessions ONE connection may hold; session/new
	// and session/load past the cap follow SessionOverflow. 0 means 1024;
	// negative lifts the cap.
	MaxSessions int

	// SessionOverflow selects what a connection at MaxSessions does with its
	// next session.
	SessionOverflow SessionOverflowPolicy
}

// Server speaks ACP v1 over newline-delimited JSON-RPC 2.0 for one
// connection.
type Server struct {
	agent  Agent
	opts   Options
	loader SessionLoader
}

// NewServer binds an Agent to a new Server.
func NewServer(agent Agent, opts *Options) *Server {
	o := Options{}
	if opts != nil {
		o = *opts
	}
	var loader SessionLoader
	if agent != nil {
		if l, ok := agent.(SessionLoader); ok {
			loader = l
		}
	}
	return &Server{agent: agent, opts: o, loader: loader}
}

type wireRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *wireRespError  `json:"error,omitempty"`
}

type wireRespError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type wireResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *wireRespError  `json:"error,omitempty"`
}

type updateParams struct {
	SessionID string `json:"sessionId"`
	Update    Update `json:"update"`
}

type permissionParams struct {
	SessionID string             `json:"sessionId"`
	ToolCall  ToolCallUpdate     `json:"toolCall"`
	Options   []PermissionOption `json:"options"`
}

type pendingResp struct {
	Result json.RawMessage
	Error  *wireRespError
}

// Client is the embedder's handle for talking to the connected ACP client
// during a prompt turn or a session replay: it writes session/update
// notifications and issues session/request_permission calls.
type Client struct {
	conn      *conn
	sessionID string
}

// SessionID is the session this Client speaks for.
func (c *Client) SessionID() string { return c.sessionID }

// Update writes one session/update notification.
func (c *Client) Update(u Update) error {
	return c.conn.writeNotification("session/update", updateParams{
		SessionID: c.sessionID,
		Update:    u,
	})
}

// RequestPermission asks the user to approve a tool call and blocks until the
// client answers, ctx ends, or the connection dies. A malformed answer
// reads as OutcomeCancelled.
func (c *Client) RequestPermission(ctx context.Context, toolCall ToolCallUpdate, options []PermissionOption) (RequestPermissionOutcome, error) {
	var zero RequestPermissionOutcome
	id := c.conn.nextID.Add(1)
	ch := make(chan pendingResp, 1)
	c.conn.pmu.Lock()
	c.conn.pending[id] = ch
	c.conn.pmu.Unlock()
	defer func() {
		c.conn.pmu.Lock()
		delete(c.conn.pending, id)
		c.conn.pmu.Unlock()
	}()
	rawID, err := json.Marshal(id)
	if err != nil {
		return zero, err
	}
	if err := c.conn.writeFrameCtx(ctx, struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  any             `json:"params"`
	}{"2.0", rawID, "session/request_permission", permissionParams{
		SessionID: c.sessionID,
		ToolCall:  toolCall,
		Options:   options,
	}}); err != nil {
		return zero, err
	}
	select {
	case resp := <-ch:
		if resp.Error != nil {
			return zero, fmt.Errorf("acp: session/request_permission error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		var out struct {
			Outcome RequestPermissionOutcome `json:"outcome"`
		}
		if len(resp.Result) > 0 {
			if err := handler.UnmarshalStrict(resp.Result, &out); err != nil {
				return zero, fmt.Errorf("acp: session/request_permission: decode outcome: %w", err)
			}
		}
		if out.Outcome.Outcome != OutcomeSelected && out.Outcome.Outcome != OutcomeCancelled {
			out.Outcome = RequestPermissionOutcome{Outcome: OutcomeCancelled}
		}
		if out.Outcome.Outcome == OutcomeSelected && out.Outcome.OptionID == "" {
			out.Outcome = RequestPermissionOutcome{Outcome: OutcomeCancelled}
		}
		return out.Outcome, nil
	case <-ctx.Done():
		return zero, ctx.Err()
	case <-c.conn.closed:
		return zero, errConnClosed
	}
}

type conn struct {
	wslot     chan struct{}
	w         io.Writer
	werr      error // set once a frame is cut short; guarded by wslot
	pmu       sync.Mutex
	pending   map[int64]chan pendingResp
	nextID    atomic.Int64
	closeOnce sync.Once
	closed    chan struct{}
}

func newConn(w io.Writer) *conn {
	return &conn{wslot: make(chan struct{}, 1), w: w, pending: map[int64]chan pendingResp{}, closed: make(chan struct{})}
}

var errConnClosed = errors.New("acp: connection closed")

var errWriterPanicked = errors.New("acp: output writer panicked; the stream is broken")

func (c *conn) writeFrame(v any) error {
	buf, err := json.Marshal(v)
	if err != nil {
		return err
	}
	select {
	case c.wslot <- struct{}{}:
	case <-c.closed:
		return errConnClosed
	}
	defer func() { <-c.wslot }()
	return c.writeHeld(buf)
}

func (c *conn) writeFrameCtx(ctx context.Context, v any) error {
	buf, err := json.Marshal(v)
	if err != nil {
		return err
	}
	select {
	case c.wslot <- struct{}{}:
	case <-c.closed:
		return errConnClosed
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		<-c.wslot
		return err
	}
	done := make(chan error, 1)
	go func() {
		defer func() { <-c.wslot }()
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic in ACP writer recovered", "operation", "write",
					"panic", textsafe.Recovered(rec), "stack", string(debug.Stack()))
				c.werr = errWriterPanicked
				done <- errWriterPanicked
			}
		}()
		done <- c.writeHeld(buf)
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return errConnClosed
	}
}

func (c *conn) writeHeld(buf []byte) error {
	select {
	case <-c.closed:
		return errConnClosed
	default:
	}
	if c.werr != nil {
		return c.werr
	}
	n, err := c.w.Write(append(buf, '\n'))
	if err == nil && n != len(buf)+1 {
		err = io.ErrShortWrite
	}
	if err != nil && n > 0 {
		c.werr = fmt.Errorf("acp: stream broken by a partial frame: %w", err)
	}
	return err
}

func (c *conn) writeResponse(resp wireResponse) error {
	return c.writeFrame(resp)
}

func (c *conn) writeNotification(method string, params any) error {
	return c.writeFrame(struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params"`
	}{"2.0", method, params})
}

func (c *conn) deliver(id int64, resp pendingResp) bool {
	c.pmu.Lock()
	ch, ok := c.pending[id]
	c.pmu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- resp:
	default:
	}
	return true
}

func (c *conn) close() {
	c.closeOnce.Do(func() { close(c.closed) })
}

type session struct {
	impl Session

	mu         sync.Mutex
	busy       bool
	cancel     context.CancelFunc
	responding chan struct{}
}

type serverState struct {
	srv           *Server
	conn          *conn
	mu            sync.Mutex
	sessions      map[string]*session
	sessionOrder  []string
	draining      map[string]*session
	ready         bool
	authenticated bool
}

// Serve reads newline-delimited JSON-RPC 2.0 frames from in and writes
// responses, notifications, and server-to-client requests to out. It
// returns nil on EOF of in and ctx.Err() if ctx is canceled first.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	st := &serverState{
		srv:      s,
		conn:     newConn(out),
		sessions: map[string]*session{},
		draining: map[string]*session{},
	}
	defer st.conn.close()
	r := &reader{}
	r.lines, r.readErr = readLines(ctx, in)
	for {
		var line []byte
		switch {
		case r.ended:
			return r.endErr
		case len(r.held) > 0:
			line, r.held = r.held[0], r.held[1:]
			r.heldBytes -= len(line)
		default:
			select {
			case <-ctx.Done():
				return ctx.Err()
			case err := <-r.readErr:
				return err
			case line = <-r.lines:
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(line) == 0 {
			continue
		}
		var frame wireRequest
		if err := decodeEnvelope(line, &frame); err != nil {
			st.respond(frame.ID, wireResponse{
				JSONRPC: "2.0",
				Error:   &wireRespError{Code: ErrParseError, Message: handler.JSONErrorDetail(err).Error()},
			})
			continue
		}
		if frame.Method != "" && !validRequestEnvelope(frame) {
			st.respond(json.RawMessage("null"), wireResponse{
				JSONRPC: "2.0",
				Error:   &wireRespError{Code: ErrInvalidRequest, Message: "invalid request: not a JSON-RPC 2.0 request object"},
			})
			continue
		}
		if frame.Method == "" {
			st.deliverAnswer(frame)
			continue
		}
		if frame.ID == nil {
			st.handleNotification(frame) // notifications never get a response
			continue
		}
		if frame.Method == "session/prompt" {
			st.startPrompt(ctx, frame) // responds asynchronously
			continue
		}
		st.respond(frame.ID, st.runRequest(ctx, frame, r))
	}
}

type reader struct {
	lines     <-chan []byte
	readErr   <-chan error
	held      [][]byte
	heldBytes int
	ended     bool
	endErr    error
}

func (st *serverState) runRequest(ctx context.Context, frame wireRequest, r *reader) wireResponse {
	hctx, hcancel := context.WithCancel(ctx)
	defer hcancel()
	done := make(chan wireResponse, 1)
	go func() { done <- st.handleRequest(hctx, frame) }()
	for {
		var lines <-chan []byte
		var readErr <-chan error
		var stop <-chan struct{}
		if !r.ended && ctx.Err() == nil {
			lines, readErr, stop = r.lines, r.readErr, ctx.Done()
		}
		select {
		case resp := <-done:
			return resp
		case <-stop:
		case err := <-readErr:
			r.ended, r.endErr = true, err
			hcancel()
			st.conn.close()
		case line := <-lines:
			st.holdLine(line, r)
		}
	}
}

const (
	maxHeldFrames = 256
	maxHeldBytes  = 16 << 20
)

func (st *serverState) holdLine(line []byte, r *reader) {
	var f wireRequest
	parsed := decodeEnvelope(line, &f) == nil
	if parsed && f.Method == "" {
		st.deliverAnswer(f)
		return
	}
	if parsed && f.ID == nil && f.Method == "session/cancel" && validRequestEnvelope(f) {
		st.handleNotification(f)
	}
	if len(r.held) >= maxHeldFrames || r.heldBytes+len(line) > maxHeldBytes {
		if parsed && f.ID != nil && validRequestEnvelope(f) {
			st.respond(f.ID, st.errResp(ErrInternalError, "server busy: too many frames waiting behind a running request"))
		}
		return
	}
	r.held = append(r.held, line)
	r.heldBytes += len(line)
}

func (st *serverState) deliverAnswer(frame wireRequest) {
	if frame.ID == nil {
		return // neither a valid request nor a response
	}
	var id int64
	if err := json.Unmarshal(frame.ID, &id); err != nil {
		return
	}
	resp := pendingResp{Result: frame.Result, Error: frame.Error}
	if frame.JSONRPC != "2.0" {
		resp = pendingResp{Error: &wireRespError{Code: ErrInvalidRequest, Message: "invalid response: not a JSON-RPC 2.0 response object"}}
	}
	st.conn.deliver(id, resp)
}

func readLines(ctx context.Context, in io.Reader) (<-chan []byte, <-chan error) {
	lines := make(chan []byte)
	readErr := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(in)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			select {
			case lines <- bytes.Clone(scanner.Bytes()):
			case <-ctx.Done():
				return
			}
		}
		readErr <- scanner.Err()
	}()
	return lines, readErr
}

func decodeEnvelope(line []byte, frame *wireRequest) error {
	if err := handler.CheckTopLevelKeys(line, handler.FoldJSONKey); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	if err := dec.Decode(frame); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err != io.EOF {
		return handler.JSONTrailingData()
	}
	return nil
}

func validRequestEnvelope(frame wireRequest) bool {
	if frame.JSONRPC != "2.0" {
		return false
	}
	if frame.ID == nil {
		return true // a notification
	}
	id := bytes.TrimSpace(frame.ID)
	if len(id) == 0 {
		return false
	}
	switch id[0] {
	case '"', 'n', '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return true
	}
	return false
}

func decodeParams(raw json.RawMessage, v any) error {
	if err := handler.CheckObjectKeys(raw, handler.FoldJSONKey); err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

func (st *serverState) respond(id json.RawMessage, resp wireResponse) {
	resp.ID = id
	if resp.JSONRPC == "" {
		resp.JSONRPC = "2.0"
	}
	_ = st.conn.writeResponse(resp)
}

func (st *serverState) errResp(code int, format string, args ...any) wireResponse {
	return wireResponse{Error: &wireRespError{Code: code, Message: fmt.Sprintf(format, args...)}}
}

func (st *serverState) handleRequest(ctx context.Context, frame wireRequest) wireResponse {
	switch frame.Method {
	case "initialize":
		return st.handleInitialize(frame)
	case "authenticate":
		return st.handleAuthenticate(ctx, frame)
	case "session/new":
		return st.handleNewSession(ctx, frame)
	case "session/load":
		return st.handleLoadSession(ctx, frame)
	default:
		return st.errResp(ErrMethodNotFound, "method not found: %s", frame.Method)
	}
}

func (st *serverState) handleNotification(frame wireRequest) {
	if frame.Method != "session/cancel" {
		return // unknown notifications are ignored per JSON-RPC
	}
	var p struct {
		SessionID string `json:"sessionId"`
	}
	if err := decodeParams(frame.Params, &p); err != nil {
		return
	}
	st.mu.Lock()
	sess := st.sessions[p.SessionID]
	st.mu.Unlock()
	if sess == nil {
		return
	}
	sess.mu.Lock()
	cancel := sess.cancel
	sess.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (st *serverState) handleInitialize(frame wireRequest) wireResponse {
	var p struct {
		ProtocolVersion    int                `json:"protocolVersion"`
		ClientCapabilities ClientCapabilities `json:"clientCapabilities"`
		ClientInfo         *Implementation    `json:"clientInfo"`
	}
	if len(frame.Params) > 0 {
		if err := decodeParams(frame.Params, &p); err != nil {
			return st.errResp(ErrInvalidParams, "initialize: %v", err)
		}
	}
	st.mu.Lock()
	st.ready = true
	st.mu.Unlock()

	version := ProtocolVersion
	if p.ProtocolVersion == ProtocolVersion {
		version = p.ProtocolVersion
	}
	result := struct {
		ProtocolVersion   int               `json:"protocolVersion"`
		AgentCapabilities AgentCapabilities `json:"agentCapabilities"`
		AgentInfo         Implementation    `json:"agentInfo"`
		AuthMethods       []AuthMethod      `json:"authMethods"`
	}{
		ProtocolVersion: version,
		AgentCapabilities: AgentCapabilities{
			LoadSession: st.srv.loader != nil,
		},
		AgentInfo:   st.srv.agent.Info(),
		AuthMethods: st.srv.opts.AuthMethods,
	}
	if result.AuthMethods == nil {
		result.AuthMethods = []AuthMethod{}
	}
	return wireResponse{Result: result}
}

func (st *serverState) requireReady() bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.ready
}

func (st *serverState) sessionGate(method string) *wireRespError {
	st.mu.Lock()
	ready, authed := st.ready, st.authenticated
	st.mu.Unlock()
	if !ready {
		return &wireRespError{Code: ErrInvalidRequest, Message: method + " before initialize: initialize the connection first"}
	}
	if len(st.srv.opts.AuthMethods) > 0 && !authed {
		return &wireRespError{Code: ErrAuthRequired, Message: method + ": authentication required: call authenticate with one of the advertised authMethods first"}
	}
	return nil
}

func recoverCallback(operation, failure string, attrs []any, err *error) {
	if rec := recover(); rec != nil {
		args := append([]any{"operation", operation}, attrs...)
		args = append(args, "panic", textsafe.Recovered(rec), "stack", string(debug.Stack()))
		slog.Error("panic in ACP callback recovered", args...)
		*err = errors.New(failure)
	}
}

func (st *serverState) runAuthenticate(ctx context.Context, methodID string) (err error) {
	defer recoverCallback("authenticate", "authenticate panicked", []any{"method", textsafe.SanitizeControlBytes(methodID)}, &err)
	return st.srv.opts.Authenticate(ctx, methodID)
}

func (st *serverState) runNewSession(ctx context.Context, cwd string) (impl Session, err error) {
	defer recoverCallback("session/new", "agent NewSession panicked", nil, &err)
	return st.srv.agent.NewSession(ctx, cwd)
}

func (st *serverState) runLoadSession(ctx context.Context, sessionID, cwd string, client *Client) (impl Session, err error) {
	defer recoverCallback("session/load", "agent LoadSession panicked", []any{"session", textsafe.SanitizeControlBytes(sessionID)}, &err)
	return st.srv.loader.LoadSession(ctx, sessionID, cwd, client)
}

func runPrompt(sess *session, ctx context.Context, prompt []ContentBlock, client *Client) (reason string, err error) {
	defer recoverCallback("session/prompt", "session Prompt panicked", nil, &err)
	return sess.impl.Prompt(ctx, prompt, client)
}

func (st *serverState) handleAuthenticate(ctx context.Context, frame wireRequest) wireResponse {
	if !st.requireReady() {
		return st.errResp(ErrInvalidRequest, "authenticate before initialize: initialize the connection first")
	}
	var p struct {
		MethodID string `json:"methodId"`
	}
	if err := decodeParams(frame.Params, &p); err != nil {
		return st.errResp(ErrInvalidParams, "authenticate: %v", err)
	}
	for _, m := range st.srv.opts.AuthMethods {
		if m.ID == p.MethodID {
			if st.srv.opts.Authenticate == nil {
				return st.errResp(ErrInternalError, "authenticate: no handler configured for %q", p.MethodID)
			}
			if err := st.runAuthenticate(ctx, p.MethodID); err != nil {
				return st.errResp(ErrAuthRequired, "authenticate %q: %v", p.MethodID, err)
			}
			st.mu.Lock()
			st.authenticated = true
			st.mu.Unlock()
			return wireResponse{Result: map[string]any{}}
		}
	}
	return st.errResp(ErrInvalidParams, "authenticate: methodId %q is not one of the advertised auth methods", p.MethodID)
}

type mcpServerName struct {
	Name string `json:"name"`
}

func (st *serverState) checkSessionSetup(cwd string, mcpServers []mcpServerName, additionalDirs []string) *wireRespError {
	if cwd == "" {
		return &wireRespError{Code: ErrInvalidParams, Message: "cwd is required"}
	}
	if !filepath.IsAbs(cwd) {
		return &wireRespError{Code: ErrInvalidParams, Message: fmt.Sprintf("cwd %q must be an absolute path", cwd)}
	}
	if len(mcpServers) > 0 {
		return &wireRespError{Code: ErrInvalidParams, Message: fmt.Sprintf("this agent does not connect to MCP servers (no mcpCapabilities advertised); %d passed", len(mcpServers))}
	}
	if len(additionalDirs) > 0 {
		return &wireRespError{Code: ErrInvalidParams, Message: "this agent does not accept additionalDirectories (capability not advertised)"}
	}
	return nil
}

func (st *serverState) decodeSessionParams(frame wireRequest, method string) (sessionSetupParams, *wireRespError) {
	var p sessionSetupParams
	if err := decodeParams(frame.Params, &p); err != nil {
		return p, &wireRespError{Code: ErrInvalidParams, Message: method + ": " + handler.JSONErrorDetail(err).Error()}
	}
	if e := st.checkSessionSetup(p.CWD, p.MCPServers, p.AdditionalDirectories); e != nil {
		return p, e
	}
	return p, nil
}

type sessionSetupParams struct {
	SessionID             string          `json:"sessionId"`
	CWD                   string          `json:"cwd"`
	MCPServers            []mcpServerName `json:"mcpServers"`
	AdditionalDirectories []string        `json:"additionalDirectories"`
}

func (st *serverState) handleNewSession(ctx context.Context, frame wireRequest) wireResponse {
	if e := st.sessionGate("session/new"); e != nil {
		return wireResponse{Error: e}
	}
	p, e := st.decodeSessionParams(frame, "session/new")
	if e != nil {
		return wireResponse{Error: e}
	}
	if e := st.refuseFullSessions(""); e != nil {
		return wireResponse{Error: e}
	}
	impl, err := st.runNewSession(ctx, p.CWD)
	if err != nil {
		return st.errResp(ErrInternalError, "session/new: internal error")
	}
	if impl == nil || impl.ID() == "" {
		return st.errResp(ErrInternalError, "session/new: agent returned an empty session ID")
	}
	if e := st.recordSession(&session{impl: impl}, impl.ID(), true); e != nil {
		return wireResponse{Error: e}
	}
	return wireResponse{Result: struct {
		SessionID string `json:"sessionId"`
	}{impl.ID()}}
}

func (st *serverState) handleLoadSession(ctx context.Context, frame wireRequest) wireResponse {
	if e := st.sessionGate("session/load"); e != nil {
		return wireResponse{Error: e}
	}
	if st.srv.loader == nil {
		return st.errResp(ErrMethodNotFound, "session/load: this agent does not advertise the loadSession capability")
	}
	p, e := st.decodeSessionParams(frame, "session/load")
	if e != nil {
		return wireResponse{Error: e}
	}
	if st.sessionBusy(p.SessionID) {
		return st.errResp(ErrInvalidRequest, "session/load: a prompt turn is in progress for session %q", p.SessionID)
	}
	if e := st.refuseFullSessions(p.SessionID); e != nil {
		return wireResponse{Error: e}
	}
	client := &Client{conn: st.conn, sessionID: p.SessionID}
	impl, err := st.runLoadSession(ctx, p.SessionID, p.CWD, client)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return st.errResp(ErrResourceNotFound, "session/load: %v", err)
		}
		return st.errResp(ErrInternalError, "session/load: internal error")
	}
	if impl == nil || impl.ID() == "" {
		return st.errResp(ErrInternalError, "session/load: agent returned an empty session ID")
	}
	if impl.ID() != p.SessionID {
		return st.errResp(ErrInternalError, "session/load: agent returned a different session ID than the one requested")
	}
	if e := st.recordSession(&session{impl: impl}, p.SessionID, false); e != nil {
		return wireResponse{Error: e}
	}
	return wireResponse{Result: map[string]any{}}
}

func (st *serverState) startPrompt(ctx context.Context, frame wireRequest) {
	if e := st.sessionGate("session/prompt"); e != nil {
		st.respond(frame.ID, wireResponse{Error: e})
		return
	}
	var p struct {
		SessionID string         `json:"sessionId"`
		Prompt    []ContentBlock `json:"prompt"`
	}
	if err := decodeParams(frame.Params, &p); err != nil {
		st.respond(frame.ID, st.errResp(ErrInvalidParams, "session/prompt: %v", err))
		return
	}
	st.mu.Lock()
	sess := st.sessions[p.SessionID]
	st.mu.Unlock()
	if sess == nil {
		st.respond(frame.ID, st.errResp(ErrResourceNotFound, "session/prompt: unknown session %q", p.SessionID))
		return
	}
	if len(p.Prompt) == 0 {
		st.respond(frame.ID, st.errResp(ErrInvalidParams, "session/prompt: prompt must contain at least one content block"))
		return
	}
	for i, b := range p.Prompt {
		if b.Type != ContentText && b.Type != ContentResourceLink {
			st.respond(frame.ID, st.errResp(ErrInvalidParams,
				"session/prompt: block %d has type %q; only text and resource_link are accepted (initialize declared promptCapabilities image/audio/embeddedContext false)", i, b.Type))
			return
		}
	}

	sess.mu.Lock()
	if pending := sess.responding; pending != nil {
		sess.mu.Unlock()
		select {
		case <-pending:
		case <-ctx.Done():
			return
		}
		sess.mu.Lock()
	}
	if sess.busy {
		sess.mu.Unlock()
		st.respond(frame.ID, st.errResp(ErrInvalidRequest, "session/prompt: a prompt turn is already in progress for session %q", p.SessionID))
		return
	}
	promptCtx, cancel := context.WithCancel(ctx)
	sess.busy = true
	sess.cancel = cancel
	sess.mu.Unlock()

	go func() {
		defer cancel()
		client := &Client{conn: st.conn, sessionID: p.SessionID}
		reason, err := runPrompt(sess, promptCtx, p.Prompt, client)
		resp := func() wireResponse {
			switch {
			case promptCtx.Err() != nil:
				return wireResponse{Result: promptResult{StopReason: StopCancelled}}
			case err != nil:
				return st.errResp(ErrInternalError, "session/prompt: internal error")
			default:
				if reason == "" {
					reason = StopEndTurn
				}
				return wireResponse{Result: promptResult{StopReason: reason}}
			}
		}()
		written := make(chan struct{})
		sess.mu.Lock()
		sess.busy = false
		sess.cancel = nil
		sess.responding = written
		sess.mu.Unlock()
		st.mu.Lock()
		if st.draining[p.SessionID] == sess {
			delete(st.draining, p.SessionID)
		}
		st.mu.Unlock()
		st.respond(frame.ID, resp)
		sess.mu.Lock()
		if sess.responding == written {
			sess.responding = nil
		}
		sess.mu.Unlock()
		close(written)
	}()
}

type promptResult struct {
	StopReason string `json:"stopReason"`
}
