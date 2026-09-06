package runtime

// rtc_e2e_test.go: the rtc runtime module (src/rtc.js) end to end in a
// real browser. Two tabs of one Chrome join one room through a
// test-local signaling server built on core/stream.StateChannel, and
// the tests assert the module's whole contract: roster hydration with
// complementary politeness, media over a fake camera, negotiated data
// channels, status propagation, reconnect generations that keep a
// connected peer connection, room close, and the no-logging rule.
//
// Skips in -short mode. Each test boots its own browser: the tabs
// share a cookie jar and the room state, and browser tests under load
// lie, so nothing here runs concurrently.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/stream"
)

// --- test signaling server -------------------------------------------
//
// A test double of the rtc server package (battery/rtc, out of bounds
// for core-ui): one room, N peers, the wire protocol of the rtc spec
// on core/stream.StateChannel with the peer id as the Role type
// parameter. The peer id comes from the ?peer= query parameter, the
// test's stand-in for the host's Authorize; the client still names
// nothing about itself on the wire.
//
// The framework's WebSocketConn cannot send a close frame with a
// reason on demand (see the header note in ws_e2e_test.go), so the
// planted close reason rides a separate raw /kill-reason endpoint:
// the page opens it, the server answers with a close frame carrying
// the planted string, and the no-logging assertion has real bytes to
// look for. The room socket itself is closed through conn.Close().

// rtcPlantedReason is the close-reason secret the no-logging test
// plants on the wire.
const rtcPlantedReason = "PLANTED-CLOSE-REASON-7f3a"

// rtcPeerInfo is a room member as the module sees it. The module also
// reads role/name/user fields when the server sends them; this double
// carries only what its assertions exercise.
type rtcPeerInfo struct {
	ID     string          `json:"id"`
	Order  int64           `json:"order"`
	Status json.RawMessage `json:"status,omitempty"`
}

// rtcICEServer is one RTCIceServer entry of the snapshot.
type rtcICEServer struct {
	URLs []string `json:"urls"`
}

// rtcSnapshot is the hydration payload: self, the other peers, and
// this peer's ICE list (empty in the test).
type rtcSnapshot struct {
	Room       string         `json:"room"`
	Self       rtcPeerInfo    `json:"self"`
	Peers      []rtcPeerInfo  `json:"peers"`
	ICEServers []rtcICEServer `json:"iceServers"`
}

// rtcEvent is the pre-filter event published to the channel.
type rtcEvent struct {
	Kind   string          // join | leave | status | signal
	Info   rtcPeerInfo     // join/leave/status subject
	Status json.RawMessage // status document
	From   string          // signal sender
	To     string          // signal recipient
	Type   string          // offer | answer | ice
	Data   json.RawMessage // opaque signal payload
	ICE    []rtcICEServer  // iceServers: the refreshed list for To
}

type rtcICEPayload struct {
	ICEServers []rtcICEServer `json:"iceServers"`
}

type rtcLeavePayload struct {
	ID string `json:"id"`
}

type rtcStatusPayload struct {
	ID     string          `json:"id"`
	Status json.RawMessage `json:"status"`
}

type rtcSignalPayload struct {
	From string          `json:"from"`
	To   string          `json:"to"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// rtcInbound is what a page may send on its socket.
type rtcInbound struct {
	Kind string          `json:"kind"` // signal | status
	To   string          `json:"to"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type rtcTestPeer struct {
	conn   *stream.WebSocketConn
	order  int64
	status json.RawMessage
}

// rtcRoom is one room: its members and its StateChannel. Every
// mutation bumps seq and publishes under the same lock hold, the
// relaySignal rule from examples/webmcp-remote-assist/session.go: a
// snapshot whose sequence trailed events a page had applied would be
// rejected by the page's reducer and a reconnect would never hydrate.
type rtcRoom struct {
	mu    sync.Mutex
	seq   uint64
	peers map[string]*rtcTestPeer
	ch    *stream.StateChannel[string, rtcSnapshot, rtcEvent]
	// Test levers. rot counts connects per ?peer= so ?rotate=1 mints a
	// fresh id each time (the battery does this when Join.PeerID is
	// empty); crossHeld and offered make two first offers cross by
	// cause rather than by clock (?crossOffer=1).
	rot       map[string]int
	crossHeld map[string]rtcInbound
	offered   map[string]bool
}

// holdForCrossing parks a peer's first offer until another peer's
// first offer passes through. Reports false when one already has, so
// the caller relays normally.
func (room *rtcRoom) holdForCrossing(id string, in rtcInbound) bool {
	room.mu.Lock()
	defer room.mu.Unlock()
	for other := range room.offered {
		if other != id {
			return false
		}
	}
	room.crossHeld[id] = in
	return true
}

// releaseCrossed records that from's first offer went through and
// relays every other peer's parked offer right behind it.
func (room *rtcRoom) releaseCrossed(from string) {
	room.mu.Lock()
	room.offered[from] = true
	type held struct {
		id string
		in rtcInbound
	}
	var rel []held
	for id, in := range room.crossHeld {
		if id != from {
			rel = append(rel, held{id, in})
			delete(room.crossHeld, id)
		}
	}
	room.mu.Unlock()
	for _, h := range rel {
		room.signal(h.id, h.in)
	}
}

// rtcSource adapts the room to stream.SnapshotSource. FilterEvent
// delivers signal only to `to`, and join/leave/status to everyone
// except the subject.
type rtcSource struct {
	room *rtcRoom
}

func (s rtcSource) SnapshotFor(id string) (rtcSnapshot, uint64) {
	r := s.room
	r.mu.Lock()
	defer r.mu.Unlock()
	snap := rtcSnapshot{Room: "rtc-e2e", Peers: []rtcPeerInfo{}, ICEServers: []rtcICEServer{}}
	if p, ok := r.peers[id]; ok {
		snap.Self = rtcPeerInfo{ID: id, Order: p.order, Status: p.status}
	}
	for pid, p := range r.peers {
		if pid == id {
			continue
		}
		snap.Peers = append(snap.Peers, rtcPeerInfo{ID: pid, Order: p.order, Status: p.status})
	}
	return snap, r.seq
}

func (s rtcSource) FilterEvent(role string, ev rtcEvent) (any, bool) {
	switch ev.Kind {
	case "join":
		if ev.Info.ID == role {
			return nil, false
		}
		return ev.Info, true
	case "leave":
		if ev.Info.ID == role {
			return nil, false
		}
		return rtcLeavePayload{ID: ev.Info.ID}, true
	case "status":
		if ev.Info.ID == role {
			return nil, false
		}
		return rtcStatusPayload{ID: ev.Info.ID, Status: ev.Status}, true
	case "signal":
		if ev.To != role {
			return nil, false
		}
		return rtcSignalPayload{From: ev.From, To: ev.To, Type: ev.Type, Data: ev.Data}, true
	case "iceServers":
		if ev.To != role {
			return nil, false
		}
		return rtcICEPayload{ICEServers: ev.ICE}, true
	}
	return nil, false
}

// pushICE publishes a refreshed ICE list to one peer, the battery's
// half-TTL TURN credential push.
func (room *rtcRoom) pushICE(id string, urls []string) bool {
	room.mu.Lock()
	defer room.mu.Unlock()
	if room.peers[id] == nil {
		return false
	}
	room.seq++
	room.ch.Publish("iceServers", rtcEvent{Kind: "iceServers", To: id, ICE: []rtcICEServer{{URLs: urls}}})
	return true
}

func newRTCRoom() *rtcRoom {
	room := &rtcRoom{peers: map[string]*rtcTestPeer{}, rot: map[string]int{}, crossHeld: map[string]rtcInbound{}, offered: map[string]bool{}}
	room.ch = stream.NewStateChannel(rtcSource{room: room})
	go room.ch.Run()
	return room
}

func (room *rtcRoom) stop() { room.ch.Stop() }

// join registers the socket as member id, replacing any previous
// socket with the same id (a reconnecting tab): the room sees leave
// then join, the replace semantics of the rtc spec.
func (room *rtcRoom) join(id string, conn *stream.WebSocketConn) {
	room.mu.Lock()
	old := room.peers[id]
	if old != nil {
		delete(room.peers, id)
		room.seq++
		room.ch.Publish("leave", rtcEvent{Kind: "leave", Info: rtcPeerInfo{ID: id, Order: old.order}})
	}
	p := &rtcTestPeer{conn: conn, order: time.Now().UnixNano()}
	room.peers[id] = p
	room.seq++
	room.ch.Publish("join", rtcEvent{Kind: "join", Info: rtcPeerInfo{ID: id, Order: p.order}})
	room.mu.Unlock()
	if old != nil {
		// Outside the lock. The old socket's drop() is a no-op: the
		// map already points at the new socket.
		_ = old.conn.Close()
	}
}

// drop removes a member and publishes the leave. Conn-identity
// guarded, so the replaced socket of a rejoining peer cannot delete
// its replacement.
func (room *rtcRoom) drop(id string, conn *stream.WebSocketConn) {
	room.mu.Lock()
	p := room.peers[id]
	if p == nil || p.conn != conn {
		room.mu.Unlock()
		return
	}
	delete(room.peers, id)
	room.seq++
	room.ch.Publish("leave", rtcEvent{Kind: "leave", Info: rtcPeerInfo{ID: id, Order: p.order}})
	room.mu.Unlock()
}

// signal relays one frame from `from` to a current member that is not
// the sender. The payload is never parsed.
func (room *rtcRoom) signal(from string, in rtcInbound) {
	room.mu.Lock()
	defer room.mu.Unlock()
	if in.To == "" || in.To == from || room.peers[in.To] == nil {
		return
	}
	switch in.Type {
	case "offer", "answer", "ice":
	default:
		return
	}
	if len(in.Data) > 48*1024 {
		return
	}
	room.seq++
	room.ch.Publish("signal", rtcEvent{Kind: "signal", From: from, To: in.To, Type: in.Type, Data: in.Data})
}

// setStatus stores a peer's status document and broadcasts it.
func (room *rtcRoom) setStatus(id string, data json.RawMessage) {
	if len(data) == 0 || len(data) > 1024 || data[0] != '{' {
		return // must be a JSON object within the cap
	}
	room.mu.Lock()
	defer room.mu.Unlock()
	p := room.peers[id]
	if p == nil {
		return
	}
	p.status = append(json.RawMessage(nil), data...)
	room.seq++
	room.ch.Publish("status", rtcEvent{Kind: "status", Info: rtcPeerInfo{ID: id, Order: p.order}, Status: p.status})
}

// kill closes the peer's socket server-side, the revocation shape;
// the OnClose path publishes the leave.
func (room *rtcRoom) kill(id string) bool {
	room.mu.Lock()
	p := room.peers[id]
	room.mu.Unlock()
	if p == nil {
		return false
	}
	_ = p.conn.Close()
	return true
}

// serveWS is the room endpoint: upgrade, join, hydrate through the
// channel, then relay inbound signaling frames and status documents.
func (room *rtcRoom) serveWS(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("peer")
	if id == "" || len(id) > 64 {
		http.Error(w, "bad peer", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("rotate") == "1" {
		room.mu.Lock()
		room.rot[id]++
		id = id + "-" + strconv.Itoa(room.rot[id])
		room.mu.Unlock()
	}
	var conn *stream.WebSocketConn
	conn, err := stream.Upgrade(w, r, stream.WSConfig{
		ConnectionID: id,
		ReadLimit:    64 << 10,
		OnClose:      func() { room.drop(id, conn) },
	})
	if err != nil {
		return // Upgrade wrote the error
	}
	// ?refuse=<status> is the battery's post-upgrade refusal: close
	// code 4000+status, never a member.
	if st, _ := strconv.Atoi(r.URL.Query().Get("refuse")); st > 0 {
		_ = conn.CloseWithStatus(uint16(4000+st), "refused")
		return
	}
	room.join(id, conn)
	room.ch.Connect(id, conn)
	defer conn.Close()
	// ?holdOffer=<ms> delays this peer's FIRST outbound offer, so a
	// test can make two first offers cross deterministically.
	hold, _ := strconv.Atoi(r.URL.Query().Get("holdOffer"))
	held := false
	// ?pairAnswer=1 holds this peer's FIRST outbound answer until its
	// first outbound offer exists, then relays the two back to back,
	// so the far side receives the offer while its
	// setRemoteDescription(answer) is still on the operations chain.
	pair := r.URL.Query().Get("pairAnswer") == "1"
	var heldAnswer *rtcInbound
	// ?crossOffer=1 parks this peer's FIRST offer until another peer's
	// first offer passes, then relays it right behind: a crossing by
	// cause, not by a timer racing the module's 1500 ms fallback.
	cross := r.URL.Query().Get("crossOffer") == "1"
	crossed := false
	for {
		data, rerr := conn.Read()
		if rerr != nil {
			return
		}
		var in rtcInbound
		if json.Unmarshal(data, &in) != nil {
			continue // malformed JSON is ignored, like the real server
		}
		in.Data = append(json.RawMessage(nil), in.Data...)
		switch in.Kind {
		case "signal":
			if in.Type == "offer" && hold > 0 && !held {
				held = true
				time.AfterFunc(time.Duration(hold)*time.Millisecond, func() { room.signal(id, in) })
				continue
			}
			if pair && in.Type == "answer" && heldAnswer == nil {
				cp := in
				heldAnswer = &cp
				continue
			}
			if pair && in.Type == "offer" && heldAnswer != nil {
				room.signal(id, *heldAnswer)
				pair = false
			}
			if in.Type == "offer" && cross && !crossed {
				crossed = true
				if room.holdForCrossing(id, in) {
					continue
				}
			}
			room.signal(id, in)
			if in.Type == "offer" {
				room.releaseCrossed(id)
			}
		case "status":
			room.setStatus(id, in.Data)
		}
	}
}

// rtcModulePage serves the runtime, the ws and rtc demand modules, and
// the page script. Same shape as wsTestPage, plus the rtc module route.
func rtcModulePage(t *testing.T, mux *http.ServeMux, script string) string {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	for _, name := range []string{"ws", "rtc"} {
		mod, ok := Module(name)
		if !ok {
			t.Fatalf("%s module not embedded", name)
		}
		mux.HandleFunc("/__gofastr/runtime/"+name+".js", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/javascript")
			_, _ = w.Write([]byte(mod))
		})
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head></head><body><span id="ready">ready</span>`+
			`<script src="/__gofastr/runtime.js"></script><script>`+script+`</script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// rtcPageScript is the instrumentation both tabs load. window.__rtc is
// the bounded surface the Go side reads: phases, peer ids and polite
// flags, connection states, channel traffic, status documents, an
// offers-sent counter, and leave events. No SDP or candidates are
// recorded here, and nothing here logs: the console tap is the point.
const rtcPageScript = wsConsoleTap + `
(() => {
  const qp = new URLSearchParams(location.search);
  const peer = qp.get('peer');
  // ?channels= overrides the 'chat' default: absent means ['chat'],
  // empty means none, a comma list is taken verbatim. A 'label!'
  // entry asks for an unreliable, unordered channel (the object form
  // of the module's channels option).
  const chans = (qp.get('channels') === null ? 'chat' : qp.get('channels')).split(',').filter(Boolean)
    .map((c) => (c.endsWith('!') ? { label: c.slice(0, -1), ordered: false, maxRetransmits: 0 } : c));
  const R = window.__rtc = {
    peer: peer,
    phases: [],
    joined: [],
    leaves: [],
    states: [],
    chanOpen: [],
    chanMsgs: [],
    statuses: [],
    tracks: [],
    offers: 0,
    cfgs: [],
  };
  // ICE lists the module applies to a live pc (setConfiguration), as
  // comma-joined URLs; no credentials are recorded.
  const origCfg = RTCPeerConnection.prototype.setConfiguration;
  RTCPeerConnection.prototype.setConfiguration = function (cfg) {
    R.cfgs.push(((cfg && cfg.iceServers) || []).map((s) => [].concat(s.urls).join(',')).join(','));
    return origCfg.call(this, cfg);
  };
  // Offers sent by THIS tab: the module's offer path calls
  // setLocalDescription() with no argument, which Chrome resolves
  // with undefined; the pc's own localDescription carries the type
  // the call produced ('offer' vs 'answer').
  const orig = RTCPeerConnection.prototype.setLocalDescription;
  RTCPeerConnection.prototype.setLocalDescription = function (...args) {
    return orig.apply(this, args).then((d) => {
      if (this.localDescription && this.localDescription.type === 'offer') R.offers += 1;
      return d;
    });
  };
  __gofastr.loadModule('rtc').then(() => {
    const fwd = ['holdOffer', 'pairAnswer', 'refuse', 'crossOffer', 'rotate'].map((k) => (qp.get(k) ? '&' + k + '=' + encodeURIComponent(qp.get(k)) : '')).join('');
    R.room = __gofastr.connectRoom('ws://' + location.host + '/ws?peer=' + encodeURIComponent(peer) + fwd, {
      channels: chans,
      onPhase: (p) => { R.phases.push(p); },
      onSnapshot: (s) => { R.selfId = s.self && s.self.id; R.snapshotPeers = Array.from(s.peers.keys()); },
      onPeer: (p) => { R.joined.push({ id: p.id, polite: !!p.polite }); },
      onPeerLeave: (p) => { R.leaves.push(p.id); },
      onPeerState: (p, st) => { R.states.push(p.id + ':' + st); },
      onTrack: (p) => { R.tracks.push(p.id); },
      onChannel: (p, ch) => { R.chanOpen.push(p.id + ':' + ch.label + ':' + (ch.ordered ? 'ordered' : 'unordered') + ':' + String(ch.maxRetransmits)); },
      onMessage: (p, ch, ev) => {
        const d = ev.data;
        const shape = d instanceof ArrayBuffer ? 'bin:' + d.byteLength : (typeof Blob !== 'undefined' && d instanceof Blob) ? 'blob:' + d.size : String(d);
        R.chanMsgs.push(p.id + ':' + ch.label + ':' + shape);
      },
      onStatus: (p, st) => { R.statuses.push({ id: p.id, status: st }); },
    });
    window.__room = R.room;
    R.booted = true;
  }).catch((e) => { R.moduleFailed = String(e && e.message || e); });
})();
`

// rtcBrowserCtx boots a browser with the fake media flags (a camera
// device and a fake grant UI, the same pair the remote-assist browser
// test uses) and tears it down with the test.
func rtcBrowserCtx(t *testing.T) context.Context {
	t.Helper()
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("use-fake-device-for-media-stream", true),
		chromedp.Flag("use-fake-ui-for-media-stream", true),
		// Without a getUserMedia grant, Chrome anonymizes ICE host
		// candidates as mDNS .local names. Between two tabs of one
		// headless browser that resolution intermittently never
		// happens and ICE stays in 'new' forever, so tests that only
		// exercise data channels flake. Emit real host candidates.
		chromedp.Flag("disable-features", "WebRtcHideLocalIpsWithMdns"),
		chromedp.WSURLReadTimeout(90*time.Second),
		chromedp.WindowSize(1280, 800),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(allocCancel)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	t.Cleanup(browserCancel)
	started := make(chan error, 1)
	go func() { started <- chromedp.Run(browserCtx) }()
	select {
	case err := <-started:
		if err != nil {
			t.Fatalf("chrome did not start: %v", err)
		}
	case <-time.After(90 * time.Second):
		t.Fatal("chrome did not start within 90s")
	}
	// A generous ceiling for the whole test: media polls are 15s each
	// and reconnects add WebSocket backoff.
	ctx, cancel := context.WithTimeout(browserCtx, 300*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// rtcTab opens one tab of the browser at url and returns its context.
func rtcTab(t *testing.T, browser context.Context, url string) context.Context {
	t.Helper()
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	if err := chromedp.Run(ctx, page.BringToFront(), chromedp.Navigate(url), chromedp.WaitVisible(`#ready`, chromedp.ByID)); err != nil {
		t.Fatalf("tab %s: %v", url, err)
	}
	return ctx
}

// rtcFront brings the tab to front, then runs the actions: two tabs on
// one browser must never be driven concurrently (chromedp CI notes).
func rtcFront(t *testing.T, ctx context.Context, acts ...chromedp.Action) {
	t.Helper()
	all := append([]chromedp.Action{page.BringToFront()}, acts...)
	if err := chromedp.Run(ctx, all...); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
}

// rtcEval evaluates an expression that yields a string.
func rtcEval(t *testing.T, ctx context.Context, expr string) string {
	t.Helper()
	var got string
	rtcFront(t, ctx, chromedp.Evaluate(expr, &got))
	return got
}

// rtcPollTrue polls a boolean expression until it is true.
func rtcPollTrue(t *testing.T, ctx context.Context, expr string, timeout time.Duration) {
	t.Helper()
	rtcFront(t, ctx, chromedp.Poll(expr, nil,
		chromedp.WithPollingTimeout(timeout), chromedp.WithPollingInterval(200*time.Millisecond)))
}

// rtcEnv is one test's room, server, and two tabs.
type rtcEnv struct {
	room *rtcRoom
	a    context.Context
	b    context.Context
}

// rtcSetup boots the room server, the page server, one browser, and
// two tabs (alpha joins first, bravo second), and waits for both tabs
// to have loaded the module and created the room. The optional query
// suffixes (tab A, then tab B) are appended to each tab's URL, e.g.
// rtcSetup(t, "&channels=", "&channels=") for rooms with no channels.
func rtcSetup(t *testing.T, query ...string) *rtcEnv {
	t.Helper()
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	room := newRTCRoom()
	t.Cleanup(room.stop)
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", room.serveWS)
	// /resnap?peer=X queues a second snapshot on X's live socket: the
	// same sequence again, which the reducer must refuse and the
	// module must not treat as a second hydration.
	mux.HandleFunc("/resnap", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("peer")
		room.mu.Lock()
		p := room.peers[id]
		room.mu.Unlock()
		if p == nil {
			http.Error(w, "no such peer", http.StatusNotFound)
			return
		}
		room.ch.Connect(id, p.conn)
		w.WriteHeader(http.StatusNoContent)
	})
	// /push-ice?peer=X&url=U sends X a refreshed ICE list holding U.
	mux.HandleFunc("/push-ice", func(w http.ResponseWriter, r *http.Request) {
		if !room.pushICE(r.URL.Query().Get("peer"), []string{r.URL.Query().Get("url")}) {
			http.Error(w, "no such peer", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/kill-reason", func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsHandshake(w, r)
		if err != nil {
			return
		}
		_, _ = conn.Write(wsCloseReason(4000, rtcPlantedReason))
		_ = conn.Close()
	})
	base := rtcModulePage(t, mux, rtcPageScript)
	browser := rtcBrowserCtx(t)
	qa, qb := "", ""
	if len(query) > 0 {
		qa = query[0]
	}
	if len(query) > 1 {
		qb = query[1]
	}
	a := rtcTab(t, browser, base+"/?peer=alpha"+qa)
	b := rtcTab(t, browser, base+"/?peer=bravo"+qb)
	rtcPollTrue(t, a, `!!(window.__rtc && window.__rtc.booted === true && !window.__rtc.moduleFailed)`, 10*time.Second)
	rtcPollTrue(t, b, `!!(window.__rtc && window.__rtc.booted === true && !window.__rtc.moduleFailed)`, 10*time.Second)
	return &rtcEnv{room: room, a: a, b: b}
}

// Both tabs hydrate, see exactly each other, and derive complementary
// politeness from the join order: bravo joined second (greater order),
// so bravo is the polite side in both views.
func TestRTCPeersSeeEachOther(t *testing.T) {
	env := rtcSetup(t)

	rtcPollTrue(t, env.a, `!!(window.__room && window.__room.status.phase === 'hydrated' && window.__room.peers.size === 1 && window.__room.self && window.__room.self.id === 'alpha' && window.__rtc.selfId === 'alpha')`, 10*time.Second)
	rtcPollTrue(t, env.b, `!!(window.__room && window.__room.status.phase === 'hydrated' && window.__room.peers.size === 1 && window.__room.self && window.__room.self.id === 'bravo' && window.__rtc.selfId === 'bravo')`, 10*time.Second)

	if got := rtcEval(t, env.a, `JSON.stringify(Array.from(window.__room.peers.values()).map((p) => [p.id, p.polite]))`); got != `[["bravo",true]]` {
		t.Fatalf("tab A peers = %s, want bravo with polite=true (greater order is polite)", got)
	}
	if got := rtcEval(t, env.b, `JSON.stringify(Array.from(window.__room.peers.values()).map((p) => [p.id, p.polite]))`); got != `[["alpha",false]]` {
		t.Fatalf("tab B peers = %s, want alpha with polite=false (the flags must be complementary)", got)
	}
	if got := rtcEval(t, env.a, `JSON.stringify(window.__rtc.joined)`); !strings.Contains(got, `{"id":"bravo","polite":true}`) {
		t.Fatalf("tab A onPeer records = %s, want bravo as the polite peer", got)
	}
	if got := rtcEval(t, env.b, `JSON.stringify(window.__rtc.joined)`); !strings.Contains(got, `{"id":"alpha","polite":false}`) {
		t.Fatalf("tab B onPeer records = %s, want alpha as the impolite peer", got)
	}
}

// Tab A shares the fake camera; tab B receives the track and both peer
// connections reach connected.
func TestRTCMediaConnects(t *testing.T) {
	env := rtcSetup(t)

	rtcPollTrue(t, env.a, `!!(window.__room && window.__room.peers.size === 1)`, 10*time.Second)
	rtcPollTrue(t, env.b, `!!(window.__room && window.__room.peers.size === 1)`, 10*time.Second)

	rtcFront(t, env.a, chromedp.Evaluate(`navigator.mediaDevices.getUserMedia({ video: true, audio: false }).then((s) => {
      window.__stream = s;
      window.__room.addTrack(s.getVideoTracks()[0], s);
      window.__added = true;
    }, (e) => { window.__mediaErr = String(e); })`, nil))
	rtcPollTrue(t, env.a, `!!(window.__added === true || window.__mediaErr)`, 15*time.Second)
	if e := rtcEval(t, env.a, `String(window.__mediaErr || '')`); e != "" {
		t.Fatalf("getUserMedia failed on tab A: %s", e)
	}

	rtcPollTrue(t, env.a, `window.__rtc.states.indexOf('bravo:connected') >= 0`, 15*time.Second)
	rtcPollTrue(t, env.b, `window.__rtc.states.indexOf('alpha:connected') >= 0 && window.__rtc.tracks.indexOf('alpha') >= 0`, 15*time.Second)
}

// The negotiated 'chat' channel opens on both sides and carries
// messages both directions; send reports how many channels it used.
func TestRTCChannelRoundTrip(t *testing.T) {
	env := rtcSetup(t)

	rtcPollTrue(t, env.a, `window.__rtc.chanOpen.some((c) => c.startsWith('bravo:chat:'))`, 15*time.Second)
	rtcPollTrue(t, env.b, `window.__rtc.chanOpen.some((c) => c.startsWith('alpha:chat:'))`, 15*time.Second)

	rtcFront(t, env.a, chromedp.Evaluate(`window.__sent = window.__room.send('chat', 'a2b')`, nil))
	rtcPollTrue(t, env.b, `window.__rtc.chanMsgs.indexOf('alpha:chat:a2b') >= 0`, 10*time.Second)
	if got := rtcEval(t, env.a, `String(window.__sent)`); got != "1" {
		t.Fatalf("send on tab A returned %s, want 1 open channel", got)
	}

	rtcFront(t, env.b, chromedp.Evaluate(`window.__sent = window.__room.send('chat', 'b2a')`, nil))
	rtcPollTrue(t, env.a, `window.__rtc.chanMsgs.indexOf('bravo:chat:b2a') >= 0`, 10*time.Second)
	if got := rtcEval(t, env.b, `String(window.__sent)`); got != "1" {
		t.Fatalf("send on tab B returned %s, want 1 open channel", got)
	}
}

// A status document set on A arrives on B as the same object.
func TestRTCStatusPropagates(t *testing.T) {
	env := rtcSetup(t)

	rtcPollTrue(t, env.a, `!!(window.__room && window.__room.peers.size === 1)`, 10*time.Second)
	rtcPollTrue(t, env.b, `!!(window.__room && window.__room.peers.size === 1)`, 10*time.Second)

	rtcFront(t, env.a, chromedp.Evaluate(`window.__room.setStatus({ muted: true })`, nil))
	rtcPollTrue(t, env.b, `window.__rtc.statuses.length > 0`, 10*time.Second)
	if got := rtcEval(t, env.b, `JSON.stringify(window.__rtc.statuses[window.__rtc.statuses.length - 1])`); got != `{"id":"alpha","status":{"muted":true}}` {
		t.Fatalf("tab B last status = %s, want the exact document tab A set", got)
	}
}

// The server kills A's socket; A reconnects in a new generation,
// rehydrates, and KEEPS its connected peer connection: same pc object,
// connected again, zero offers sent by A across the reconnect. B,
// which saw the leave/join pair, recreates its pc and re-offers.
func TestRTCReconnectKeepsConnectedPeer(t *testing.T) {
	env := rtcSetup(t)

	// ICE is up on both sides through the negotiated channel.
	rtcPollTrue(t, env.a, `window.__rtc.chanOpen.some((c) => c.startsWith('bravo:chat:'))`, 15*time.Second)
	rtcPollTrue(t, env.b, `window.__rtc.chanOpen.some((c) => c.startsWith('alpha:chat:'))`, 15*time.Second)

	// Baseline before the kill: the pc object and the offer count.
	rtcFront(t, env.a, chromedp.Evaluate(`window.__pcB = window.__room.peers.get('bravo').pc;
      window.__offers0 = window.__rtc.offers;
      window.__gen0 = window.__room.status.generation;`, nil))

	if !env.room.kill("alpha") {
		t.Fatal("no alpha socket to kill")
	}

	rtcPollTrue(t, env.a, `!!(window.__room && window.__room.status.generation >= window.__gen0 + 1 && window.__room.status.phase === 'hydrated')`, 20*time.Second)
	rtcPollTrue(t, env.a, `!!(window.__room.peers.get('bravo') && window.__room.peers.get('bravo').pc === window.__pcB && window.__room.peers.get('bravo').state === 'connected')`, 15*time.Second)
	if got := rtcEval(t, env.a, `String(window.__rtc.offers - window.__offers0)`); got != "0" {
		t.Fatalf("tab A sent %s offers after the reconnect, want 0 (a kept connected pc must not renegotiate)", got)
	}
	rtcPollTrue(t, env.b, `!!(window.__room.peers.get('alpha') && window.__room.peers.get('alpha').state === 'connected')`, 15*time.Second)

	// B rebuilt its pc, so its offer restarted ICE and DTLS on A's kept
	// pc and closed A's old channel objects. The module must recreate
	// them on the live pc: chat has to work both ways after the
	// reconnect, not only video.
	rtcPollTrue(t, env.a, `!!(window.__room.peers.get('bravo').channels.chat && window.__room.peers.get('bravo').channels.chat.readyState === 'open')`, 15*time.Second)
	rtcFront(t, env.b, chromedp.Evaluate(`window.__room.send('chat', 'after-reconnect-b2a')`, nil))
	rtcPollTrue(t, env.a, `window.__rtc.chanMsgs.indexOf('bravo:chat:after-reconnect-b2a') >= 0`, 10*time.Second)
	rtcFront(t, env.a, chromedp.Evaluate(`window.__sent = window.__room.send('chat', 'after-reconnect-a2b')`, nil))
	rtcPollTrue(t, env.b, `window.__rtc.chanMsgs.indexOf('alpha:chat:after-reconnect-a2b') >= 0`, 10*time.Second)
	if got := rtcEval(t, env.a, `String(window.__sent)`); got != "1" {
		t.Fatalf("send after reconnect returned %s, want 1 open channel", got)
	}
}

// B closes its room; A sees the leave and its roster empties, and B's
// own phase lands on the stop class.
func TestRTCCloseRemovesPeer(t *testing.T) {
	env := rtcSetup(t)

	rtcPollTrue(t, env.a, `!!(window.__room && window.__room.peers.size === 1)`, 10*time.Second)
	rtcPollTrue(t, env.b, `!!(window.__room && window.__room.peers.size === 1)`, 10*time.Second)

	rtcFront(t, env.b, chromedp.Evaluate(`window.__room.close()`, nil))
	rtcPollTrue(t, env.a, `!!(window.__rtc.leaves.indexOf('bravo') >= 0 && window.__room.peers.size === 0)`, 10*time.Second)
	rtcPollTrue(t, env.b, `window.__rtc.phases.indexOf('closed:stop') >= 0`, 10*time.Second)
}

// Nothing sensitive reaches the console in either tab: no SDP
// (a=candidate, v=0), no credential, and not the planted close reason
// the server sent on the control socket. The channel-open
// preconditions prove SDP and candidates actually crossed the module,
// and __rawReason proves the planted reason reached the page, so the
// absence assertions are about real bytes, not a dead wire.
func TestRTCNothingSensitiveLogged(t *testing.T) {
	env := rtcSetup(t)

	rtcPollTrue(t, env.a, `window.__rtc.chanOpen.some((c) => c.startsWith('bravo:chat:'))`, 15*time.Second)
	rtcPollTrue(t, env.b, `window.__rtc.chanOpen.some((c) => c.startsWith('alpha:chat:'))`, 15*time.Second)

	rtcFront(t, env.a, chromedp.Evaluate(`(() => {
      window.__rawReason = '';
      const k = new WebSocket('ws://' + location.host + '/kill-reason');
      k.onclose = (ev) => { window.__rawReason = ev.reason || '(none)'; };
    })()`, nil))
	rtcPollTrue(t, env.a, `window.__rawReason.indexOf('PLANTED-CLOSE-REASON-7f3a') >= 0`, 10*time.Second)

	// Kill A's room socket too, so close and reconnect traffic flows
	// through the module before the console is read.
	env.room.kill("alpha")
	rtcPollTrue(t, env.a, `!!(window.__room && window.__room.status.generation >= 2 && window.__room.status.phase === 'hydrated')`, 20*time.Second)

	logsA := rtcEval(t, env.a, `JSON.stringify(window.__logs || [])`)
	logsB := rtcEval(t, env.b, `JSON.stringify(window.__logs || [])`)
	for _, sub := range []string{"a=candidate", "v=0", "credential", rtcPlantedReason} {
		if strings.Contains(logsA, sub) || strings.Contains(logsB, sub) {
			t.Fatalf("console tap recorded %q; SDP, candidates, credentials, and close reasons must never reach the log", sub)
		}
	}
}

// Both tabs add a fake camera track after the chat channel is open on
// both sides, back to back, so the second round of addTrack calls
// lands nearly simultaneously on both sides: a mid-call renegotiation
// glare. The polite side (bravo) rolls its colliding local offer back
// implicitly and the pair still converges: each side ends with two
// remote tracks and both peer connections connected.
func TestRTCRenegotiateBothSidesAtOnce(t *testing.T) {
	env := rtcSetup(t)

	// The initial negotiation must be complete before both sides
	// renegotiate at once, so the glare is mid-call, not initial.
	rtcPollTrue(t, env.a, `window.__rtc.chanOpen.some((c) => c.startsWith('bravo:chat:'))`, 15*time.Second)
	rtcPollTrue(t, env.b, `window.__rtc.chanOpen.some((c) => c.startsWith('alpha:chat:'))`, 15*time.Second)

	for round := 1; round <= 2; round++ {
		for _, ctx := range []context.Context{env.a, env.b} {
			rtcFront(t, ctx, chromedp.Evaluate(fmt.Sprintf(`navigator.mediaDevices.getUserMedia({ video: true, audio: false }).then((s) => {
      window.__room.addTrack(s.getVideoTracks()[0], s);
      window.__added%d = true;
    }, (e) => { window.__mediaErr = String(e); })`, round), nil))
		}
		for _, ctx := range []context.Context{env.a, env.b} {
			rtcPollTrue(t, ctx, fmt.Sprintf(`!!(window.__added%d === true || window.__mediaErr)`, round), 15*time.Second)
			if e := rtcEval(t, ctx, `String(window.__mediaErr || '')`); e != "" {
				t.Fatalf("round %d getUserMedia failed: %s", round, e)
			}
		}
	}

	rtcPollTrue(t, env.a, `window.__rtc.tracks.filter((id) => id === 'bravo').length >= 2`, 20*time.Second)
	rtcPollTrue(t, env.b, `window.__rtc.tracks.filter((id) => id === 'alpha').length >= 2`, 20*time.Second)
	rtcPollTrue(t, env.a, `window.__room.peers.get('bravo').state === 'connected'`, 20*time.Second)
	rtcPollTrue(t, env.b, `window.__room.peers.get('alpha').state === 'connected'`, 20*time.Second)
}

// A silent older peer and a newer peer with a track: alpha (joined
// first, impolite) has no channels and never adds anything, so it
// never fires negotiationneeded and never offers. Bravo's first offer
// is suppressed (polite, never negotiated), and the only thing that
// can connect the pair is the suppression fallback timer: alpha
// receives the track and both peer connections reach connected.
func TestRTCNewerPeerBroadcastsToSilentOlder(t *testing.T) {
	env := rtcSetup(t, "&channels=", "&channels=")

	rtcPollTrue(t, env.a, `!!(window.__room && window.__room.status.phase === 'hydrated' && window.__room.peers.size === 1)`, 10*time.Second)
	rtcPollTrue(t, env.b, `!!(window.__room && window.__room.status.phase === 'hydrated' && window.__room.peers.size === 1)`, 10*time.Second)

	rtcFront(t, env.b, chromedp.Evaluate(`navigator.mediaDevices.getUserMedia({ video: true, audio: false }).then((s) => {
      window.__room.addTrack(s.getVideoTracks()[0], s);
      window.__added = true;
    }, (e) => { window.__mediaErr = String(e); })`, nil))
	rtcPollTrue(t, env.b, `!!(window.__added === true || window.__mediaErr)`, 15*time.Second)
	if e := rtcEval(t, env.b, `String(window.__mediaErr || '')`); e != "" {
		t.Fatalf("getUserMedia failed on tab B: %s", e)
	}

	rtcPollTrue(t, env.a, `window.__rtc.tracks.indexOf('bravo') >= 0`, 20*time.Second)
	rtcPollTrue(t, env.a, `window.__room.peers.get('bravo').state === 'connected'`, 20*time.Second)
	rtcPollTrue(t, env.b, `window.__room.peers.get('alpha').state === 'connected'`, 20*time.Second)
}

// The object form of the channels option: a 'pose' channel declared
// unordered with zero retransmits opens on both sides with exactly
// those settings, beside the reliable 'chat' channel. Telemetry that
// must never queue behind a stale frame (Field Assist's phone pose
// stream was the case) needs this shape.
func TestRTCUnreliableChannelOpens(t *testing.T) {
	env := rtcSetup(t, "&channels=chat,pose!", "&channels=chat,pose!")

	rtcPollTrue(t, env.a, `window.__rtc.chanOpen.indexOf('bravo:pose:unordered:0') >= 0 && window.__rtc.chanOpen.some((c) => c.startsWith('bravo:chat:ordered:'))`, 15*time.Second)
	rtcPollTrue(t, env.b, `window.__rtc.chanOpen.indexOf('alpha:pose:unordered:0') >= 0 && window.__rtc.chanOpen.some((c) => c.startsWith('alpha:chat:ordered:'))`, 15*time.Second)

	rtcFront(t, env.a, chromedp.Evaluate(`window.__sent = window.__room.send('pose', JSON.stringify({ alpha: 1 }))`, nil))
	rtcPollTrue(t, env.b, `window.__rtc.chanMsgs.indexOf('alpha:pose:{"alpha":1}') >= 0`, 10*time.Second)
	if got := rtcEval(t, env.a, `String(window.__sent)`); got != "1" {
		t.Fatalf("send on the pose channel returned %s, want 1", got)
	}
}

// Two first offers cross: alpha (impolite) has its first offer held by
// the server for longer than bravo's fallback, so bravo (polite) offers
// first and alpha's offer lands while bravo is in have-local-offer on a
// never-negotiated pc. The module must not roll that back (Chrome then
// loops on empty offers); it rebuilds and answers. Both channels open,
// media connects, and the offer count settles.
func TestRTCFirstOfferGlareConverges(t *testing.T) {
	// alpha's first offer is parked until bravo's fallback offer passes
	// (bravo is polite and offers only from its 1500 ms fallback), so
	// the two first offers cross whatever the machine's load. A timed
	// hold with a 20 ms margin lost that race on a loaded CI box.
	env := rtcSetup(t, "&crossOffer=1", "")

	rtcPollTrue(t, env.a, `window.__rtc.chanOpen.some((c) => c.startsWith('bravo:chat:'))`, 20*time.Second)
	rtcPollTrue(t, env.b, `window.__rtc.chanOpen.some((c) => c.startsWith('alpha:chat:'))`, 20*time.Second)
	rtcPollTrue(t, env.a, `window.__rtc.states.indexOf('bravo:connected') >= 0`, 15*time.Second)
	rtcPollTrue(t, env.b, `window.__rtc.states.indexOf('alpha:connected') >= 0`, 15*time.Second)

	rtcFront(t, env.a, chromedp.Evaluate(`window.__offersA = window.__rtc.offers`, nil))
	rtcFront(t, env.b, chromedp.Evaluate(`window.__offersB = window.__rtc.offers`, nil))
	time.Sleep(3 * time.Second)
	if got := rtcEval(t, env.a, `String(window.__rtc.offers - window.__offersA)`); got != "0" {
		t.Fatalf("alpha sent %s offers after settling, want 0 (offer loop)", got)
	}
	if got := rtcEval(t, env.b, `String(window.__rtc.offers - window.__offersB)`); got != "0" {
		t.Fatalf("bravo sent %s offers after settling, want 0 (offer loop)", got)
	}
	if got := rtcEval(t, env.b, `String(window.__rtc.offers)`); got == "0" {
		t.Fatal("bravo never offered: the hold did not produce a crossing, the test proves nothing")
	}
	rtcFront(t, env.a, chromedp.Evaluate(`window.__sent = window.__room.send('chat', 'glare-ok')`, nil))
	rtcPollTrue(t, env.b, `window.__rtc.chanMsgs.indexOf('alpha:chat:glare-ok') >= 0`, 10*time.Second)
}

// A duplicate snapshot on a live socket (same sequence again) is
// refused by the reducer and must not count as a second hydration:
// exactly one 'hydrated' phase per generation.
func TestRTCDuplicateSnapshotHydratesOnce(t *testing.T) {
	env := rtcSetup(t)
	rtcPollTrue(t, env.a, `window.__rtc.phases.indexOf('hydrated') >= 0 && window.__room.peers.size === 1`, 10*time.Second)

	rtcFront(t, env.a, chromedp.Evaluate(`window.__resnap = 'pending'; fetch('/resnap?peer=alpha').then((r) => { window.__resnap = String(r.status); })`, nil))
	rtcPollTrue(t, env.a, `window.__resnap === '204'`, 10*time.Second)
	time.Sleep(500 * time.Millisecond)
	if got := rtcEval(t, env.a, `String(window.__rtc.phases.filter((p) => p === 'hydrated').length)`); got != "1" {
		t.Fatalf("hydrated phases = %s, want 1 (a refused snapshot must not rehydrate)", got)
	}
	if got := rtcEval(t, env.a, `String(window.__room.status.generation)`); got != "1" {
		t.Fatalf("generation = %s, want 1 (the socket must not have reconnected)", got)
	}
}

// A status set before a peer arrives reaches that peer through its
// snapshot as an onStatus event, not only as a silent field: bravo's
// page reloads after alpha muted, and the fresh page hears it.
func TestRTCLateJoinerSeesStatus(t *testing.T) {
	env := rtcSetup(t)
	rtcPollTrue(t, env.a, `!!(window.__room && window.__room.peers.size === 1)`, 10*time.Second)
	rtcPollTrue(t, env.b, `!!(window.__room && window.__room.peers.size === 1)`, 10*time.Second)

	rtcFront(t, env.a, chromedp.Evaluate(`window.__room.setStatus({ muted: true })`, nil))
	rtcPollTrue(t, env.b, `window.__rtc.statuses.length > 0`, 10*time.Second)

	rtcFront(t, env.b, chromedp.Reload(), chromedp.WaitVisible(`#ready`, chromedp.ByID))
	rtcPollTrue(t, env.b, `!!(window.__rtc && window.__rtc.booted === true && window.__room && window.__room.status.phase === 'hydrated')`, 15*time.Second)
	rtcPollTrue(t, env.b, `window.__rtc.statuses.some((s) => s.id === 'alpha' && s.status && s.status.muted === true)`, 10*time.Second)
}

// An offer that arrives while setRemoteDescription(answer) is still on
// the operations chain is not a collision: the connection is stable by
// the time the offer is chained (W3C perfect negotiation, the
// isSettingRemoteAnswerPending flag). The impolite side (alpha) must
// answer it, so the track bravo added right after answering reaches
// alpha. The module used to overwrite the pending-answer flag with the
// incoming offer's type before the readiness check, drop the offer,
// and leave bravo in have-local-offer for the rest of the call.
func TestRTCOfferDuringPendingAnswer(t *testing.T) {
	env := rtcSetup(t, "", "&pairAnswer=1")

	rtcPollTrue(t, env.a, `!!(window.__room && window.__room.status.phase === 'hydrated' && window.__room.peers.size === 1)`, 15*time.Second)
	rtcPollTrue(t, env.b, `!!(window.__room && window.__room.status.phase === 'hydrated' && window.__room.peers.size === 1)`, 15*time.Second)
	if got := rtcEval(t, env.a, `String(window.__room.peers.get('bravo').polite)`); got != "true" {
		t.Fatalf("bravo polite = %s, want true (alpha must be the impolite side)", got)
	}

	// Bravo has answered alpha's first offer (the answer is held by the
	// server): bravo is stable with a remote description applied.
	rtcPollTrue(t, env.b, `(() => { const pc = window.__room.peers.get('alpha').pc; return !!(pc && pc.remoteDescription && pc.signalingState === 'stable'); })()`, 20*time.Second)

	// Bravo adds a camera track: negotiationneeded fires, bravo offers,
	// and the server releases [answer, offer] back to back to alpha.
	rtcFront(t, env.b, chromedp.Evaluate(`navigator.mediaDevices.getUserMedia({ video: true, audio: false }).then((s) => {
      window.__room.addTrack(s.getVideoTracks()[0], s);
      window.__added = true;
    }, (e) => { window.__mediaErr = String(e); })`, nil))
	rtcPollTrue(t, env.b, `!!(window.__added === true || window.__mediaErr)`, 20*time.Second)
	if e := rtcEval(t, env.b, `String(window.__mediaErr || '')`); e != "" {
		t.Fatalf("getUserMedia failed on tab B: %s", e)
	}

	// Positive control: the held answer was delivered and applied.
	rtcPollTrue(t, env.a, `(() => { const pc = window.__room.peers.get('bravo').pc; return !!(pc && pc.remoteDescription); })()`, 20*time.Second)

	// Alpha must apply bravo's offer: ontrack fires on the remote
	// description, before any ICE connectivity.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if rtcEval(t, env.a, `String(window.__rtc.tracks.indexOf('bravo') >= 0)`) == "true" {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	aState := rtcEval(t, env.a, `String(window.__room.peers.get('bravo').pc.signalingState)`)
	bState := rtcEval(t, env.b, `String(window.__room.peers.get('alpha').pc.signalingState)`)
	t.Fatalf("alpha never applied bravo's offer: alpha signalingState=%s, bravo signalingState=%s (bravo stuck in have-local-offer means the offer was dropped and never answered)", aState, bState)
}

// A refreshed ICE list (the battery pushes a re-minted TURN credential
// every half TTL) reaches the live peer connections through
// setConfiguration, so a later ICE restart allocates with a credential
// that has not expired. Addressed: bravo's connections are untouched.
func TestRTCIceServersRefreshApplied(t *testing.T) {
	env := rtcSetup(t)
	rtcPollTrue(t, env.a, `!!(window.__room && window.__room.peers.get('bravo') && window.__room.peers.get('bravo').state === 'connected')`, 20*time.Second)

	const fresh = "stun:refreshed.example:3478"
	rtcFront(t, env.a, chromedp.Evaluate(`window.__push = 'pending'; fetch('/push-ice?peer=alpha&url=`+fresh+`').then((r) => { window.__push = String(r.status); })`, nil))
	rtcPollTrue(t, env.a, `window.__push === '204'`, 10*time.Second)

	rtcPollTrue(t, env.a, `window.__rtc.cfgs.some((c) => c.indexOf('`+fresh+`') >= 0)`, 10*time.Second)
	if got := rtcEval(t, env.a, `String(window.__room.peers.get('bravo').pc.getConfiguration().iceServers.some((s) => [].concat(s.urls).indexOf('`+fresh+`') >= 0))`); got != "true" {
		t.Fatalf("the kept pc's configuration does not hold the refreshed server: %s", got)
	}
	if got := rtcEval(t, env.b, `String(window.__rtc.cfgs.some((c) => c.indexOf('`+fresh+`') >= 0))`); got != "false" {
		t.Fatal("bravo applied a list addressed to alpha")
	}
}

// A refused join (the battery closes with 4000+status after the
// handshake) surfaces as phase 'closed:refused' with the status on
// room.status.refused, and the module does not retry: the page can
// say "room full" instead of spinning on a refusal for ever.
func TestRTCRefusedPhase(t *testing.T) {
	env := rtcSetup(t, "&refuse=409", "")
	rtcPollTrue(t, env.a, `window.__rtc.phases.indexOf('closed:refused') >= 0`, 15*time.Second)
	time.Sleep(3 * time.Second)
	got := rtcEval(t, env.a, `JSON.stringify({refused: window.__room.status.refused, gen: window.__room.status.generation, opens: window.__rtc.phases.filter((p) => p === 'open').length})`)
	if got != `{"refused":409,"gen":1,"opens":1}` {
		t.Fatalf("after a refusal: %s, want status 409, one generation, one open", got)
	}
}

// room.send passes binary through untouched: an ArrayBuffer view
// arrives as binary on the other side, not as the JSON of its indexed
// properties.
func TestRTCBinarySendPassesThrough(t *testing.T) {
	env := rtcSetup(t)
	rtcPollTrue(t, env.a, `window.__rtc.chanOpen.some((c) => c.startsWith('bravo:chat:'))`, 15*time.Second)
	rtcPollTrue(t, env.b, `window.__rtc.chanOpen.some((c) => c.startsWith('alpha:chat:'))`, 15*time.Second)
	if got := rtcEval(t, env.a, `String(window.__room.send('chat', new Uint8Array([1, 2, 3])))`); got != "1" {
		t.Fatalf("send returned %s, want 1", got)
	}
	rtcPollTrue(t, env.b, `window.__rtc.chanMsgs.some((m) => m === 'alpha:chat:bin:3')`, 10*time.Second)
	// One shape on every browser: the spec default moved from blob to
	// arraybuffer in 2024 and Firefox lagged, so the module pins it.
	if got := rtcEval(t, env.b, `String(window.__room.peers.get('alpha').channels.chat.binaryType)`); got != "arraybuffer" {
		t.Fatalf("channel binaryType = %s, want arraybuffer", got)
	}
}

// room.replaceTrack swaps the track on every sender without a
// renegotiation, and updates the module's own track list, so a
// connection built later (a peer that rejoins) carries the new track,
// not the one it replaced.
func TestRTCReplaceTrackKeepsTrackList(t *testing.T) {
	env := rtcSetup(t)
	rtcPollTrue(t, env.a, `window.__rtc.chanOpen.some((c) => c.startsWith('bravo:chat:'))`, 15*time.Second)

	rtcFront(t, env.a, chromedp.Evaluate(`navigator.mediaDevices.getUserMedia({ video: true, audio: false }).then((s) => {
      window.__old = s.getVideoTracks()[0];
      window.__room.addTrack(window.__old, s);
      window.__added = true;
    }, (e) => { window.__mediaErr = String(e); })`, nil))
	rtcPollTrue(t, env.a, `!!(window.__added === true || window.__mediaErr)`, 20*time.Second)
	if e := rtcEval(t, env.a, `String(window.__mediaErr || '')`); e != "" {
		t.Fatalf("getUserMedia failed on tab A: %s", e)
	}
	rtcPollTrue(t, env.b, `window.__rtc.tracks.indexOf('alpha') >= 0`, 20*time.Second)

	rtcFront(t, env.a, chromedp.Evaluate(`(() => {
      window.__offers0 = window.__rtc.offers;
      window.__pcB = window.__room.peers.get('bravo').pc;
      const c = document.createElement('canvas'); c.width = 64; c.height = 64;
      c.getContext('2d').fillRect(0, 0, 64, 64);
      window.__new = c.captureStream(5).getVideoTracks()[0];
      Promise.resolve(window.__room.replaceTrack(window.__old, window.__new)).then(() => { window.__replaced = true; }, (e) => { window.__replaceErr = String(e); });
    })()`, nil))
	rtcPollTrue(t, env.a, `!!(window.__replaced === true || window.__replaceErr)`, 10*time.Second)
	if e := rtcEval(t, env.a, `String(window.__replaceErr || '')`); e != "" {
		t.Fatalf("replaceTrack failed: %s", e)
	}
	time.Sleep(time.Second)
	if got := rtcEval(t, env.a, `String(window.__rtc.offers - window.__offers0)`); got != "0" {
		t.Fatalf("replaceTrack sent %s offers, want 0 (a sender swap needs no renegotiation)", got)
	}
	if got := rtcEval(t, env.a, `String(window.__pcB.getSenders().some((s) => s.track === window.__new))`); got != "true" {
		t.Fatal("the live sender does not carry the new track")
	}

	// Bravo's socket dies and comes back: alpha sees leave then join
	// and builds a NEW connection for bravo from its track list, which
	// must hold the replacement, not the original.
	if !env.room.kill("bravo") {
		t.Fatal("no bravo socket to kill")
	}
	rtcPollTrue(t, env.a, `(() => { const p = window.__room.peers.get('bravo'); return !!(p && p.pc && p.pc !== window.__pcB && p.pc.getSenders().some((s) => s.track === window.__new) && !p.pc.getSenders().some((s) => s.track === window.__old)); })()`, 20*time.Second)
}

// setStatus updates room.self.status locally too, so a page that
// renders its own pill from room.self is not stale until the next
// hydration.
func TestRTCSetStatusEchoesSelf(t *testing.T) {
	env := rtcSetup(t)
	rtcPollTrue(t, env.a, `!!(window.__room && window.__room.status.phase === 'hydrated')`, 15*time.Second)
	if got := rtcEval(t, env.a, `(() => { window.__room.setStatus({ muted: true }); return String(!!(window.__room.self && window.__room.self.status && window.__room.self.status.muted === true)); })()`); got != "true" {
		t.Fatal("room.self.status did not reflect setStatus")
	}
}

// room.replaceTrack(track, null) is the spec's "stop sending on this
// sender" idiom. It must drop the entry from the module's track list:
// pc.addTrack(null) is a TypeError, and a connection built later from
// a list holding null threw before its channels were created and came
// up with no channels and no onPeer.
func TestRTCReplaceTrackNullDropsEntry(t *testing.T) {
	env := rtcSetup(t)
	rtcPollTrue(t, env.a, `window.__rtc.chanOpen.some((c) => c.startsWith('bravo:chat:'))`, 20*time.Second)
	rtcPollTrue(t, env.b, `window.__rtc.chanOpen.some((c) => c.startsWith('alpha:chat:'))`, 20*time.Second)

	rtcFront(t, env.a, chromedp.Evaluate(`navigator.mediaDevices.getUserMedia({ video: true, audio: false }).then((s) => {
      window.__old = s.getVideoTracks()[0];
      window.__room.addTrack(window.__old, s);
      window.__added = true;
    }, (e) => { window.__mediaErr = String(e); })`, nil))
	rtcPollTrue(t, env.a, `!!(window.__added === true || window.__mediaErr)`, 20*time.Second)
	if e := rtcEval(t, env.a, `String(window.__mediaErr || '')`); e != "" {
		t.Fatalf("getUserMedia failed on tab A: %s", e)
	}
	rtcPollTrue(t, env.b, `window.__rtc.tracks.indexOf('alpha') >= 0`, 20*time.Second)

	rtcFront(t, env.a, chromedp.Evaluate(`Promise.resolve(window.__room.replaceTrack(window.__old, null))
      .then(() => { window.__nulled = true; }, (e) => { window.__nullErr = String(e); })`, nil))
	rtcPollTrue(t, env.a, `!!(window.__nulled === true || window.__nullErr)`, 15*time.Second)
	if e := rtcEval(t, env.a, `String(window.__nullErr || '')`); e != "" {
		t.Fatalf("replaceTrack(old, null) rejected: %s", e)
	}
	if got := rtcEval(t, env.a, `String(window.__room.peers.get('bravo').pc.getSenders().some((s) => s.track === null))`); got != "true" {
		t.Fatal("no sender was cleared")
	}

	rtcFront(t, env.a, chromedp.Evaluate(`window.__pcB = window.__room.peers.get('bravo').pc;`, nil))
	if !env.room.kill("bravo") {
		t.Fatal("no bravo socket to kill")
	}
	rtcPollTrue(t, env.a, `!!(window.__room.peers.get('bravo') && window.__room.peers.get('bravo').pc && window.__room.peers.get('bravo').pc !== window.__pcB)`, 25*time.Second)
	got := rtcEval(t, env.a, `JSON.stringify({
      chans: Object.keys(window.__room.peers.get('bravo').channels || {}),
      onPeerFired: window.__rtc.joined.filter((j) => j.id === 'bravo').length,
    })`)
	if got != `{"chans":["chat"],"onPeerFired":2}` {
		t.Fatalf("the rebuilt bravo connection: %s, want the chat channel and a second onPeer", got)
	}
}

// Both sides renegotiate at the same instant (one wall-clock epoch,
// both tabs in one browser process), asymmetrically: alpha adds two
// tracks, bravo one. The impolite side must ignore the colliding
// offer; if both sides yield they settle on different m-line sets and
// one signaling state wedges in have-local-offer for the rest of the
// call. Deleting the ignore branch turns this red.
func TestRTCAsymmetricMidCallGlare(t *testing.T) {
	env := rtcSetup(t)
	rtcPollTrue(t, env.a, `window.__rtc.chanOpen.some((c) => c.startsWith('bravo:chat:'))`, 25*time.Second)
	rtcPollTrue(t, env.b, `window.__rtc.chanOpen.some((c) => c.startsWith('alpha:chat:'))`, 25*time.Second)

	rtcFront(t, env.a, chromedp.Evaluate(`navigator.mediaDevices.getUserMedia({ video: true, audio: false }).then((s) => {
      const c = document.createElement('canvas'); c.width = 64; c.height = 64;
      c.getContext('2d').fillRect(0, 0, 64, 64);
      window.__t1 = s.getVideoTracks()[0]; window.__s1 = s;
      window.__c = c.captureStream(5); window.__t2 = window.__c.getVideoTracks()[0];
      window.__armed = true;
    }, (e) => { window.__mediaErr = String(e); })`, nil))
	rtcFront(t, env.b, chromedp.Evaluate(`navigator.mediaDevices.getUserMedia({ video: true, audio: false }).then((s) => {
      window.__t1 = s.getVideoTracks()[0]; window.__s1 = s;
      window.__armed = true;
    }, (e) => { window.__mediaErr = String(e); })`, nil))
	rtcPollTrue(t, env.a, `!!(window.__armed === true || window.__mediaErr)`, 25*time.Second)
	rtcPollTrue(t, env.b, `!!(window.__armed === true || window.__mediaErr)`, 25*time.Second)
	fire := time.Now().Add(4 * time.Second).UnixMilli()
	rtcFront(t, env.a, chromedp.Evaluate(fmt.Sprintf(`setTimeout(() => { window.__room.addTrack(window.__t1, window.__s1); window.__room.addTrack(window.__t2, window.__c); window.__fired = true; }, %d - Date.now())`, fire), nil))
	rtcFront(t, env.b, chromedp.Evaluate(fmt.Sprintf(`setTimeout(() => { window.__room.addTrack(window.__t1, window.__s1); window.__fired = true; }, %d - Date.now())`, fire), nil))
	rtcPollTrue(t, env.a, `window.__fired === true`, 20*time.Second)
	rtcPollTrue(t, env.b, `window.__fired === true`, 20*time.Second)

	rtcPollTrue(t, env.a, `window.__rtc.tracks.filter((id) => id === 'bravo').length >= 1`, 30*time.Second)
	rtcPollTrue(t, env.b, `window.__rtc.tracks.filter((id) => id === 'alpha').length >= 2`, 30*time.Second)
	rtcPollTrue(t, env.a, `window.__room.peers.get('bravo').state === 'connected'`, 30*time.Second)
	rtcPollTrue(t, env.b, `window.__room.peers.get('alpha').state === 'connected'`, 30*time.Second)
	time.Sleep(4 * time.Second)
	if got := rtcEval(t, env.a, `String(window.__room.peers.get('bravo').pc.signalingState)`); got != "stable" {
		t.Fatalf("alpha signalingState = %s, want stable", got)
	}
	if got := rtcEval(t, env.b, `String(window.__room.peers.get('alpha').pc.signalingState)`); got != "stable" {
		t.Fatalf("bravo signalingState = %s, want stable", got)
	}
}

// A signaling reconnect that lands under a NEW peer id (the battery
// mints one per socket when Join.PeerID is empty) means every remote
// saw this side leave and rebuilt its connection. The kept-connection
// rule cannot apply: the module rebuilds every peer connection, and
// the call comes back both ways.
func TestRTCNewSelfIdRebuildsConnections(t *testing.T) {
	env := rtcSetup(t, "&rotate=1", "")
	rtcPollTrue(t, env.a, `window.__rtc.chanOpen.some((c) => c.startsWith('bravo:chat:'))`, 20*time.Second)
	rtcPollTrue(t, env.b, `window.__rtc.chanOpen.some((c) => c.startsWith('alpha-1:chat:'))`, 20*time.Second)
	rtcFront(t, env.a, chromedp.Evaluate(`window.__pcB = window.__room.peers.get('bravo').pc;`, nil))

	if !env.room.kill("alpha-1") {
		t.Fatal("no alpha-1 socket to kill")
	}
	rtcPollTrue(t, env.a, `!!(window.__room.self && window.__room.self.id === 'alpha-2' && window.__room.status.phase === 'hydrated')`, 20*time.Second)
	rtcPollTrue(t, env.a, `(() => { const p = window.__room.peers.get('bravo'); return !!(p && p.pc && p.pc !== window.__pcB); })()`, 15*time.Second)

	rtcPollTrue(t, env.b, `window.__rtc.chanOpen.some((c) => c.startsWith('alpha-2:chat:'))`, 30*time.Second)
	rtcFront(t, env.b, chromedp.Evaluate(`window.__room.send('chat', 'b2a-rotated')`, nil))
	rtcPollTrue(t, env.a, `window.__rtc.chanMsgs.indexOf('bravo:chat:b2a-rotated') >= 0`, 15*time.Second)
	rtcFront(t, env.a, chromedp.Evaluate(`window.__sent = window.__room.send('chat', 'a2b-rotated')`, nil))
	rtcPollTrue(t, env.b, `window.__rtc.chanMsgs.indexOf('alpha-2:chat:a2b-rotated') >= 0`, 15*time.Second)
}
