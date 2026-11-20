# Real-time audio/video signaling (`battery/rtc`, experimental)

> **Experimental:** `battery/rtc` provides WebRTC signaling (rooms, SDP offer/answer exchange, ICE candidate relay, and peer presence) on top of GoFastr's existing HTTP and SSE primitives. Media flows peer-to-peer or through your own TURN/SFU server.

## How it works

WebRTC browsers need a signaling channel to exchange SDP offers/answers and ICE candidates before media flows directly between peers:
- **Signaling writes** (join room, send offer/answer, send ICE candidate, leave) are normal authenticated HTTP POST requests.
- **Signaling notifications** (peer joined/left, incoming offer/answer/ICE) push to room participants over GoFastr's `/__gofastr/sse` bus (and cross replicas via `framework.WithFanout`).
- **TURN credentials:** generate short-lived HMAC-signed TURN credentials server-side so peers behind symmetric NATs can connect without exposing static TURN passwords.

## Security rules

- **Room authorization:** verify the authenticated user is allowed in the requested room on every join and signaling POST — never trust a client-supplied room ID alone.
- **Peer identity from session:** derive the sender's peer ID from the authenticated user/session on the server so one participant cannot spoof another peer's SDP offers or kick other peers.
- **Size and rate bounds:** bound SDP and ICE candidate payload sizes and rate-limit signaling endpoints to prevent room flooding.

## Common mistakes

- **Expecting GoFastr to act as a media SFU:** `battery/rtc` handles signaling only. For large group calls beyond small peer-to-peer meshes, point clients at LiveKit, Pion SFU, or a TURN server.
- **Shipping static TURN passwords to the browser:** always mint short-lived time-limited TURN credentials per session.
- **Skipping `framework.WithFanout` in a multi-replica deployment:** peers connected to different replicas won't receive each other's offers/ICE candidates without shared fanout.
- **Using WebRTC signaling for ordinary live dashboards:** stick to `data-cui-poll` or `/__gofastr/sse` island push (see [Reactivity](reactivity.md)).
