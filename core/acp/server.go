package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// ErrSessionNotFound reports a session/load (or session/prompt) for a
// session ID this agent does not know. The server maps it to the ACP
// resource-not-found error (-32002).
var ErrSessionNotFound = errors.New("acp: session not found")

// Agent is the embedder-supplied brain of the ACP server. Methods may
// be called concurrently; the server serializes prompt turns per
// session.
type Agent interface {
	// Info identifies the agent in the initialize response.
	Info() Implementation

	// NewSession creates one conversation. cwd is the absolute working
	// directory the client asked for. The returned Session's ID()
	// becomes the sessionId on the wire; an empty ID fails the
	// session/new request with an internal error.
	NewSession(ctx context.Context, cwd string) (Session, error)
}

// Session is one ACP conversation.
type Session interface {
	// ID is the wire sessionId. It must be stable for the session's
	// lifetime and unique within the agent.
	ID() string

	// Prompt runs one prompt turn to completion. It streams progress
	// by writing session/update notifications through out and may call
	// out.RequestPermission to gate a tool call on the user. ctx is
	// canceled when the client sends session/cancel; implementations
	// MUST return promptly (with any reason) when it fires, because
	// the server answers the client with stopReason "cancelled" only
	// after Prompt returns.
	Prompt(ctx context.Context, prompt []ContentBlock, out *Client) (string, error)
}

// SessionLoader is the optional second interface an Agent may
// implement to support session/load. When present, the server
// advertises the loadSession capability.
type SessionLoader interface {
	// LoadSession restores a session previously created on this agent
	// and replays its conversation as user/agent message chunks
	// through out BEFORE returning. Return ErrSessionNotFound (wrapped
	// is fine) for an unknown ID.
	LoadSession(ctx context.Context, sessionID, cwd string, out *Client) (Session, error)
}

// Options configures a Server. The zero value is usable.
type Options struct {
	// AuthMethods are advertised in the initialize response. Empty
	// means the agent requires no authentication and authenticate
	// rejects every methodId.
	AuthMethods []AuthMethod

	// Authenticate runs one protocol-driven authentication attempt for
	// a methodId the client picked from AuthMethods. Required when
	// AuthMethods is non-empty; an error fails the call with the
	// auth-required code (-32000).
	Authenticate func(ctx context.Context, methodID string) error
}

// Server speaks ACP v1 over newline-delimited JSON-RPC 2.0 for one
// connection. Build one with NewServer and run it with Serve.
type Server struct {
	agent  Agent
	opts   Options
	loader SessionLoader
}

// NewServer binds an Agent to a new Server. agent must be non-nil.
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

// --- wire frames --------------------------------------------------------

type wireRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	// Result/Error are only set on frames from the CLIENT answering a
	// server-to-client request (session/request_permission).
	Result json.RawMessage `json:"result,omitempty"`
	Error  *wireRespError  `json:"error,omitempty"`
}

type wireRespError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type wireResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *wireRespError  `json:"error,omitempty"`
}

// updateParams is the session/update notification body.
type updateParams struct {
	SessionID string `json:"sessionId"`
	Update    Update `json:"update"`
}

// permissionParams is the session/request_permission request body.
type permissionParams struct {
	SessionID string             `json:"sessionId"`
	ToolCall  ToolCallUpdate     `json:"toolCall"`
	Options   []PermissionOption `json:"options"`
}

// pendingResp is a client answer to one of our outbound requests.
type pendingResp struct {
	Result json.RawMessage
	Error  *wireRespError
}

// Client is the embedder's handle for talking to the connected ACP
// client during a prompt turn or a session replay: it writes
// session/update notifications and issues session/request_permission
// calls. It is safe for concurrent use. A Client is bound to one
// session; the server creates it per turn.
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

// RequestPermission asks the user to approve a tool call and blocks
// until the client answers, ctx ends, or the connection dies. The
// outcome is the user's decision; on a transport error the caller
// should treat the turn as failed.
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
	if err := c.conn.writeFrame(struct {
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
			if err := json.Unmarshal(resp.Result, &out); err != nil {
				return zero, fmt.Errorf("acp: session/request_permission: decode outcome: %w", err)
			}
		}
		return out.Outcome, nil
	case <-ctx.Done():
		return zero, ctx.Err()
	case <-c.conn.closed:
		return zero, errors.New("acp: connection closed")
	}
}

// --- connection ---------------------------------------------------------

// conn serializes writes on the output stream and routes responses to
// pending server-to-client requests.
type conn struct {
	mu        sync.Mutex
	w         io.Writer
	pmu       sync.Mutex
	pending   map[int64]chan pendingResp
	nextID    atomic.Int64
	closeOnce sync.Once
	closed    chan struct{}
}

func newConn(w io.Writer) *conn {
	return &conn{w: w, pending: map[int64]chan pendingResp{}, closed: make(chan struct{})}
}

func (c *conn) writeFrame(v any) error {
	buf, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.w.Write(append(buf, '\n'))
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

// deliver routes a client frame that answers one of our requests.
// It reports whether id matched a pending call.
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

// close signals teardown to everything selecting on c.closed. The channel
// is never set to nil: Client.RequestPermission selects on it without
// holding c.mu, so nilling it would both race that read and kill the arm
// (a select on a nil channel never fires), leaving a permission request
// issued after teardown blocked until its own context expires.
func (c *conn) close() {
	c.closeOnce.Do(func() { close(c.closed) })
}

// --- per-connection server state ----------------------------------------

type session struct {
	impl Session

	mu     sync.Mutex
	busy   bool
	cancel context.CancelFunc
}

// serverState is per-Serve state; a Server may serve sequential
// connections, each with fresh sessions.
type serverState struct {
	srv      *Server
	conn     *conn
	mu       sync.Mutex
	sessions map[string]*session
	ready    bool
}

// Serve reads newline-delimited JSON-RPC 2.0 frames from in and writes
// responses, notifications, and server-to-client requests to out. It
// returns nil on EOF of in and ctx.Err() if ctx is canceled first.
//
// Requests other than session/prompt are answered inline in read
// order. session/prompt runs in its own goroutine so the reader keeps
// accepting session/cancel notifications and permission responses
// while the turn is in flight; its response is written when the turn
// ends, after every update the turn produced.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	st := &serverState{
		srv:      s,
		conn:     newConn(out),
		sessions: map[string]*session{},
	}
	defer st.conn.close()
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var frame wireRequest
		if err := json.Unmarshal(line, &frame); err != nil {
			st.respond(frame.ID, wireResponse{
				JSONRPC: "2.0",
				Error:   &wireRespError{Code: ErrParseError, Message: err.Error()},
			})
			continue
		}
		// A frame with no method is the client ANSWERING one of our
		// requests (session/request_permission).
		if frame.Method == "" {
			if frame.ID == nil {
				continue // neither a valid request nor a response
			}
			var id int64
			if err := json.Unmarshal(frame.ID, &id); err == nil {
				st.conn.deliver(id, pendingResp{Result: frame.Result, Error: frame.Error})
			}
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
		st.respond(frame.ID, st.handleRequest(ctx, frame))
	}
	return scanner.Err()
}

// respond writes one response, stamping the request id.
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

// handleRequest dispatches one client request (anything except
// session/prompt, which startPrompt owns).
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
	_ = json.Unmarshal(frame.Params, &p)
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
		if err := json.Unmarshal(frame.Params, &p); err != nil {
			return st.errResp(ErrInvalidParams, "initialize: %v", err)
		}
	}
	st.mu.Lock()
	st.ready = true
	st.mu.Unlock()

	// Version negotiation: echo the client's version when we speak it,
	// otherwise answer with ours and let the client decide.
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
			// promptCapabilities image/audio/embeddedContext and
			// mcpCapabilities http/sse marshal as explicit false here:
			// the client learns these absences at initialize.
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

func (st *serverState) handleAuthenticate(ctx context.Context, frame wireRequest) wireResponse {
	var p struct {
		MethodID string `json:"methodId"`
	}
	if err := json.Unmarshal(frame.Params, &p); err != nil {
		return st.errResp(ErrInvalidParams, "authenticate: %v", err)
	}
	for _, m := range st.srv.opts.AuthMethods {
		if m.ID == p.MethodID {
			if st.srv.opts.Authenticate == nil {
				return st.errResp(ErrInternalError, "authenticate: no handler configured for %q", p.MethodID)
			}
			if err := st.srv.opts.Authenticate(ctx, p.MethodID); err != nil {
				return st.errResp(ErrAuthRequired, "authenticate %q: %v", p.MethodID, err)
			}
			return wireResponse{Result: map[string]any{}}
		}
	}
	return st.errResp(ErrInvalidParams, "authenticate: methodId %q is not one of the advertised auth methods", p.MethodID)
}

type mcpServerName struct {
	Name string `json:"name"`
}

// checkSessionSetup validates the session/new and session/load
// arguments this server has opinions about: cwd must be absolute,
// mcpServers must be empty (no mcpCapabilities are advertised, so
// accepting servers would promise a connection that never happens),
// and additionalDirectories must be empty (capability not advertised
// either).
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
	if err := json.Unmarshal(frame.Params, &p); err != nil {
		return p, &wireRespError{Code: ErrInvalidParams, Message: method + ": " + err.Error()}
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
	if !st.requireReady() {
		return st.errResp(ErrInvalidRequest, "session/new before initialize: initialize the connection first")
	}
	p, e := st.decodeSessionParams(frame, "session/new")
	if e != nil {
		return wireResponse{Error: e}
	}
	impl, err := st.srv.agent.NewSession(ctx, p.CWD)
	if err != nil {
		return st.errResp(ErrInternalError, "session/new: %v", err)
	}
	if impl == nil || impl.ID() == "" {
		return st.errResp(ErrInternalError, "session/new: agent returned an empty session ID")
	}
	st.mu.Lock()
	st.sessions[impl.ID()] = &session{impl: impl}
	st.mu.Unlock()
	return wireResponse{Result: struct {
		SessionID string `json:"sessionId"`
	}{impl.ID()}}
}

func (st *serverState) handleLoadSession(ctx context.Context, frame wireRequest) wireResponse {
	if !st.requireReady() {
		return st.errResp(ErrInvalidRequest, "session/load before initialize: initialize the connection first")
	}
	if st.srv.loader == nil {
		return st.errResp(ErrMethodNotFound, "session/load: this agent does not advertise the loadSession capability")
	}
	p, e := st.decodeSessionParams(frame, "session/load")
	if e != nil {
		return wireResponse{Error: e}
	}
	client := &Client{conn: st.conn, sessionID: p.SessionID}
	impl, err := st.srv.loader.LoadSession(ctx, p.SessionID, p.CWD, client)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return st.errResp(ErrResourceNotFound, "session/load: %v", err)
		}
		return st.errResp(ErrInternalError, "session/load: %v", err)
	}
	if impl == nil || impl.ID() == "" {
		return st.errResp(ErrInternalError, "session/load: agent returned an empty session ID")
	}
	st.mu.Lock()
	st.sessions[impl.ID()] = &session{impl: impl}
	st.mu.Unlock()
	return wireResponse{Result: map[string]any{}}
}

// startPrompt validates and launches one session/prompt turn on its
// own goroutine. The reader loop must stay free while the turn runs so
// session/cancel notifications and permission responses keep flowing.
func (st *serverState) startPrompt(ctx context.Context, frame wireRequest) {
	if !st.requireReady() {
		st.respond(frame.ID, st.errResp(ErrInvalidRequest, "session/prompt before initialize: initialize the connection first"))
		return
	}
	var p struct {
		SessionID string         `json:"sessionId"`
		Prompt    []ContentBlock `json:"prompt"`
	}
	if err := json.Unmarshal(frame.Params, &p); err != nil {
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
		defer func() {
			sess.mu.Lock()
			sess.busy = false
			sess.cancel = nil
			sess.mu.Unlock()
			cancel()
		}()
		client := &Client{conn: st.conn, sessionID: p.SessionID}
		reason, err := sess.impl.Prompt(promptCtx, p.Prompt, client)
		switch {
		case promptCtx.Err() != nil:
			// session/cancel fired: the spec requires the cancelled
			// stop reason even if the implementation errored out.
			st.respond(frame.ID, wireResponse{Result: promptResult{StopReason: StopCancelled}})
		case err != nil:
			st.respond(frame.ID, st.errResp(ErrInternalError, "session/prompt: %v", err))
		default:
			if reason == "" {
				reason = StopEndTurn
			}
			st.respond(frame.ID, wireResponse{Result: promptResult{StopReason: reason}})
		}
	}()
}

type promptResult struct {
	StopReason string `json:"stopReason"`
}
