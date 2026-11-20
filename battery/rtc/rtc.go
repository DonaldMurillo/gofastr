// Package rtc is a WebRTC signaling server: rooms of peers over one
// core/stream.StateChannel WebSocket per room, addressed relay of
// offer/answer/ICE frames, ICE server configuration with per-peer
// time-limited TURN credentials, and cross-replica relay over
// core/fanout.
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
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/stream"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework"
	"golang.org/x/net/http/httpguts"
)

// DefaultPath is the WebSocket endpoint Mount registers when Config.Path
// is empty.
const DefaultPath = "/__gofastr/rtc"

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
// to a peer.
type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// TURN mints per-peer, time-limited TURN credentials in the coturn
// static-auth-secret convention (also called the TURN REST API): username
// is "<unix-expiry>:<peer id>", credential is base64(HMAC-SHA1(secret,
// username)).
type TURN struct {
	URLs   []string      // "turn:host:3478?transport=udp", "turns:host:5349"
	Secret string        // static-auth-secret configured on the TURN server
	TTL    time.Duration // credential lifetime; 0 means 1h; minimum 1m
}

// Join is the server-derived identity of one connecting peer.
type Join struct {
	Room        string // required; 1-128 bytes, printable ASCII, no whitespace
	Role        string // optional app label ("caller", "viewer"); 0-32 bytes, same charset
	PeerID      string // optional stable id; empty means the server mints one per socket
	User        string // optional identity shown to the room (a user id); empty = anonymous
	DisplayName string // optional; 0-64 bytes; control bytes refused
}

// PeerInfo is a room member as every other member sees it.
type PeerInfo struct {
	ID          string          `json:"id"`
	Role        string          `json:"role,omitempty"`
	User        string          `json:"user,omitempty"`
	DisplayName string          `json:"name,omitempty"`
	Order       int64           `json:"order"`
	Status      json.RawMessage `json:"status,omitempty"`
}

type Config struct {
	// ICEServers is the static list every peer receives (STUN servers, or
	// TURN entries with long-lived credentials the host accepts).
	ICEServers []ICEServer

	// TURN, when set, appends a per-peer time-limited entry.
	TURN *TURN

	// Authorize decides who joins which room.
	Authorize func(r *http.Request) (Join, error)

	// Path is the WebSocket endpoint Mount registers.
	Path string

	// MaxPeers caps members per room, local plus remote; default 8.
	MaxPeers int

	// MaxSignalBytes caps one signaling payload (SDP, ICE candidate); default
	// 48 KiB.
	MaxSignalBytes int

	// MaxStatusBytes caps a peer's status document; default 1 KiB.
	MaxStatusBytes int

	// MaxFramesPerSecond caps inbound frames per socket (token bucket, burst
	// = 2× rate); default 128.
	MaxFramesPerSecond int

	// MaxSocketsPerUser caps concurrent signaling sockets per principal
	// (Join.User) across every room; default 16, the core/stream seat parity
	// (SSE broker, CRUD event stream).
	MaxSocketsPerUser int

	// AnonymousSeats decides what a join with an empty Join.User gets.
	AnonymousSeats AnonymousSeatPolicy

	// RoomIdleTTL is how long an empty room's state survives before it is
	// dropped; default 1 minute.
	RoomIdleTTL time.Duration

	// WS is applied to every upgrade.
	WS stream.WSConfig

	Logger *slog.Logger // nil means slog.Default()
}

// AnonymousSeatPolicy is Config.AnonymousSeats: what a join with no
// Join.User gets.
type AnonymousSeatPolicy int

const (
	// AnonymousRefused refuses a join with no Join.User (403).
	AnonymousRefused AnonymousSeatPolicy = iota
	// AnonymousPerSocket admits a join with no Join.User and gives each such
	// socket its own seat: MaxPeers still caps the room, but
	// MaxSocketsPerUser does not apply.
	AnonymousPerSocket
)

// Signaler is one signaling server: the room set, its sockets, and the
// optional cross-replica fanout lane.
type Signaler struct {
	cfg    Config
	logger *slog.Logger

	testAtAnnounce   chan string
	testAnnounceGate chan struct{}

	mu        sync.Mutex
	rooms     map[string]*room
	seatOrder map[string][]*socketSeat

	closed bool

	fanout       fanout.Fanout
	nodeID       string
	fanoutSend   func([]byte, func(error))
	fanoutStopQ  func()
	fanoutCancel func()
	remote       map[string]map[string]*remoteEntry
	beatDone     chan struct{}
	beatWG       sync.WaitGroup

	heartbeatEvery   time.Duration
	recheckEvery     time.Duration
	remoteTTL        time.Duration
	turnRefreshEvery time.Duration
	refreshDone      chan struct{}
	refreshWG        sync.WaitGroup
}

// New validates cfg and constructs the Signaler.
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
	if cfg.AnonymousSeats != AnonymousRefused && cfg.AnonymousSeats != AnonymousPerSocket {
		panic("rtc: Config.AnonymousSeats is not AnonymousRefused or AnonymousPerSocket")
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
	if cfg.WS.ReadLimit <= 0 || cfg.WS.ReadLimit > defaultWSReadLimit {
		cfg.WS.ReadLimit = defaultWSReadLimit
	}
	const envelopeSlack = 1024
	if int64(cfg.MaxSignalBytes)+envelopeSlack > cfg.WS.ReadLimit {
		panic(fmt.Sprintf("rtc: MaxSignalBytes %d does not fit the socket read limit %d (at most %d)",
			cfg.MaxSignalBytes, cfg.WS.ReadLimit, cfg.WS.ReadLimit-envelopeSlack))
	}
	if int64(cfg.MaxStatusBytes)+envelopeSlack > cfg.WS.ReadLimit {
		panic(fmt.Sprintf("rtc: MaxStatusBytes %d does not fit the socket read limit %d (at most %d)",
			cfg.MaxStatusBytes, cfg.WS.ReadLimit, cfg.WS.ReadLimit-envelopeSlack))
	}
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
		recheckEvery:   peerRecheckEvery,
		remoteTTL:      rtcRemoteTTL,
	}
}

// Name implements framework.Plugin.
func (s *Signaler) Name() string { return "rtc" }

// ReservedEmbedPrefixes reports the path this Signaler actually mounted.
func (s *Signaler) ReservedEmbedPrefixes() []string { return []string{s.cfg.Path} }

// Init is the lifecycle path: Mount on the App (which registers the route
// and, when the app has WithFanout, wires SetFanout) and Close into app
// shutdown.
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

// Mount registers GET cfg.Path → ServeHTTP on the router.
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
	join, err := s.cfg.Authorize(r) //gofastr:allow(unrechecked) re-run per frame and on the idle ticker through the recheck closure serve's loop calls (peerHeld → Authorize(fresh)), a callback parameter the rule cannot follow
	if err != nil {
		var he *HTTPError
		if errors.As(err, &he) {
			s.refuse(w, r, he.Status, he.Reason, he.Params)
			return
		}
		s.logger.Warn("rtc: join refused", "reason", "unauthorized")
		s.refuse(w, r, http.StatusForbidden, i18n.Phrase{}, nil)
		return
	}
	s.serve(w, r, join, func(fresh *http.Request) bool {
		again, err := s.cfg.Authorize(fresh) //gofastr:allow(unrechecked) this IS the re-run the connect-time call above is judged against, inside the recheck closure handed to serve
		return err == nil && again.Room == join.Room && again.User == join.User && again.Role == join.Role
	})
}

func (s *Signaler) refuse(w http.ResponseWriter, r *http.Request, status int, reason i18n.Phrase, params map[string]any) {
	if status < 400 || status > 599 {
		status = http.StatusForbidden
	}
	if reason.IsZero() {
		reason = i18n.StatusPhrase(status)
	}
	if !httpguts.HeaderValuesContainsToken(r.Header.Values("Upgrade"), "websocket") {
		i18n.HTTPError(w, r, status, reason, params)
		return
	}
	msg := reason.Text(r.Context(), params)
	ws := s.cfg.WS
	ws.CloseTimeout = refuseCloseTimeout
	conn, err := stream.Upgrade(w, r, ws)
	if err != nil {
		i18n.HTTPError(w, r, http.StatusBadRequest, i18n.ReasonWebSocketRequired)
		return
	}
	_ = conn.CloseWithStatus(uint16(4000+status), msg)
}

const refuseCloseTimeout = 20 * time.Millisecond

// Peers returns the merged local plus live-remote roster of a room,
// sorted by Order then ID.
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

// Kick closes the local socket of one peer.
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
	closeDetached(conn)
	return true
}

// Close stops every room, closes every socket, stops the fanout
// subscription.
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

	s.stopFanout(rooms)
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

// PermissionsPolicy returns a Permissions-Policy header value that closes
// geolocation, microphone, camera, and display-capture and opens the
// named features to the app's own origin ("(self)").
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
