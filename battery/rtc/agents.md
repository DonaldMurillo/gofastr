# battery/rtc

WebRTC rooms and signaling on `core/stream.StateChannel`: rooms of
peers over one WebSocket per room, addressed offer/answer/ICE relay,
ICE server config with per-peer time-limited TURN credentials, and
cross-replica relay over `core/fanout`. Media never crosses the Go
process; the browser half is the `rtc` runtime module
(`__gofastr.connectRoom`).

**Use this when** the prompt mentions: video call, screen share,
camera, microphone, peer-to-peer data channel, WebRTC, signaling,
STUN, TURN, perfect negotiation, "connect peers directly".

**Import:** `github.com/DonaldMurillo/gofastr/battery/rtc`

**Shape:**
```go
app.RegisterPlugin(rtc.New(rtc.Config{
    ICEServers: []rtc.ICEServer{{URLs: []string{"stun:stun.example.net:3478"}}},
    TURN:       &rtc.TURN{URLs: []string{"turn:turn.example.net:3478"},
                         Secret: os.Getenv("TURN_SECRET")},
    Authorize: func(r *http.Request) (rtc.Join, error) {
        // resolve room + identity from the app's own auth
    },
}))
// Init mounts GET /__gofastr/rtc and closes on app shutdown;
// SetFanout is wired automatically with WithFanout.
```

**Rules that will bite you if ignored:**
- `New` PANICS on an invalid Config (bad Path, non-ICE URLs, negative
  limits). Mount panics and Init errors without `Config.Authorize`:
  a signaling endpoint with no gate is a bug, not a default. Identity
  is server-derived; a client names nothing about itself on the wire.
- The TURN secret never leaves the process; each peer sees only its
  own credential, minted into its own snapshot and re-minted on the
  `iceServers` event every half `TURN.TTL`. Never log SDP,
  candidates, statuses, or credentials.
- `Kick` closes one local socket; the browser reconnects. A ban is
  `Authorize` refusing the reconnect. A refusal on an upgrade request
  is delivered as close code 4000+status (a browser cannot read the
  HTTP status of a failed handshake); the `ws` module retries a 5xx
  and treats a 4xx as final.
- `rtc.PermissionsPolicy` writes every feature, closed unless named:
  name exactly what the page uses (`rtc.Camera`, `rtc.Microphone`,
  `rtc.DisplayCapture`).
- Mesh only: `MaxPeers` (default 8) exists because connections grow
  O(n²). An SFU, recording, or server-side media is a different
  product; a media server would just be another peer speaking this
  protocol.

Full doc: `framework/docs/content/rtc.md` (`gofastr docs rtc`).
