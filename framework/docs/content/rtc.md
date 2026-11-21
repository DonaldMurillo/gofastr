# Real-time audio/video signaling (`battery/rtc`, experimental)

> **Experimental:** `battery/rtc` provides WebRTC signaling (rooms, SDP offer/answer exchange, ICE candidate relay, and peer presence) over WebSockets. Media flows peer-to-peer or through your own TURN/SFU server.

## How it works

WebRTC browsers exchange SDP offers/answers and ICE candidates over a WebSocket endpoint registered by `battery/rtc` (default `GET /__gofastr/rtc`):

```go
rooms := rtc.New(rtc.Config{
	Path:     "/__gofastr/rtc", // default
	MaxPeers: 8,                // default mesh cap
	Authorize: func(r *http.Request) (rtc.Join, error) {
		user := auth.GetCurrentUser(r.Context())
		if user == nil {
			return rtc.Join{}, errors.New("authentication required")
		}
		return rtc.Join{Room: r.URL.Query().Get("room"), User: user.GetID()}, nil
	},
})
```

- **Bidirectional WebSocket frames:** both outbound signaling (join, offer/answer, ICE candidate) and inbound notifications (peer joined/left, incoming signals) flow as WebSocket frames on `GET /__gofastr/rtc` (not HTTP POST + SSE). Across multiple replicas, pair with `framework.WithFanout`.
- **Mandatory `Config.Authorize`:** `Mount` and `ServeHTTP` require `Config.Authorize` to be set before mounting; returning an error refuses the WebSocket upgrade (403 or `*HTTPError` status), and authorization is rechecked on the live socket so revoked access terminates cleanly.
- **TURN credentials:** configure `Config.TURN` to append per-peer time-limited HMAC TURN credentials so peers behind symmetric NATs can connect without exposing static TURN passwords.

## Security rules

- **Room authorization:** verify the authenticated user is allowed in the requested room inside `Config.Authorize` — never trust a client-supplied room ID alone.
- **Peer identity from session:** derive `rtc.Join.User` / `PeerID` from the authenticated user/session on the server so one participant cannot spoof another peer's SDP offers.
- **Size and peer bounds:** `MaxPeers` defaults to `8` (409 on overflow) and `MaxSignalBytes` defaults to `48 KiB` (oversized frames are dropped and the peer closed).

## Common mistakes

- **Mounting `battery/rtc` without setting `Config.Authorize`:** `Mount` and `ServeHTTP` require `Authorize` to decide room membership and peer identity.
- **Expecting GoFastr to act as a media SFU:** `battery/rtc` handles signaling only. For large group calls beyond small peer-to-peer meshes, point clients at LiveKit, Pion SFU, or a TURN server.
- **Shipping static TURN passwords to the browser:** always use `Config.TURN` to mint short-lived time-limited TURN credentials per peer.
- **Skipping `framework.WithFanout` in a multi-replica deployment:** peers connected to different replicas won't receive each other's offers/ICE candidates without shared fanout.
- **Using WebRTC signaling for ordinary live dashboards:** stick to `data-cui-poll` or `/__gofastr/sse` island push (see [Reactivity](reactivity.md)).
