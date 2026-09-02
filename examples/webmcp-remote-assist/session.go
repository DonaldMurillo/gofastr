package main

// session.go owns the assist domain: the session store, the one typed
// command every caller funnels through, and the realtime channel. The
// Go files in this example split the same way a host app would: screens
// render, main wires, this file holds the state and the rules.
//
// Two boundaries live here and nowhere else:
//
//   - Role authorization. Cookies carry roles; every mutating endpoint
//     and both WebSocket endpoints re-check them. The WebMCP marker
//     header is never an authorization input (see observeToolEvent).
//   - State shape on the wire. SnapshotSource filters per role BEFORE
//     serialization, so a field a role must not see (the WebMCP
//     invocation id is support-only correlation data) never crosses
//     the transport.

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/stream"
	"github.com/DonaldMurillo/gofastr/framework/experimental/webmcp"
)

// Cookie names and lifetimes. The support cookie is Path=/ because the
// WebMCP bridge assets live under /__gofastr/, outside the /support
// page tree; the operator cookie is Path=/session so it never rides on
// a support URL. Both are HttpOnly: page script can trigger requests
// that carry them but can never read them.
const (
	supportCookieName  = "assist_support"
	operatorCookieName = "assist_operator"
	sessionTTL         = 10 * time.Minute
	cookieTTL          = 12 * time.Hour
)

// role names the two participants. It is the StateChannel's Role type
// parameter: snapshots and events are filtered by it.
type role string

const (
	roleSupport  role = "support"
	roleOperator role = "operator"
)

// Command kinds. One typed command, one apply path.
const (
	cmdInstruction = "instruction"
	cmdClear       = "clear"
	cmdAck         = "ack"
)

// assistCommand is the ONE typed mutation shape in the app. The manual
// form on the support console, the WebMCP send_instruction and
// clear_instruction tools, and the operator's acknowledgement all
// decode into this struct and call applyCommand. A rule that holds for
// one caller therefore holds for every caller; there is no second code
// path to drift.
type assistCommand struct {
	Session     string `json:"session"`
	Kind        string `json:"kind"` // cmdInstruction | cmdClear | cmdAck
	Instruction string `json:"instruction,omitempty"`

	// Ref correlates the mutation with its caller: the WebMCP
	// InvocationID when an in-browser agent issued the command, or ""
	// for the manual button. The operator's acknowledgement echoes it
	// back so support can correlate command -> delivery -> ack.
	Ref string `json:"-"`
}

// assistSnapshot is a role's view of one session at one sequence. The
// Invocation field (the WebMCP invocation id of the current
// instruction) is support-only: SnapshotFor omits it for the operator.
type assistSnapshot struct {
	Session        string `json:"session"`
	Instruction    string `json:"instruction,omitempty"`
	Acked          bool   `json:"acked"`
	OperatorOnline bool   `json:"operatorOnline"`
	SupportOnline  bool   `json:"supportOnline"`
	MediaUp        bool   `json:"mediaUp"`
	Invocation     string `json:"invocation,omitempty"` // support view only
}

// assistEvent is the pre-filter event published to the channel.
// FilterEvent decides, per role, which payload (if any) each side sees.
type assistEvent struct {
	Kind        string         `json:"kind"`
	Instruction string         `json:"instruction,omitempty"`
	Acked       bool           `json:"acked,omitempty"`
	Ref         string         `json:"ref,omitempty"`
	Online      bool           `json:"online,omitempty"`
	MediaUp     bool           `json:"mediaUp,omitempty"`
	Role        role           `json:"role,omitempty"` // presence: whose count changed
	Signal      *signalMessage `json:"-"`
}

// signalMessage is one WebRTC signaling frame relayed between peers.
// The server never interprets the payload: it validates the envelope
// (type and addressed role) and forwards it. SDP and ICE cross the Go
// process only as signaling bytes; camera media never does.
type signalMessage struct {
	From role            `json:"from"`
	To   role            `json:"to"`
	Type string          `json:"type"` // offer | answer | ice
	Data json.RawMessage `json:"data"`
}

// assistSession is one assist session and its realtime channel.
type assistSession struct {
	id        string
	joinToken string
	joinUsed  bool
	created   time.Time
	expires   time.Time

	// seq is the channel-wide version of the state below. It is read
	// and written under assistApp.mu, and SnapshotFor returns it from
	// the same locked read as the payload (the one-immutable-read
	// contract stream.SnapshotSource documents).
	seq         uint64
	instruction string
	invocation  string // WebMCP invocation id of the current instruction
	acked       bool
	mediaUp     bool
	opConns     int
	supConns    int

	channel *stream.StateChannel[role, assistSnapshot, assistEvent]

	// conns tracks the live WebSockets (with role) so the server can
	// drop a role's transport on demand: the same power a production
	// revocation needs, and the transport-death the browser test
	// replays. connIDs records the recent ids (bounded) for
	// diagnostics, so a browser reconnect reusing its client id
	// correlates across generations (WSConfig.ConnectionID contract).
	conns   map[role]*stream.WebSocketConn
	connIDs []string
}

// assistApp is the in-memory store. State lives here, not in the
// interactive layer: any request reconstructs its view from this store
// plus the request's cookies. A single-process example keeps it in
// RAM; a multi-replica deployment moves this map to a database and the
// channel fanout to a shared broker.
type assistApp struct {
	mu         sync.Mutex
	sessions   map[string]*assistSession
	byToken    map[string]string // join token -> session id
	operators  map[string]string // operator cookie value -> session id
	supporters map[string]time.Time

	// supportKey is the demo sign-in credential: ASSIST_SUPPORT_KEY
	// from the environment, or a random value printed once at boot. A
	// real deployment replaces the whole sign-in with battery/auth; the
	// cookie it mints and every check downstream stay the same.
	supportKey string

	// toolEvents is the bounded observer log (metadata only: phase,
	// name, status, class). Inputs never enter it — ToolEvent does not
	// carry them, and the format string below must keep it that way.
	toolEvents []string
}

func newAssistApp() *assistApp {
	key := os.Getenv("ASSIST_SUPPORT_KEY")
	if key == "" {
		key = randomID()
	}
	return &assistApp{
		sessions:   map[string]*assistSession{},
		byToken:    map[string]string{},
		operators:  map[string]string{},
		supporters: map[string]time.Time{},
		supportKey: key,
	}
}

// checkSupportKey compares the sign-in credential in constant time.
func (a *assistApp) checkSupportKey(candidate string) bool {
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(a.supportKey)) == 1
}

func randomID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		log.Fatalf("assist: crypto/rand unavailable: %v", err)
	}
	return hex.EncodeToString(b[:])
}

// observeToolEvent records one WebMCP ToolEvent for diagnostics and
// tests. ToolEvent deliberately carries no input body, headers, or
// query string; this log must never add them.
func (a *assistApp) observeToolEvent(ev webmcp.ToolEvent) {
	line := strings.Join([]string{
		string(ev.Phase), ev.Name, ev.Method, ev.Path,
		strconv.Itoa(ev.StatusCode), ev.ErrClass, ev.Duration.String(), ev.InvocationID,
	}, " ")
	a.mu.Lock()
	if len(a.toolEvents) >= 64 {
		a.toolEvents = a.toolEvents[1:]
	}
	a.toolEvents = append(a.toolEvents, line)
	a.mu.Unlock()
	log.Printf("assist webmcp: %s", line)
}

// createSession starts a short-lived session and its channel. The join
// token is single-use; the session id is public to both roles once
// exchanged, the token never is.
func (a *assistApp) createSession() *assistSession {
	s := &assistSession{
		id:        randomID(),
		joinToken: randomID(),
		created:   time.Now(),
		expires:   time.Now().Add(sessionTTL),
		conns:     map[role]*stream.WebSocketConn{},
	}
	s.channel = stream.NewStateChannel[role, assistSnapshot, assistEvent](sessionSource{app: a, id: s.id})
	go s.channel.Run()

	a.mu.Lock()
	a.sessions[s.id] = s
	a.byToken[s.joinToken] = s.id
	a.mu.Unlock()
	return s
}

// toolEventLog snapshots the bounded observer log for tests.
func (a *assistApp) toolEventLog() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.toolEvents...)
}

// dropRoleSocket closes the role's live WebSocket on the session: the
// server-side transport death a revocation (or a network drop) looks
// like from the browser. The page's ws module classifies the close,
// reconnects, and rehydrates from a fresh snapshot.
func (a *assistApp) dropRoleSocket(id string, r role) bool {
	s := a.lookup(id)
	if s == nil {
		return false
	}
	a.mu.Lock()
	conn := s.conns[r]
	delete(s.conns, r)
	a.mu.Unlock()
	if conn == nil {
		return false
	}
	go conn.Close()
	return true
}

// lookup returns the live session, or nil. Expired sessions are
// dropped here (lazy expiry) and their channel stopped.
func (a *assistApp) lookup(id string) *assistSession {
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessions[id]
	if !ok {
		return nil
	}
	if time.Now().After(s.expires) {
		a.dropLocked(s)
		return nil
	}
	return s
}

// dropLocked removes a session and stops its channel. Caller holds mu.
func (a *assistApp) dropLocked(s *assistSession) {
	delete(a.sessions, s.id)
	delete(a.byToken, s.joinToken)
	for tok, sid := range a.operators {
		if sid == s.id {
			delete(a.operators, tok)
		}
	}
	conns := s.conns
	s.conns = map[role]*stream.WebSocketConn{}
	go func() {
		for _, c := range conns {
			c.Close()
		}
		s.channel.Stop()
	}()
}

// joinOpen reports whether the token could still be exchanged, without
// exchanging it: the confirmation page's GET asks this, so fetching the
// link spends nothing.
func (a *assistApp) joinOpen(token string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	sid, found := a.byToken[token]
	if !found {
		return false
	}
	s := a.sessions[sid]
	return s != nil && !s.joinUsed && time.Now().Before(s.expires)
}

// exchangeJoin consumes the one-time join token and mints the
// operator's role cookie value. The second caller with the same token
// gets ok=false and learns nothing beyond "gone".
func (a *assistApp) exchangeJoin(token string) (sessionID, cookieValue string, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	sid, found := a.byToken[token]
	if !found {
		return "", "", false
	}
	s := a.sessions[sid]
	if s == nil || s.joinUsed || time.Now().After(s.expires) {
		return "", "", false
	}
	s.joinUsed = true
	cookieValue = randomID()
	a.operators[cookieValue] = s.id
	return s.id, cookieValue, true
}

// authorizeOperator reports whether cookieValue was minted by the join
// for exactly this session. A cookie from another session authorizes
// nothing here.
func (a *assistApp) authorizeOperator(cookieValue, sessionID string) bool {
	if cookieValue == "" || sessionID == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.operators[cookieValue] == sessionID && a.sessions[sessionID] != nil
}

// mintSupportCookie registers a fresh support credential and returns
// its cookie value. In a real deployment this is a login.
func (a *assistApp) mintSupportCookie() string {
	v := randomID()
	a.mu.Lock()
	a.supporters[v] = time.Now().Add(cookieTTL)
	a.mu.Unlock()
	return v
}

// authorizeSupport reports whether the request carries a live support
// cookie. This is the ONLY support authorization check; the WebMCP
// marker header is not consulted and must never be.
func (a *assistApp) authorizeSupport(r *http.Request) bool {
	c, err := r.Cookie(supportCookieName)
	if err != nil || c.Value == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	exp, ok := a.supporters[c.Value]
	return ok && time.Now().Before(exp)
}

// requireSupport is the router middleware shape of authorizeSupport
// for JSON endpoints: 403, plain, no detail a caller can mine.
func (a *assistApp) requireSupport() router.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !a.authorizeSupport(r) {
				http.Error(w, "support role required", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requireOperator is the router middleware for the operator's write
// routes: the cookie must have been minted by the join for exactly the
// {id} in the route. 403, plain, no detail a caller can mine.
func (a *assistApp) requireOperator() router.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !a.authorizeOperator(operatorCookie(r), router.Param(r, "id")) {
				http.Error(w, "operator role required", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// sameOrigin refuses cross-site mutating requests. It mirrors the
// convention battery/auth uses for its login forms (unexported there):
// Sec-Fetch-Site is the authoritative signal and is checked first;
// "cross-site" is refused outright, "same-origin" and "none" pass. The
// Origin host comparison is the fallback for clients without Fetch
// Metadata. A missing or "null" Origin passes: a legitimate top-level
// same-origin form navigation sends Origin: null (opaque origin), and
// curl never sends one. The role cookie remains the authorization
// decision; this only answers WHERE FROM.
func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if crossSite(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func crossSite(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "cross-site":
		return true
	case "same-origin", "none":
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" || origin == "null" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return true
	}
	return !strings.EqualFold(u.Host, r.Host)
}

// applyCommand is the single mutation path. Manual form posts, WebMCP
// tool calls, and the operator ack all land here after decoding into
// assistCommand. It validates, mutates under the store lock, bumps the
// channel-wide sequence, and publishes the event while still holding
// the lock: Publish is non-blocking by contract (it enqueues or drops),
// and publishing under the lock is what makes event order equal
// mutation order. Two callers racing would otherwise be able to
// enqueue their events in the opposite order of their mutations, and
// every page would converge on the wrong final state.
//
// cmd.Ref is the WebMCP InvocationID for agent-issued commands ("" for
// the manual button); the ack command echoes the instruction's ref back
// so support correlates command -> ack without trusting the tool call's
// HTTP status.
func (a *assistApp) applyCommand(cmd assistCommand) (assistSnapshot, int, error) {
	if cmd.Kind == "" || cmd.Session == "" {
		return assistSnapshot{}, http.StatusBadRequest, errBadRequest{"kind and session are required"}
	}
	s := a.lookup(cmd.Session)
	if s == nil {
		return assistSnapshot{}, http.StatusGone, errSessionGone{}
	}

	var ev assistEvent
	a.mu.Lock()
	switch cmd.Kind {
	case cmdInstruction:
		text := strings.TrimSpace(cmd.Instruction)
		if text == "" || len(text) > 500 {
			a.mu.Unlock()
			return assistSnapshot{}, http.StatusBadRequest, errBadRequest{"instruction must be 1-500 characters"}
		}
		s.instruction = text
		s.invocation = cmd.Ref
		s.acked = false
		ev = assistEvent{Kind: cmdInstruction, Instruction: text, Ref: cmd.Ref}
	case cmdClear:
		s.instruction = ""
		s.invocation = cmd.Ref
		s.acked = false
		ev = assistEvent{Kind: cmdClear, Ref: cmd.Ref}
	case cmdAck:
		s.acked = true
		ev = assistEvent{Kind: cmdAck, Acked: true, Ref: s.invocation}
	default:
		a.mu.Unlock()
		return assistSnapshot{}, http.StatusBadRequest, errBadRequest{"unknown command kind"}
	}
	s.seq++
	s.channel.Publish(ev.Kind, ev)
	snap := a.snapshotLocked(roleSupport, s)
	a.mu.Unlock()
	return snap, http.StatusOK, nil
}

// snapshotOf returns a role's view of a session id, nil-safe so a
// screen can render the empty shape while the guard middleware is
// already answering the request.
func (a *assistApp) snapshotOf(id string, r role) assistSnapshot {
	s := a.lookup(id)
	if s == nil {
		return assistSnapshot{Session: id}
	}
	return a.snapshotFor(r, s)
}

// snapshotFor builds the role's view under the store lock.
func (a *assistApp) snapshotFor(r role, s *assistSession) assistSnapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.snapshotLocked(r, s)
}

func (a *assistApp) snapshotLocked(r role, s *assistSession) assistSnapshot {
	snap := assistSnapshot{
		Session:        s.id,
		Instruction:    s.instruction,
		Acked:          s.acked,
		OperatorOnline: s.opConns > 0,
		SupportOnline:  s.supConns > 0,
		MediaUp:        s.mediaUp,
	}
	if r == roleSupport {
		snap.Invocation = s.invocation
	}
	return snap
}

// presence records a role's connection count change and publishes it.
func (a *assistApp) presence(s *assistSession, r role, delta int) {
	a.mu.Lock()
	if r == roleOperator {
		s.opConns += delta
	} else {
		s.supConns += delta
	}
	s.seq++
	s.channel.Publish("presence", assistEvent{Kind: "presence", Online: a.onlineLocked(s, r), Role: r})
	a.mu.Unlock()
}

func (a *assistApp) onlineLocked(s *assistSession, r role) bool {
	if r == roleOperator {
		return s.opConns > 0
	}
	return s.supConns > 0
}

// setMedia records a peer's report that the WebRTC media path is up or
// down. The server never observes media itself; it trusts a
// participant's boolean, which is exactly the point of the boundary.
func (a *assistApp) setMedia(s *assistSession, up bool) {
	a.mu.Lock()
	s.mediaUp = up
	s.seq++
	s.channel.Publish("media", assistEvent{Kind: "media", MediaUp: up})
	a.mu.Unlock()
}

// relaySignal forwards one signaling frame from `from` to the peer
// role. The addressed role must be the peer of the authenticated
// sender; anything else is dropped (an operator may not address the
// operator, and support may not talk to itself).
//
// A relayed frame is not session state, but it IS a channel event, and
// the channel assigns every event the next sequence. The session
// version must advance with it: a snapshot whose sequence trailed the
// events a page had already applied would be rejected by the page's
// reducer, and a reconnect would never rehydrate. Every Publish in
// this file is paired with one s.seq increment, under the same lock
// hold, for that reason.
func (a *assistApp) relaySignal(s *assistSession, from role, msg signalMessage) bool {
	peer := roleOperator
	if from == roleOperator {
		peer = roleSupport
	}
	if msg.To != peer {
		return false
	}
	switch msg.Type {
	case "offer", "answer", "ice":
	default:
		return false
	}
	// SDP for the fake camera is a few KB; the WS ReadLimit (64 KiB)
	// already bounded the frame. Refuse anything absurd anyway.
	if len(msg.Data) > 48*1024 {
		return false
	}
	a.mu.Lock()
	s.seq++
	s.channel.Publish("signal", assistEvent{Kind: "signal", Signal: &signalMessage{
		From: from, To: peer, Type: msg.Type, Data: msg.Data,
	}})
	a.mu.Unlock()
	return true
}

// recordConn remembers a connection id for diagnostics (bounded).
func (a *assistApp) recordConn(s *assistSession, id string) {
	a.mu.Lock()
	if len(s.connIDs) >= 16 {
		s.connIDs = s.connIDs[1:]
	}
	s.connIDs = append(s.connIDs, id)
	a.mu.Unlock()
}

// errBadRequest / errSessionGone give handlers the status without a
// stringly-typed API.
type errBadRequest struct{ msg string }

func (e errBadRequest) Error() string { return e.msg }

type errSessionGone struct{}

func (errSessionGone) Error() string { return "session has ended" }

// sessionSource adapts the store to stream.SnapshotSource. One type,
// one session: SnapshotFor and FilterEvent both read the same session
// under the store lock, so payload and sequence always come from one
// immutable read.
type sessionSource struct {
	app *assistApp
	id  string
}

func (src sessionSource) SnapshotFor(r role) (assistSnapshot, uint64) {
	src.app.mu.Lock()
	defer src.app.mu.Unlock()
	s, ok := src.app.sessions[src.id]
	if !ok {
		return assistSnapshot{Session: src.id}, 0
	}
	return src.app.snapshotLocked(r, s), s.seq
}

// FilterEvent shapes each event per role BEFORE serialization:
//
//   - signal: addressed delivery. An offer is FOR support, an answer
//     is FOR the operator; the sender's role never receives its own
//     SDP back, and the bytes are never marshaled for anyone else.
//   - instruction, clear, ack: both roles learn what happened; only
//     support receives the invocation ref (support-side correlation
//     data). The ref is stripped from the operator's copy here, and
//     the operator's snapshot never carried it (snapshotLocked).
//   - everything else: identical for both roles.
func (src sessionSource) FilterEvent(r role, ev assistEvent) (any, bool) {
	switch ev.Kind {
	case "signal":
		if ev.Signal == nil || ev.Signal.To != r {
			return nil, false
		}
		return signalMessage{From: ev.Signal.From, To: ev.Signal.To, Type: ev.Signal.Type, Data: ev.Signal.Data}, true
	case cmdInstruction, cmdClear, cmdAck:
		if r != roleSupport {
			ev.Ref = ""
		}
		return ev, true
	default:
		return ev, true
	}
}

// inboundMsg is what a page may send on its socket. Everything else is
// ignored; the socket is for signaling and media reports only.
type inboundMsg struct {
	Kind string          `json:"kind"` // signal | media
	To   role            `json:"to"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
	Up   bool            `json:"up"`
}

// handleWS is the per-role WebSocket endpoint: it authorizes the role
// cookie for THIS endpoint's page tree, upgrades, hydrates via the
// session's StateChannel, then relays inbound signaling frames.
//
// role comes from which endpoint the page connected to, never from the
// wire: /support/session/{id}/ws requires the support cookie and
// speaks as support; /session/{id}/ws requires the operator cookie
// minted by THIS session's join link and speaks as the operator.
//
// client is a page-minted id (stored in sessionStorage) passed as a
// query parameter: reconnects reuse it, so WSConfig.ConnectionID —
// session-role-client — correlates a browser's reconnect generations
// in server-side logs while each upgrade stays distinct.
func (a *assistApp) handleWS(r role) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		sid := router.Param(req, "id")
		if r == roleSupport {
			if !a.authorizeSupport(req) {
				http.Error(w, "support role required", http.StatusForbidden)
				return
			}
		} else if !a.authorizeOperator(operatorCookie(req), sid) {
			http.Error(w, "operator role required", http.StatusForbidden)
			return
		}
		s := a.lookup(sid)
		if s == nil {
			http.Error(w, "session has ended", http.StatusGone)
			return
		}

		client := req.URL.Query().Get("client")
		if len(client) > 64 {
			client = client[:64]
		}
		var conn *stream.WebSocketConn
		conn, err := stream.Upgrade(w, req, stream.WSConfig{
			ConnectionID: sid + "-" + string(r) + "-" + client,
			OnClose: func() {
				a.presence(s, r, -1)
				a.mu.Lock()
				if s.conns[r] == conn {
					delete(s.conns, r)
				}
				a.mu.Unlock()
			},
		})
		if err != nil {
			return // Upgrade wrote the error
		}
		a.recordConn(s, conn.ConnectionID())
		a.mu.Lock()
		if s.conns == nil {
			s.conns = map[role]*stream.WebSocketConn{}
		}
		s.conns[r] = conn
		a.mu.Unlock()

		// Hydrate before counting presence so the snapshot the page
		// applies is followed by a presence event that says "you are
		// online"; both orders converge on the same state.
		s.channel.Connect(r, conn)
		a.presence(s, r, +1)

		defer conn.Close()
		for {
			data, rerr := conn.Read()
			if rerr != nil {
				return
			}
			var in inboundMsg
			if json.Unmarshal(data, &in) != nil {
				continue
			}
			switch in.Kind {
			case "signal":
				a.relaySignal(s, r, signalMessage{To: in.To, Type: in.Type, Data: in.Data})
			case "media":
				a.setMedia(s, in.Up)
			}
		}
	})
}

// operatorCookie pulls the operator credential without leaking the
// error shape into handlers.
func operatorCookie(r *http.Request) string {
	c, err := r.Cookie(operatorCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// invocationRef surfaces the WebMCP invocation id for agent-driven
// commands so applyCommand can correlate the ack. Empty for manual
// calls: the marker header is absent there and must stay meaningless.
func invocationRef(ctx context.Context) string {
	return webmcp.InvocationID(ctx)
}
