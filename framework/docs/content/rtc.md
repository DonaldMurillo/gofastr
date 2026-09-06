# WebRTC rooms

`battery/rtc` is a signaling server for browser-to-browser
WebRTC: rooms, peers, and an addressed relay for SDP offers, answers, and
ICE candidates, carried over a `core/stream.StateChannel` WebSocket. It
also hands each peer its ICE server list, minting per-peer time-limited
TURN credentials when you configure a TURN server. Media never crosses
the Go process: audio and video flow peer to peer between browsers.

The browser half is the `rtc` runtime module,
`__gofastr.connectRoom(url, opts)`, which runs one `RTCPeerConnection`
per remote peer with perfect negotiation and reconnects with the same
generation model as `__gofastr.connectWebSocket`.

It is a battery: construct it with `rtc.New`, register it with
`app.RegisterPlugin`, and it mounts its endpoint, takes the app's fanout,
and closes with the app.

## What this is not

- **No server-side media.** No SFU, no MCU, no forwarding, no recording.
  Everyone connects to everyone: a full mesh. `MaxPeers` defaults to 8
  because mesh cost is O(n²) connections; an SFU is a different product.
- **No SDP parsing.** The server relays signaling JSON between peers. It
  never reads, stores, or logs an SDP body or an ICE candidate.
- **No client identity.** A browser names nothing about itself; see the
  security model below.

A media server, if you ever want one, would be another peer that speaks
this same protocol.

## The security model (read this first)

**Identity is server-derived. Never client-supplied.**

Every peer's room, role, id, user, and display name come from the
`rtc.Join` your `Config.Authorize` returns. `Authorize` reads cookies,
sessions, or `handler.GetUser(r.Context())`, whatever you already trust,
and its return value is trusted as-is. A client can trigger a join; it
cannot shape one. There is no `?user=` or `?name=` the wire accepts.
`Join.Room` is an authorization decision, not a label: an `Authorize`
that copies `?room=` lets any signed-in user name any room, so check
membership there when rooms are private. `Join.PeerID` left empty
mints an unguessable id per socket, which is right for a user with
several tabs; a signaling reconnect then lands under a new id, and the
browser module rebuilds every connection (each remote saw the old id
leave). Set a stable per-tab `PeerID` only when you can mint one the
client cannot forge, and a reconnect becomes a rejoin that keeps its
connected peer connections.

**Refusals reach the browser.** An error from `Authorize`, a bad
`Join`, a full room, or a closed signaler keeps its HTTP status for a
plain request (curl, a Go peer). A WebSocket upgrade request is
accepted and closed with code 4000+status (`4409` room full, `4403`
forbidden, the status of an `*rtc.HTTPError`), because a browser
cannot read the status of a failed handshake: the WHATWG spec withholds
it so a page cannot probe the network. The `ws` runtime module classes
a 4xx code (4400 to 4499) as `refused`, keeps the status on
`status.refused`, and does not reconnect, so a signed-out or over-cap
tab shows a message instead of re-dialing every 30 s for ever. A 5xx
code (an `*rtc.HTTPError` with status 503, "not now") is an ordinary
transport error and is retried with the usual backoff.

**The TURN secret never leaves the server.** With
`Config.TURN` set, the package mints credentials in the coturn
`static-auth-secret` convention (the TURN REST API): username is
`"<unix-expiry>:<peer id>"`, credential is
`base64(HMAC-SHA1(secret, username))`. The secret stays in the Go
process. Each peer receives only its own credential, inside its own
snapshot; peer A's credential is never marshaled for anyone but A.
`TURN.TTL` bounds the lifetime (default 1h, minimum 1m), so a leaked
credential expires on its own. Expiry does not disturb a relay
allocation the peer already holds; it refuses new ones, and an ICE
restart after a network change needs new ones. So every half TTL the
server pushes a freshly minted credential to each peer (the
`iceServers` event) and the browser module applies it to its live
connections with `setConfiguration`, so a call that outlives the TTL
still recovers from a network change.

**Signals are relayed, not parsed.** The `data` of a `signal` frame is
size-checked (`MaxSignalBytes`, default 48 KiB; a one-camera offer is
3 to 8 KiB, a many-codec simulcast offer 15 to 25 KiB) and forwarded to the
named recipient only. The server never decodes SDP or candidates.

**Sockets are bounded.** Inbound frames are rate-limited
(`MaxFramesPerSecond`, default 128, token bucket); over the limit the
socket closes. Outbound, a peer whose send buffer overflows
(`WS.SendBuffer`, default 128 frames) is closed rather than starved:
signals are not in the snapshot, so a dropped answer could never be
recovered, while a closed socket reconnects, re-hydrates, and the far
side renegotiates. A `signal`'s `to` must be a current member of the room
and not the sender. A `status` document must be a JSON object within
`MaxStatusBytes` (default 1 KiB). Rooms cap at `MaxPeers` members
(default 8): a join past the cap is refused with 409 before the upgrade,
and the cap is checked again under the lock that registers the peer, so
two joins racing for the last seat end with one member and one closed
socket, never an oversize room.

**Nothing sensitive is logged.** Joins and leaves log at Debug, refusals
at Warn with a bounded reason class. Never logged: SDP, candidates,
status bodies, credentials, close reasons.

## Wiring

```go
import (
	"net/http"
	"os"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/battery/rtc"
	"github.com/DonaldMurillo/gofastr/core/handler"
)

sig := rtc.New(rtc.Config{
	ICEServers: []rtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}},
	TURN: &rtc.TURN{URLs: []string{"turn:turn.example.com:3478"}, Secret: os.Getenv("TURN_SECRET")},
	Authorize: func(r *http.Request) (rtc.Join, error) {
		u, ok := handler.GetUser(r.Context())
		if !ok {
			return rtc.Join{}, &rtc.HTTPError{Status: http.StatusUnauthorized, Message: "sign in first"}
		}
		user := u.(auth.User)
		return rtc.Join{Room: r.URL.Query().Get("room"), User: user.GetID(), DisplayName: user.GetEmail()}, nil
	},
})
app.RegisterPlugin(sig) // GET /__gofastr/rtc?room=…; Init mounts it, wires the app's fanout, closes it on stop
```

`rtc.New` panics on an invalid Config with a message prefixed `rtc:`,
the same posture as `relay.New`: a bad ICE URL scheme, a negative
limit, or a payload cap the socket cannot carry is a construction-time
programmer error, not a runtime
condition to limp along with.

`Authorize` is required for `Init`, `Mount`, and `ServeHTTP` (a mounted
endpoint with no gate is a bug, so `Mount` panics on a nil hook). A
returned error refuses the upgrade with 403, or the status carried by a
`*rtc.HTTPError` (401, 404, 410 …). Apps that own their route call
`sig.Serve(w, r, join)` with an already-trusted `Join` and may leave
`Authorize` nil; `sig.Mount(router)` registers the endpoint without the
plugin lifecycle.

`rtc.Join` fields: `Room` (required, 1-128 bytes printable ASCII, no
whitespace), `Role` and `User` and `DisplayName` (optional labels the
room sees), `PeerID` (optional stable id; empty means the server mints
16 hex bytes per socket). Reconnecting with a `PeerID` that already
exists in the room closes the old socket and replaces it, so a
reconnecting tab reclaims its seat.

## The Go API beyond Config

```go
sig.Peers(room)          // merged local + live-remote roster, sorted by Order then ID
sig.Rooms()              // rooms with at least one local member
sig.Kick(room, peerID)   // close one local socket (a nudge, not a ban: see below); reports whether found on this replica
sig.Close()              // stop everything; safe to call twice
sig.SetFanout(f)         // wired for you by Init (or App.Mount) under WithFanout
rtc.TURN.Credentials(peerID, now) // the per-peer ICEServer entry, exported for non-browser peers
```

## The Permissions-Policy helper

The framework's default headers close `camera=()` and `microphone=()`,
so `getUserMedia` fails until you open them. `rtc.PermissionsPolicy`
writes every feature it knows, closed unless named, and opens the
named ones to your own origin:

```go
// "geolocation=(), microphone=(self), camera=(self), display-capture=()"
middleware.SecurityHeadersConfig{PermissionsPolicy: rtc.PermissionsPolicy(rtc.Camera, rtc.Microphone)}
```

An omitted directive is not a closed one: the default allowlist for
`camera`, `microphone`, and `display-capture` is `self`, so a header
that only listed what it opened would open what it left out. That is
why the helper always writes all four. Open the least the feature
needs: a listen-only viewer surface names `rtc.Microphone` alone, and
a page that shares a screen names `rtc.DisplayCapture`, without which
`getDisplayMedia` is refused.

## The wire protocol

Server to client, every message is `{"type":T,"sequence":N,"payload":P}`
(a StateChannel envelope):

| type | payload | delivered to |
|---|---|---|
| `snapshot` | `{"room":"r","self":PeerInfo,"peers":[PeerInfo…],"iceServers":[ICEServer…]}` | the connecting peer, once per connect |
| `join` | PeerInfo | every peer except the subject |
| `leave` | `{"id":"p"}` | every peer except the subject |
| `status` | `{"id":"p","status":{…}}` | every peer except the subject |
| `signal` | `{"from":"p1","to":"p2","type":"offer"\|"answer"\|"ice","data":{…}}` | `to` only |
| `iceServers` | `{"iceServers":[ICEServer…]}` | every peer, each with its own fresh TURN credential, every half `TURN.TTL` while TURN is configured |

`PeerInfo` is `{id, role, user, name, order, status}`. `order` is the
join instant in Unix nanoseconds.

Client to server:

| kind | shape | rule |
|---|---|---|
| `signal` | `{"kind":"signal","to":"p2","type":"offer","data":{"sdp":…}}` or `{"type":"ice","data":{"candidate":…}}` | `to` is a current member, not self; data ≤ MaxSignalBytes; the server never parses `data` |
| `status` | `{"kind":"status","data":{…}}` | JSON object ≤ MaxStatusBytes; stored on the peer and broadcast |

A refused join on an upgrade request is a completed handshake followed
by a close frame with code 4000+status and a short reason; see
"Refusals reach the browser" above.

**Politeness** (both sides derive it, it is never sent): for the pair
(A, B), the peer with the greater `order` is polite; on a tie, the
greater id (string compare) is polite. The polite peer yields in an SDP
collision, exactly as the W3C perfect-negotiation pattern defines.

## The browser module

```js
await __gofastr.loadModule('rtc');
const room = __gofastr.connectRoom('/__gofastr/rtc?room=standup', {
  channels: ['chat'],
  onTrack(peer, ev) { videoFor(peer.id).srcObject = ev.streams[0]; },
  onMessage(peer, ch, ev) { appendChat(peer.name, ev.data); },
  onPeerLeave(peer) { videoFor(peer.id).remove(); },
});
const media = await navigator.mediaDevices.getUserMedia({ video: true, audio: true });
media.getTracks().forEach((t) => room.addTrack(t, media));
room.send('chat', 'hello');
```

The module is demand-loaded, has no DOM marker, and loads `ws` itself.
The browser APIs it relies on (implicit rollback, argument-free
`setLocalDescription`, negotiated channels, `restartIce`,
`setConfiguration`) are in current Chrome, Safari, and both Firefox
ESR lines; this repo's suites drive Chromium only.
It runs one `RTCPeerConnection` per remote peer with perfect
negotiation (the polite/impolite rule above), trickle ICE, and
negotiated data channels: each entry in `channels` becomes a channel
with `negotiated: true` and id = its index, so both sides create it and
no `ondatachannel` is needed. A string entry is a reliable, ordered
channel by that label. An object entry `{label, ...RTCDataChannelInit}`
sets the rest: `{label: 'pose', ordered: false, maxRetransmits: 0}` is
the shape for telemetry that must never queue behind a stale frame
(check `channel.bufferedAmount` before each send and drop the frame
when it is backed up; the module does not).

Declare at least one data channel even if the product is video only.
The first negotiation of a pair happens when one side has something to
negotiate; with a channel, that is the moment the older peer sees the
`join`, so a track added later renegotiates at once. With no channel
and no track on the older side, the newer peer waits out a 1500 ms
fallback before it offers.

The hooks:

| Hook | Fires when |
|---|---|
| `onSnapshot({room, self, peers})` | after each hydration |
| `onPeer(peer)` / `onPeerLeave(peer)` | a remote peer appears / is gone |
| `onTrack(peer, ev)` | an `RTCTrackEvent` on a peer's connection |
| `onChannel(peer, ch)` / `onMessage(peer, ch, ev)` | a data channel opened / received on one |
| `onPeerState(peer, state)` | the peer's `connectionState` changed |
| `onStatus(peer, status)` | the peer's status document changed |
| `onPhase(phase)` | `connecting` → `open` → `hydrated` → `closed:<class>`, the same classes as the `ws` module; `closed:refused` is final, with the status on `room.status.refused` |

A peer object is `{id, role, name, user, order, status, pc, polite,
channels, state}`. The room handle adds:

| Member | What it does |
|---|---|
| `room.self`, `room.peers` | your `PeerInfo` (null before hydration), a `Map` of peers |
| `room.status` | `{generation, phase, peers, refused}`, the live status object |
| `room.addTrack(track, stream)` | add to every current and future peer connection |
| `room.removeTrack(track)` | remove from every peer connection |
| `room.replaceTrack(oldTrack, newTrack)` | swap a sender's track on every connection with no renegotiation (camera switch, screen share); future connections get the new one; `null` stops sending and forgets the track |
| `room.send(label, data)` | to every open channel with that label; strings and binary (`ArrayBuffer`, a typed array, `Blob`) pass through, anything else is JSON-encoded; returns the count sent. Channels are pinned to `binaryType = 'arraybuffer'`, so `onMessage` sees one shape on every browser |
| `room.setStatus(obj)` | publish your status; `room.self.status` updates at once, and it is resent after each reconnect |
| `room.close()` | leave |

**Reconnects.** The signaling WebSocket reconnects with a new
generation, like every `ws` consumer. On the snapshot after a
reconnect, peers that are gone close (`onPeerLeave`), peers still
`connected` or `connecting` keep their peer connection (no
renegotiation storm), anything else is torn down and rebuilt with the
tracks and channels re-added, and the remembered status is resent. The
other side saw you leave and rejoin and rebuilt its connection, so its
next offer restarts ICE and DTLS on your kept one; media carries on,
but the old data channel objects close with the old transport, and the
module recreates them on the live connection. A status a peer already
carried when you met it (in a snapshot or a join) arrives through
`onStatus` too, so a late joiner or a reloaded page sees "muted"
without waiting for the next toggle. The ICE list a snapshot or an
`iceServers` event carries is applied to every live connection, so a
kept connection restarts ICE with a credential that has not expired.
The module never logs; no SDP,
candidate, credential, or close reason reaches the console or a
status object.

**Collisions.** Both sides can offer at once. The newer peer of a pair
is polite and does not make the first offer at all; it waits for the
older peer, with a 1500 ms fallback that fires only while nothing has
arrived. If two first offers still cross, the polite side discards its
never-negotiated connection and answers from a fresh one rather than
rolling back: a first-offer rollback leaves Chrome offering forever
without the data channel. Mid-call collisions roll back as the W3C
pattern says. More than twelve offers in five seconds on one
connection is treated as a loop and the connection is rebuilt.

## Multiple replicas

With `framework.WithFanout` attached (wired automatically by
`App.Mount`), rooms span replicas on the `gofastr.rtc` fanout topic,
the same lossy self-healing model presence uses: each replica
broadcasts its full local roster per room on every join and leave and
on a 15 s heartbeat, and mirrors a status change as one event (the
heartbeat is the backstop for a mirror a replica missed); receivers
keep a TTL'd (45 s) per-(replica, room) table capped at 512 replicas
per room. A beat naming more than `MaxPeers` members is dropped whole,
and a beat cannot restate a peer this replica holds (local wins on the
join mirror as on the leave mirror). A room's local state is dropped
`RoomIdleTTL` (default 1m) after its last local peer leaves, on its own
timer, whether the last peer left or migrated to another replica; what
other replicas hold for that name stays under the remote TTL. Signals
addressed to a peer on another replica are published on the fanout and
delivered by the owning replica. A crashed replica's peers vanish from
rosters within the TTL; `Close` publishes empty rosters first so a
rolling restart converges promptly. Without a fanout, nothing runs and
behaviour is byte-for-byte the single-replica result.

## Deployment

- **HTTPS/WSS.** `getUserMedia` is a secure-context API. Browsers
  refuse it on plain HTTP except localhost.
- **STUN** gets you most direct connections: one public STUN server in
  `Config.ICEServers` is the minimum.
- **TURN** relays for peers behind symmetric NAT or hard firewalls.
  Run coturn with `use-auth-secret` and `static-auth-secret=<secret>` (the first turns the REST-API credential mode on, the second supplies the value; without the flag every minted credential is refused) and pass the same
  secret in `Config.TURN.Secret`; the package mints the per-peer
  credentials coturn expects. A TURN server relays media: it costs
  bandwidth, and it is the only place your media touches a server you
  run, but the Go process still never sees it.
- **Mesh limits.** `MaxPeers` (default 8) caps a room. Video in a full
  mesh means each browser uploads one stream per remote peer; past a
  handful of participants you want an SFU, which this package is not.

## Knowing whether a call went through TURN

The server never sees media, so it cannot tell a direct path from a
relayed one. The browser can: `pc.getStats()` reports the selected
candidate pair, and a `relay` candidate on either end means TURN. Put
that on the status document and it reaches the other peers and the
server's roster:

```js
async function pathOf(pc) {
  const report = await pc.getStats();
  let pair;
  report.forEach((s) => { if (s.type === 'transport' && s.selectedCandidatePairId) pair = report.get(s.selectedCandidatePairId); });
  // Firefox before 153 (both live ESR lines) has no 'transport' stat;
  // the selected pair is flagged on the pair itself there.
  if (!pair) report.forEach((s) => { if (s.type === 'candidate-pair' && (s.selected || (s.nominated && s.state === 'succeeded'))) pair = s; });
  if (!pair) return null;
  const local = report.get(pair.localCandidateId), remote = report.get(pair.remoteCandidateId);
  return { relay: local?.candidateType === 'relay' || remote?.candidateType === 'relay' };
}
// in onPeerState, when state === 'connected':
pathOf(peer.pc).then((p) => p && room.setStatus({ path: p }));
```

Server side, `sig.Peers(room)` returns each member's current status,
so an MCP tool or a support console can answer "is the operator on a
relay" from backend state without the browser being asked. It is the
browser's own report, not something the server verified: fine for a
support view, never an input to an authorization or billing decision.
Keep the document to allow-listed values; it is a status, not a log.

## See also

- [Reactivity model](reactivity.md) places WebSockets and signaling in
  the delivery ladder.
- [Core packages](core-packages.md) documents `core/stream.StateChannel`,
  the transport under this package.
- [Horizontal scaling](scaling.md) covers the fanout seam.
- `examples/webmcp-remote-assist` is the hand-rolled single-session
  reference this package generalizes.

## Common mistakes

- **Leaving `Authorize` for later.** `Mount` panics without it, on
  purpose: an ungated signaling endpoint lets any visitor join any
  room name and spend your TURN relay budget.
- **Forgetting the Permissions-Policy.** The default headers close
  camera and microphone; `getUserMedia` fails with a console error
  nobody connects to the header. Use `rtc.PermissionsPolicy` and open
  only what the page needs.
- **Saying nothing when the camera is refused.** A denied permission,
  a machine with no camera, or a device another app holds all reject
  `getUserMedia`, and a page that only records the error name leaves
  the user with a button that did nothing. Render a notice the script
  fills from the rejection (the `rtc-call` example's `#call-notice`),
  and do the same for `closed:refused`.
- **Expecting the server to see media.** It cannot help with quality,
  record a call, or mute a participant's audio; those are peer-side
  (`room.removeTrack`) or TURN-side concerns.
- **Assuming remote audio autoplays.** A remote stream with an audio
  track plays only after a user gesture on that page; a viewer who
  never clicked gets a frozen first frame and a rejected `play()`.
  Call `video.play()` from a click handler, or mute the element when
  the product is video only (iOS Safari was where this bit first).
- **Treating `Kick` as a ban.** It closes one socket on this replica.
  The browser module reconnects with backoff, so a peer that must stay
  out is refused by `Authorize` on that reconnect, and the TURN
  credential it already holds keeps working until `TURN.TTL` runs out;
  set the TTL with that window in mind.
- **Reading status documents as private.** `setStatus` is delivered to
  every peer in the room and stored on the roster the server hands out.
  It is the right place for "muted", "sharing screen", or "on a relay",
  and the wrong place for anything one role must not see; that state
  belongs on your own `StateChannel` with its own `FilterEvent`.
- **Raising `MaxSignalBytes` past the socket.** The socket read limit
  is 64 KiB at most, and `New` refuses a payload cap that would not
  fit it plus a kilobyte of envelope, because the frame would be cut
  off before the cap was ever consulted. The default already leaves
  headroom over the largest offers browsers produce.
- **Putting 30 people in a room.** Mesh cost is O(n²) connections and
  each browser uploads one stream per peer. Keep rooms small; split
  viewers into listen-only rooms or reach for an SFU.
