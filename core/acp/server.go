package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/core/handler"
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

	// MaxSessions caps the live sessions ONE connection may hold;
	// session/new and session/load past the cap follow
	// SessionOverflow. 0 = 1024 (the documented default; an ACP client
	// legitimately mints one session per task over a long-lived
	// connection, so the bound is generous — but it exists, because
	// session/new is otherwise a frame-cheap unbounded write into
	// per-connection state). Negative lifts the cap.
	MaxSessions int

	// SessionOverflow selects what a connection at MaxSessions does
	// with its next session: refuse with an invalid-params error (the
	// default), or evict the connection's oldest session (canceling
	// its in-flight prompt turn, if any).
	SessionOverflow SessionOverflowPolicy
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
// until the client answers, ctx ends, or the connection dies. It works
// from any handler, session/load included. The outcome is the user's
// decision; on a transport error the caller should treat the turn as
// failed.
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
			if err := handler.UnmarshalStrict(resp.Result, &out); err != nil {
				return zero, fmt.Errorf("acp: session/request_permission: decode outcome: %w", err)
			}
		}
		// Fail closed: RequestPermissionOutcome is a two-value protocol
		// type (protocol.go), and the client is the untrusted peer on
		// this wire. A result that carries neither a selection nor a
		// cancellation — null, {}, an unknown outcome — is coerced to
		// OutcomeCancelled so a malformed or hostile answer can never
		// read as "no objection" downstream.
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

// --- connection ---------------------------------------------------------

// conn serializes writes on the output stream and routes responses to
// pending server-to-client requests.
type conn struct {
	// wslot is the one-slot write lock. A channel rather than a mutex so a
	// writer queued behind a stalled one can give up when the connection
	// closes.
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

// errConnClosed is what a Client call made after Serve returned gets.
var errConnClosed = errors.New("acp: connection closed")

// writeFrame refuses once the connection is closed: Serve has returned
// and its caller owns w again, and a write to an output nobody reads
// any more would block the turn that a retained Client is writing for.
// A writer waiting behind one stuck on such an output gives up at close
// too, instead of queueing on it forever.
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
		// Part of a frame reached the peer; anything written after it
		// would read as the rest of that frame.
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
// holding the write slot, so nilling it would both race that read and kill the arm
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
	// responding is closed once the last turn's response is written;
	// nil when no response is pending. The next prompt on the session
	// waits for it (startPrompt).
	responding chan struct{}
}

// serverState is per-Serve state; a Server may serve sequential
// connections, each with fresh sessions.
type serverState struct {
	srv  *Server
	conn *conn
	mu   sync.Mutex
	// sessions maps this connection's live session ids to their state.
	sessions map[string]*session
	// sessionOrder is the same set as a FIFO of ids (mint order) the
	// EvictOldest session policy pops; guarded by mu like sessions.
	sessionOrder []string
	// draining holds sessions EvictOldest dropped mid-turn until the
	// turn returns, so a reload of the id still sees it busy; guarded
	// by mu.
	draining map[string]*session
	ready    bool
}

// Serve reads newline-delimited JSON-RPC 2.0 frames from in and writes
// responses, notifications, and server-to-client requests to out. It
// returns nil on EOF of in and ctx.Err() if ctx is canceled first.
//
// Requests other than session/prompt are answered one at a time in read
// order; while one runs, the reader still delivers permission answers
// and holds every other frame until it is answered (runRequest). A
// session/cancel is acted on at once and held too. At most 256 frames
// or 16 MiB wait; past that a request is refused with an internal
// error. EOF cancels the running request's context. session/prompt runs in its own goroutine so the reader keeps
// accepting session/cancel notifications and permission responses
// while the turn is in flight; its response is written when the turn
// ends, after every update the turn produced.
//
// Prompt turns run under a context Serve cancels when it returns, so
// a turn still in flight at EOF ends with the connection instead of
// outliving it for as long as the caller's ctx lives.
//
// A cancel returns while a read is still blocked: the read runs in its
// own goroutine, which ends when in next yields a line, EOF or an
// error. Close in (a pipe, a socket) to end it sooner.
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
			// Frames still held were read before the input ended, but
			// nobody is left to answer them, and a handler run now
			// would hold Serve open on a live context.
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
		// The no-ambiguity rule applies to the envelope's own boundary
		// here (no key may repeat; no two keys may fold to one name;
		// unknown envelope keys refused): stdlib keeps the last
		// duplicate and folds key case, so a first-occurrence parser —
		// proxy, logger, audit trail — could otherwise disagree with
		// the dispatcher's decode. The walk stops at the top level
		// because the full-depth rule is each params decoder's to apply
		// (handler.UnmarshalStrict at every method handler): a folded
		// pair inside params is an invalid-params refusal answered WITH
		// the request id, which a frame-level refusal here cannot be —
		// it would answer id-less and hang the client's matching. The
		// 4 MiB scanner buffer in readLines is the size cap.
		if err := decodeEnvelope(line, &frame); err != nil {
			st.respond(frame.ID, wireResponse{
				JSONRPC: "2.0",
				Error:   &wireRespError{Code: ErrParseError, Message: err.Error()},
			})
			continue
		}
		// A frame carrying a method must be a JSON-RPC 2.0 Request
		// object (§4): jsonrpc exactly "2.0", and an id that is absent
		// (a notification), a string, a number, or null. Anything else
		// is refused before dispatch with -32600 and a null id, since
		// an id of the wrong type cannot be echoed back as one (§5).
		if frame.Method != "" && !validRequestEnvelope(frame) {
			st.respond(json.RawMessage("null"), wireResponse{
				JSONRPC: "2.0",
				Error:   &wireRespError{Code: ErrInvalidRequest, Message: "invalid request: not a JSON-RPC 2.0 request object"},
			})
			continue
		}
		// A frame with no method is the client ANSWERING one of our
		// requests (session/request_permission).
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

// reader is Serve's input: the line channels, the lines read while a
// request ran and still to be handled, and the read's end once seen.
type reader struct {
	lines     <-chan []byte
	readErr   <-chan error
	held      [][]byte
	heldBytes int
	ended     bool
	endErr    error
}

// runRequest answers one request in read order, while the reader keeps
// going: the handler may ask the client for permission (a load, or
// any handler through a Client kept from a turn), and only this loop
// reads the answer. Answers are delivered as they arrive; any other
// frame waits in r.held until the request is answered, so requests
// still run one at a time in the order they were read.
func (st *serverState) runRequest(ctx context.Context, frame wireRequest, r *reader) wireResponse {
	// The handler's context also ends at EOF: Serve's own cancel runs
	// only after this returns, which waits for the handler.
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
			// The handler runs under ctx, so it ends; wait for it.
		case err := <-readErr:
			// No answer can arrive now: fail a pending permission
			// request rather than wait for one.
			r.ended, r.endErr = true, err
			hcancel()
			st.conn.close()
		case line := <-lines:
			st.holdLine(line, r)
		}
	}
}

// maxHeldFrames and maxHeldBytes bound what a running request holds
// back. Past either, a request is refused at once instead of held, and
// reading goes on, so a permission answer behind it still arrives.
const (
	maxHeldFrames = 256
	maxHeldBytes  = 16 << 20
)

// holdLine takes one frame read while a request runs: an answer is
// delivered, anything else waits in r.held for its turn.
func (st *serverState) holdLine(line []byte, r *reader) {
	var f wireRequest
	parsed := decodeEnvelope(line, &f) == nil
	if parsed && f.Method == "" {
		st.deliverAnswer(f)
		return
	}
	// A cancel stops a running turn now, or it waits as long as the
	// request does. It is held as well, so a prompt still waiting here
	// that the client sent before it is cancelled once it starts.
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

// deliverAnswer routes a client frame with no method, an answer to one
// of our requests, to the request waiting on it.
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
		// Not a JSON-RPC 2.0 Response object (§5). It still settles the
		// request it names, as a refusal: dropping it would leave the
		// request waiting, and its result must never read as an approval.
		resp = pendingResp{Error: &wireRespError{Code: ErrInvalidRequest, Message: "invalid response: not a JSON-RPC 2.0 response object"}}
	}
	st.conn.deliver(id, resp)
}

// readLines scans in on its own goroutine so Serve can stop on ctx
// while a read blocks. Each line is a copy (the scanner reuses its
// buffer). The error channel carries the scan's end, nil on EOF, and is
// sent only after every line was taken, so no line is lost behind it.
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

// decodeEnvelope applies the strict decode to the frame's own boundary:
// CheckTopLevelKeys refuses repeated and case-folded envelope keys, the
// decoder refuses unknown envelope keys and exactly-one-value bodies.
// Depth is deliberately NOT walked here — every params decode runs
// handler.UnmarshalStrict, which applies the full-depth rule and can
// answer with the request's id and the invalid-params code; see the
// comment at the Serve call site.
func decodeEnvelope(line []byte, frame *wireRequest) error {
	if err := handler.CheckTopLevelKeys(line, strings.ToLower); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	if err := dec.Decode(frame); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err != io.EOF {
		return errors.New("trailing data after the JSON-RPC envelope")
	}
	return nil
}

// validRequestEnvelope reports whether a frame that names a method is
// a JSON-RPC 2.0 Request object: jsonrpc is exactly "2.0" and the id,
// when present, is a string, a number, or null.
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
		// The envelope decode already proved the value is valid JSON,
		// so its first byte names its type: string, null, or number.
		return true
	}
	return false
}

// decodeParams applies the no-ambiguity rule to one frame's params at
// every depth — the a2a decodeParams grammar: a duplicate or
// case-folded key pair is refused wherever it sits (a nested ambiguity
// resolves by parser accident exactly like a top-level one), while
// unknown fields stay tolerated because the protocol reserves the right
// to add them.
func decodeParams(raw json.RawMessage, v any) error {
	if err := handler.CheckObjectKeys(raw, strings.ToLower); err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
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
	// Notifications get no response, so a malformed body cannot be
	// refused on the wire — but it must not march on as zero-value
	// data either: an unparseable session/cancel is dropped here,
	// explicitly, rather than cancelling (or not) by accident of the
	// zero sessionId.
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

// runAuthenticate invokes the app-supplied auth callback under a
// recover guard: the transport loop that reaches here has no
// per-request net, so a panicking authenticator must become a protocol
// error, not a process crash (the contract core/mcp's checkToolGate
// established for gates).
func (st *serverState) runAuthenticate(ctx context.Context, methodID string) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("authenticate panicked")
		}
	}()
	return st.srv.opts.Authenticate(ctx, methodID)
}

// runNewSession, runLoadSession and runPrompt invoke the embedder's
// Agent, SessionLoader and Session implementations under the same
// recover guard runAuthenticate gives the auth callback: Serve is a
// bare read loop with no per-request net (and the prompt goroutine has
// none at all), so a panic in embedder code must become a protocol
// error answered to that one request — never a process crash. The
// recovered value is not echoed to the client.
func (st *serverState) runNewSession(ctx context.Context, cwd string) (impl Session, err error) {
	defer func() {
		if recover() != nil {
			impl = nil
			err = errors.New("agent NewSession panicked")
		}
	}()
	return st.srv.agent.NewSession(ctx, cwd)
}

func (st *serverState) runLoadSession(ctx context.Context, sessionID, cwd string, client *Client) (impl Session, err error) {
	defer func() {
		if recover() != nil {
			impl = nil
			err = errors.New("agent LoadSession panicked")
		}
	}()
	return st.srv.loader.LoadSession(ctx, sessionID, cwd, client)
}

func runPrompt(sess *session, ctx context.Context, prompt []ContentBlock, client *Client) (reason string, err error) {
	defer func() {
		if recover() != nil {
			reason = ""
			err = errors.New("session Prompt panicked")
		}
	}()
	return sess.impl.Prompt(ctx, prompt, client)
}

func (st *serverState) handleAuthenticate(ctx context.Context, frame wireRequest) wireResponse {
	// authenticate establishes credentials, which makes it exactly as
	// session-scoped as session/new: a connection that never sent
	// initialize has agreed to no protocol version and no capabilities,
	// and must not reach the embedder's auth callback.
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
	if err := decodeParams(frame.Params, &p); err != nil {
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
	// Refuse a frame that cannot be seated BEFORE the embedder mints a
	// session for it (the per-connection session cap; see Options).
	if e := st.refuseFullSessions(""); e != nil {
		return wireResponse{Error: e}
	}
	impl, err := st.runNewSession(ctx, p.CWD)
	if err != nil {
		// The embedder's error text is internal detail (paths, driver
		// text) and must not cross to the client on the internal-error
		// code.
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
	// A session with a prompt turn in flight cannot be reloaded:
	// replacing its entry would clear the busy flag (a second prompt
	// would then run concurrently with the first) and drop the turn's
	// cancel handle (session/cancel would no longer reach it). The
	// check is race-free because turns start only on this reader loop,
	// which is busy with this frame.
	if st.sessionBusy(p.SessionID) {
		return st.errResp(ErrInvalidRequest, "session/load: a prompt turn is in progress for session %q", p.SessionID)
	}
	// Same pre-check as session/new: never make the embedder load a
	// session this connection cannot seat. Reloading an id this
	// connection already holds replaces it and stays seated.
	if e := st.refuseFullSessions(p.SessionID); e != nil {
		return wireResponse{Error: e}
	}
	client := &Client{conn: st.conn, sessionID: p.SessionID}
	impl, err := st.runLoadSession(ctx, p.SessionID, p.CWD, client)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return st.errResp(ErrResourceNotFound, "session/load: %v", err)
		}
		// Same posture as session/new: embedder error text stays internal.
		return st.errResp(ErrInternalError, "session/load: internal error")
	}
	if impl == nil || impl.ID() == "" {
		return st.errResp(ErrInternalError, "session/load: agent returned an empty session ID")
	}
	// The busy and seat checks above ran for the requested id. A loader
	// answering with a different one would be recorded over that other
	// session, replacing its state mid-turn.
	if impl.ID() != p.SessionID {
		return st.errResp(ErrInternalError, "session/load: agent returned a different session ID than the one requested")
	}
	if e := st.recordSession(&session{impl: impl}, p.SessionID, false); e != nil {
		return wireResponse{Error: e}
	}
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
	// The last turn released busy before its response went out (see
	// below), and that response may still be waiting on a write the
	// client is not reading. Deciding this prompt only once it is
	// written keeps one parked responder per session: a client that
	// stopped reading and kept sending prompts parked one goroutine per
	// prompt. A stalled write holds this reader loop instead, as a
	// refusal written from here already does.
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
				// session/cancel fired: the spec requires the cancelled
				// stop reason even if the implementation errored out.
				return wireResponse{Result: promptResult{StopReason: StopCancelled}}
			case err != nil:
				// As in session/new: the embedder's error text stays
				// inside.
				return st.errResp(ErrInternalError, "session/prompt: internal error")
			default:
				if reason == "" {
					reason = StopEndTurn
				}
				return wireResponse{Result: promptResult{StopReason: reason}}
			}
		}()
		// Release the turn BEFORE the response goes out: a client may
		// send its next session/prompt the moment it reads this
		// response, and a still-set busy flag refuses it spuriously.
		// The next prompt waits on written instead, so it is never
		// refused for this turn and never decided before this response
		// is out.
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
