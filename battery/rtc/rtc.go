// Package rtc is a WebRTC signaling server: rooms of peers over one
// core/stream.StateChannel WebSocket per room, addressed relay of
// offer/answer/ICE frames, ICE server configuration with per-peer
// time-limited TURN credentials, and cross-replica relay over
// core/fanout. Signaling only: SDP and ICE candidates cross the Go
// process as uninterpreted bytes, media never does. The browser half
// is the `rtc` runtime module (`__gofastr.connectRoom`).
//
// Identity is server-derived. A client names nothing about itself on
// the wire; every Join field comes from the host's own authorization
// (cookies, sessions, handler.GetUser) through Config.Authorize. The
// TURN static-auth-secret never leaves the process: each peer's
// credential is minted at snapshot time and marshaled for that peer
// only.
//
// Usage:
//
//	app.RegisterPlugin(rtc.New(rtc.Config{
//	    ICEServers: []rtc.ICEServer{{URLs: []string{"stun:stun.example.net:3478"}}},
//	    Authorize: func(r *http.Request) (rtc.Join, error) {
//	        // resolve the room and identity from the app's own auth
//	        return rtc.Join{Room: "support-17", Role: "caller"}, nil
//	    },
//	}))
//
// Init mounts the endpoint (GET Config.Path, default /__gofastr/rtc)
// and wires Close into app shutdown; App.Mount wires SetFanout
// automatically when the app has WithFanout.
//
// Serve skips Authorize when the caller has already authorized the
// request and passes its own Join.
package rtc

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/stream"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework"
)

// DefaultPath is the WebSocket endpoint Mount registers when
// Config.Path is empty.
const DefaultPath = "/__gofastr/rtc"

// Defaults applied by New when a Config field is zero. Negative values
// are refused at New, not silently remapped.
const (
	defaultMaxPeers              = 8
	defaultMaxSignalBytes        = 48 * 1024
	defaultMaxStatusBytes        = 1024
	defaultMaxFramesPerSec       = 128
	defaultRoomIdleTTL           = time.Minute
	defaultWSSendBuffer          = 128
	defaultWSReadLimit     int64 = 64 * 1024
)

// ICEServer is one entry of the RTCConfiguration.iceServers list handed
// to a peer. JSON tags match the browser's RTCIceServer dictionary.
type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// TURN mints per-peer, time-limited TURN credentials in the coturn
// static-auth-secret convention (also called the TURN REST API):
// username is "<unix-expiry>:<peer id>", credential is
// base64(HMAC-SHA1(secret, username)). The secret is shared with the
// TURN server and never leaves the Go process; each peer receives only
// its own credential, inside its own snapshot.
type TURN struct {
	URLs   []string      // "turn:host:3478?transport=udp", "turns:host:5349"
	Secret string        // static-auth-secret configured on the TURN server
	TTL    time.Duration // credential lifetime; 0 means 1h; minimum 1m
}

// Credentials (in turn.go) returns the ICEServer entry for one peer,
// valid until now+TTL. Exported so a host can mint credentials for a
// non-browser peer or a test can pin the algorithm.

// Join is the server-derived identity of one connecting peer. Every
// field comes from the host's own authorization (cookies, sessions,
// handler.GetUser), never from the wire: a client names nothing about
// itself except through the host's Authorize.
type Join struct {
	Room        string // required; 1-128 bytes, printable ASCII, no whitespace
	Role        string // optional app label ("caller", "viewer"); 0-32 bytes, same charset
	PeerID      string // optional stable id; empty means the server mints one per socket
	User        string // optional identity shown to the room (a user id); empty = anonymous
	DisplayName string // optional; 0-64 bytes; control bytes refused
}

// PeerInfo is a room member as every other member sees it. Order is the
// join instant in Unix nanoseconds and decides negotiation politeness
// (see the wire protocol's politeness rule); ties break on ID.
type PeerInfo struct {
	ID          string          `json:"id"`
	Role        string          `json:"role,omitempty"`
	User        string          `json:"user,omitempty"`
	DisplayName string          `json:"name,omitempty"`
	Order       int64           `json:"order"`
	Status      json.RawMessage `json:"status,omitempty"`
}

type Config struct {
	// ICEServers is the static list every peer receives (STUN servers,
	// or TURN entries with long-lived credentials the host accepts).
	// Only stun:, stuns:, turn:, turns: URL schemes are allowed; New
	// refuses anything else.
	ICEServers []ICEServer

	// TURN, when set, appends a per-peer time-limited entry.
	TURN *TURN

	// Authorize decides who joins which room. Required for Mount and
	// ServeHTTP; Serve callers pass their own Join and may leave it nil.
	// A returned error refuses the upgrade with 403 (or the status
	// carried by a *HTTPError). The Join it returns is trusted.
	Authorize func(r *http.Request) (Join, error)

	// Path is the WebSocket endpoint Mount registers. Default
	// "/__gofastr/rtc". Validated like relay.Config.Path: absolute, no
	// trailing slash, no traversal, no percent-encoding.
	Path string

	// MaxPeers caps members per room, local plus remote; default 8. A
	// join past the cap is refused with 409 before the upgrade. Mesh
	// cost is O(n²) connections; an SFU is a different product.
	MaxPeers int

	// MaxSignalBytes caps one signaling payload (SDP, ICE candidate);
	// default 48 KiB. Larger frames are dropped and the peer closed.
	// A camera plus a data channel offers in 3-8 KiB; many codecs,
	// simulcast, and several tracks reach 15-25 KiB. The cap plus 1 KiB
	// of envelope must fit WS.ReadLimit (at most 64 KiB), or New panics
	// rather than accept a cap the socket would cut off first.
	MaxSignalBytes int

	// MaxStatusBytes caps a peer's status document; default 1 KiB.
	MaxStatusBytes int

	// MaxFramesPerSecond caps inbound frames per socket (token bucket,
	// burst = 2× rate); default 128. Over the cap the socket is closed.
	MaxFramesPerSecond int

	// MaxSocketsPerUser caps concurrent signaling sockets per principal
	// (Join.User) across every room; default 16, the core/stream seat
	// parity (SSE broker, CRUD event stream). A principal's next socket
	// past the cap displaces its oldest one — the replace-on-rejoin
	// policy, applied across rooms — so a reconnecting client always
	// wins and one principal can never park unbounded read loops and
	// per-room channel goroutines. Zero keeps the default; sockets whose
	// host derived no Join.User share one bucket.
	MaxSocketsPerUser int

	// RoomIdleTTL is how long an empty room's state survives before it
	// is dropped; default 1 minute. Rooms are created on first join.
	RoomIdleTTL time.Duration

	// WS is applied to every upgrade. ConnectionID and OnClose are
	// owned by the package and overwritten. Leave zero for defaults
	// (ReadLimit is forced to at most 64 KiB either way).
	WS stream.WSConfig

	Logger *slog.Logger // nil means slog.Default()
}

// Signaler is one signaling server: the room set, its sockets, and the
// optional cross-replica fanout lane. Construct with New, register
// with App.RegisterPlugin. Implements framework.Plugin.
type Signaler struct {
	cfg    Config
	logger *slog.Logger

	// mu serializes every room mutation and every channel Publish, so
	// event order on a room's channel equals mutation order (the
	// sequence discipline StateChannel snapshots reconcile against).
	mu    sync.Mutex
	rooms map[string]*room
	// seatOrder is the per-principal socket FIFO the seat cap evicts
	// from; guarded by mu (seats.go).
	seatOrder map[string][]*socketSeat

	closed bool

	// Cross-replica state; empty until SetFanout.
	fanout       fanout.Fanout
	nodeID       string
	fanoutSend   func([]byte)
	fanoutStopQ  func()
	fanoutCancel func()
	remote       map[string]map[string]*remoteEntry // room -> replica node -> roster
	beatDone     chan struct{}
	beatWG       sync.WaitGroup

	// Test knobs: production runs on the defaults (see fanout.go).
	heartbeatEvery time.Duration
	remoteTTL      time.Duration
	// turnRefreshEvery is the credential push interval (default half
	// the TURN TTL); refreshDone stops the loop, refreshWG joins it.
	turnRefreshEvery time.Duration
	refreshDone      chan struct{}
	refreshWG        sync.WaitGroup
}

// New validates cfg and constructs the Signaler. It panics on invalid
// configuration with a message prefixed "rtc:": a bad rtc Config is a
// construction-time programmer error (same posture as relay.New and
// framework.NewApp's registration panics), not a runtime condition
// the process should limp along with. Call Close when done, or let
// Init wire it to app shutdown.
func New(cfg Config) *Signaler {
	if cfg.Path == "" {
		cfg.Path = DefaultPath
	}
	if err := validatePath(cfg.Path); err != nil {
		panic(err.Error())
	}
	if err := validateICEServers(cfg); err != nil {
		panic(err.Error())
	}
	if cfg.MaxPeers < 0 || cfg.MaxSignalBytes < 0 || cfg.MaxStatusBytes < 0 ||
		cfg.MaxFramesPerSecond < 0 || cfg.RoomIdleTTL < 0 || cfg.MaxSocketsPerUser < 0 {
		panic("rtc: Config limits must not be negative")
	}
	if cfg.MaxPeers == 0 {
		cfg.MaxPeers = defaultMaxPeers
	}
	if cfg.MaxSignalBytes == 0 {
		cfg.MaxSignalBytes = defaultMaxSignalBytes
	}
	if cfg.MaxStatusBytes == 0 {
		cfg.MaxStatusBytes = defaultMaxStatusBytes
	}
	if cfg.MaxFramesPerSecond == 0 {
		cfg.MaxFramesPerSecond = defaultMaxFramesPerSec
	}
	if cfg.MaxSocketsPerUser == 0 {
		cfg.MaxSocketsPerUser = defaultSeatsPerUser
	}
	if cfg.RoomIdleTTL == 0 {
		cfg.RoomIdleTTL = defaultRoomIdleTTL
	}
	// At most 64 KiB either way: a host may tighten, never loosen.
	if cfg.WS.ReadLimit <= 0 || cfg.WS.ReadLimit > defaultWSReadLimit {
		cfg.WS.ReadLimit = defaultWSReadLimit
	}
	// A payload cap the socket cannot carry is dead config: the frame
	// is cut off by the read limit before the cap is consulted, and the
	// host would believe it allowed something it silently drops. The
	// slack covers the JSON envelope around the payload.
	const envelopeSlack = 1024
	if int64(cfg.MaxSignalBytes)+envelopeSlack > cfg.WS.ReadLimit {
		panic(fmt.Sprintf("rtc: MaxSignalBytes %d does not fit the socket read limit %d (at most %d)",
			cfg.MaxSignalBytes, cfg.WS.ReadLimit, cfg.WS.ReadLimit-envelopeSlack))
	}
	if int64(cfg.MaxStatusBytes)+envelopeSlack > cfg.WS.ReadLimit {
		panic(fmt.Sprintf("rtc: MaxStatusBytes %d does not fit the socket read limit %d (at most %d)",
			cfg.MaxStatusBytes, cfg.WS.ReadLimit, cfg.WS.ReadLimit-envelopeSlack))
	}
	// Signaling bursts (trickle ICE from a full room) queue deeper
	// than presence does, and an overflow closes the socket here
	// (see newRoomLocked), so the default buffer is four times the
	// stream package's.
	if cfg.WS.SendBuffer == 0 {
		cfg.WS.SendBuffer = defaultWSSendBuffer
	}
	cfg.WS.ConnectionID = ""
	cfg.WS.OnClose = nil
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Signaler{
		cfg:            cfg,
		logger:         logger,
		rooms:          make(map[string]*room),
		seatOrder:      make(map[string][]*socketSeat),
		remote:         make(map[string]map[string]*remoteEntry),
		heartbeatEvery: rtcHeartbeat,
		remoteTTL:      rtcRemoteTTL,
	}
}

// Name implements framework.Plugin.
func (s *Signaler) Name() string { return "rtc" }

// Init is the lifecycle path: Mount on the App (which registers the
// route and, when the app has WithFanout, wires SetFanout) and Close
// into app shutdown. A nil Config.Authorize is returned as an error,
// not a panic: plugin wiring is a runtime step the host can react to.
// Mount keeps its panic for the direct-mount spelling of the same
// rule.
func (s *Signaler) Init(app *framework.App) error {
	if s.cfg.Authorize == nil {
		return errors.New("rtc: Init: Config.Authorize is nil: the endpoint must decide who joins")
	}
	app.Mount(s)
	app.OnStop(func() error {
		s.Close()
		return nil
	})
	return nil
}

// validatePath enforces the mount contract, the same rules
// relay.Config.Path applies: absolute, clean, no percent-encoding, no
// control bytes.
func validatePath(p string) error {
	if !strings.HasPrefix(p, "/") {
		return fmt.Errorf("rtc: Config.Path %q must be absolute (start with /)", p)
	}
	if p == "/" {
		return fmt.Errorf(`rtc: Config.Path "/" would capture the whole app`)
	}
	if strings.HasSuffix(p, "/") {
		return fmt.Errorf("rtc: Config.Path %q must not end with a slash", p)
	}
	for i := range len(p) {
		c := p[i]
		if c <= 0x20 || c == 0x7f {
			return fmt.Errorf("rtc: Config.Path %q contains a control character or space", p)
		}
		if c == '%' || c == '#' || c == '?' || c == '\\' {
			return fmt.Errorf("rtc: Config.Path %q contains %q; percent-encoding and fragments are not valid here", p, string(c))
		}
	}
	for _, seg := range strings.Split(p[1:], "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("rtc: Config.Path %q contains an empty or traversal segment", p)
		}
	}
	return nil
}

func validateICEServers(cfg Config) error {
	for i, srv := range cfg.ICEServers {
		if len(srv.URLs) == 0 || !validICEURLs(srv.URLs) {
			return fmt.Errorf("rtc: ICEServers[%d]: only stun:/stuns:/turn:/turns: URLs are allowed", i)
		}
	}
	if cfg.TURN != nil {
		if len(cfg.TURN.URLs) == 0 || !validICEURLs(cfg.TURN.URLs) {
			return fmt.Errorf("rtc: TURN: only stun:/stuns:/turn:/turns: URLs are allowed")
		}
		if cfg.TURN.Secret == "" {
			return fmt.Errorf("rtc: TURN.Secret is required (the coturn static-auth-secret)")
		}
		if cfg.TURN.TTL < 0 {
			return fmt.Errorf("rtc: TURN.TTL must not be negative")
		}
	}
	return nil
}

// validJoin enforces the Join contract: the room is a plain path-shaped
// token, identity fields are bounded, and nothing carries control
// bytes into logs or the roster.
func validJoin(j Join) error {
	if !printableToken(j.Room, 1, 128) {
		return fmt.Errorf("rtc: Join.Room must be 1-128 bytes of printable ASCII without whitespace")
	}
	if !printableToken(j.Role, 0, 32) {
		return fmt.Errorf("rtc: Join.Role must be 0-32 bytes of printable ASCII without whitespace")
	}
	if !printableToken(j.PeerID, 0, 64) {
		return fmt.Errorf("rtc: Join.PeerID must be 0-64 bytes of printable ASCII without whitespace")
	}
	if len(j.User) > 128 || textsafe.HasControlBytes(j.User) {
		return fmt.Errorf("rtc: Join.User must be 0-128 bytes without control bytes")
	}
	if len(j.DisplayName) > 64 || textsafe.HasControlBytes(j.DisplayName) {
		return fmt.Errorf("rtc: Join.DisplayName must be 0-64 bytes without control bytes")
	}
	return nil
}

// printableToken reports whether s is n..max bytes of printable ASCII
// excluding whitespace (0x21-0x7E).
func printableToken(s string, min, max int) bool {
	if len(s) < min || len(s) > max {
		return false
	}
	for i := range len(s) {
		if s[i] < 0x21 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// scrubLogField strips C0 control bytes and DEL so a room name or peer
// id that somehow carries them cannot forge log lines. Join validation
// already refuses them; this is the sink-side backstop the controlbytes
// analyzer pins.
func scrubLogField(s string) string {
	if !strings.ContainsAny(s, "\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x0c\x0d\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f\x7f") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := range len(s) {
		if s[i] < 0x20 || s[i] == 0x7f {
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// Mount registers GET cfg.Path → ServeHTTP on the router. It panics if
// Config.Authorize is nil: a mounted signaling endpoint with no gate is
// a bug, not a default. The Signaler also satisfies the framework's
// Mountable seam, so App.Mount wires SetFanout automatically when the
// app has WithFanout.
func (s *Signaler) Mount(rt *router.Router) {
	if s.cfg.Authorize == nil {
		panic("rtc: Mount: Config.Authorize is nil: a mounted signaling endpoint must decide who joins")
	}
	rt.Get(s.cfg.Path, s)
}

// ServeHTTP runs Authorize then Serve.
func (s *Signaler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Authorize == nil {
		panic("rtc: ServeHTTP: Config.Authorize is nil: the endpoint must decide who joins")
	}
	join, err := s.cfg.Authorize(r)
	if err != nil {
		var he *HTTPError
		if errors.As(err, &he) {
			s.refuse(w, r, he.Status, he.Message)
			return
		}
		s.logger.Warn("rtc: join refused", "reason", "unauthorized")
		s.refuse(w, r, http.StatusForbidden, "forbidden")
		return
	}
	s.Serve(w, r, join)
}

// refuse answers a refused join. A plain request gets the HTTP status
// and msg as the body. A WebSocket upgrade request is accepted and
// closed with code 4000+status and msg as the reason: a browser cannot
// read the status of a failed handshake (the WHATWG spec withholds it
// so a page cannot probe the network), and the runtime's ws module
// classes that band as refused and does not reconnect. A status
// outside 4xx/5xx is sent as 403.
func (s *Signaler) refuse(w http.ResponseWriter, r *http.Request, status int, msg string) {
	// Clamp before either branch: a host's HTTPError with a stray or
	// zero Status must not reach WriteHeader (which panics) any more
	// than the wire.
	if status < 400 || status > 599 {
		status = http.StatusForbidden
	}
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		http.Error(w, msg, status)
		return
	}
	// The close code is on the wire before the close handshake wait
	// begins, so the wait buys a refusal nothing: at the default 1 s
	// CloseTimeout every unauthenticated dial pinned a hijacked
	// socket, its fd, and two goroutines for a second.
	ws := s.cfg.WS
	ws.CloseTimeout = refuseCloseTimeout
	conn, err := stream.Upgrade(w, r, ws)
	if err != nil {
		http.Error(w, "websocket upgrade required", http.StatusBadRequest)
		return
	}
	_ = conn.CloseWithStatus(uint16(4000+status), msg)
}

// refuseCloseTimeout bounds how long a refused socket waits for the
// peer's reciprocal close: long enough for a browser on the same host
// to answer, short enough that a flood pins nothing.
const refuseCloseTimeout = 20 * time.Millisecond

// Serve (in room.go) upgrades r to a WebSocket, joins the peer to
// join.Room, and runs the read loop until the socket closes. The
// caller has already authorized the request; join is trusted. Blocks.
// Writes the HTTP error itself on a refused join (400 bad Join, 409
// room full, 503 closed).

// Peers returns the merged local plus live-remote roster of a room,
// sorted by Order then ID. Empty for an unknown room.
func (s *Signaler) Peers(room string) []PeerInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mergedPeersLocked(room, time.Now())
}

// Rooms returns the names of rooms with at least one local member.
func (s *Signaler) Rooms() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.sweepIdleLocked(now)
	names := make([]string, 0, len(s.rooms))
	for name, rm := range s.rooms {
		if len(rm.peers) > 0 {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// Kick closes the local socket of one peer. Reports whether a local
// peer with that id was found; a peer held by another replica is not
// reached. The leave event is published when the socket's read loop
// exits. Kick is a nudge, not a revocation: the browser module
// reconnects with backoff, so a peer that must stay out is refused by
// Authorize on that reconnect, and a TURN credential it already holds
// keeps working until TURN.TTL runs out.
func (s *Signaler) Kick(room, peerID string) bool {
	s.mu.Lock()
	rm, ok := s.rooms[room]
	var conn *stream.WebSocketConn
	if ok {
		if p, live := rm.peers[peerID]; live {
			conn = p.conn
		}
	}
	s.mu.Unlock()
	if conn == nil {
		return false
	}
	conn.Close()
	return true
}

// Close stops every room, closes every socket, stops the fanout
// subscription. Safe to call twice.
func (s *Signaler) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	rooms := maps.Clone(s.rooms)
	s.rooms = make(map[string]*room)
	if s.refreshDone != nil {
		close(s.refreshDone)
	}
	s.mu.Unlock()
	s.refreshWG.Wait()

	// Graceful leave first (inside stopFanout: empty rosters are
	// published synchronously before the subscription drops), then
	// stop the rooms.
	s.stopFanout()
	for _, rm := range rooms {
		rm.channel.Stop()
	}
}

// Feature names a browser feature the Permissions-Policy helper opens.
type Feature string

const (
	Camera         Feature = "camera"
	Microphone     Feature = "microphone"
	DisplayCapture Feature = "display-capture"
)

// PermissionsPolicy returns a Permissions-Policy header value that
// closes geolocation, microphone, camera, and display-capture and
// opens the named features to the app's own origin ("(self)"). Every
// feature is written out: an omitted directive is not a closed one
// (the default allowlist for camera, microphone, and display-capture
// is self), so a header that only listed what it opened would open
// what it left out as well. Hand it to
// middleware.SecurityHeadersConfig.PermissionsPolicy. Example:
// PermissionsPolicy(Camera, Microphone) ==
// "geolocation=(), microphone=(self), camera=(self), display-capture=()".
func PermissionsPolicy(features ...Feature) string {
	wanted := make(map[Feature]bool, len(features))
	for _, f := range features {
		wanted[f] = true
	}
	parts := []string{"geolocation=()"}
	for _, f := range []Feature{Microphone, Camera, DisplayCapture} {
		if wanted[f] {
			parts = append(parts, string(f)+"=(self)")
		} else {
			parts = append(parts, string(f)+"=()")
		}
	}
	return strings.Join(parts, ", ")
}
