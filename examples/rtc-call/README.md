# rtc-call

One Go binary, anonymous users, rooms by name: the dogfood example for
`battery/rtc` and the `rtc` runtime module. Two people who type the
same room name end up in the same peer-to-peer video call, with chat
over a negotiated data channel. No accounts, no invite links, and no
media in the Go process.

This is the reference example for the packaged signaling surface:

- **`battery/rtc`** (`rtc.New` + `app.RegisterPlugin`): rooms, the
  addressed offer/answer/ICE relay, ICE server config with optional
  per-peer TURN credentials. The example writes zero signaling code.
- **The `rtc` runtime module** (`__gofastr.connectRoom`): one
  `RTCPeerConnection` per remote peer, perfect negotiation, trickle
  ICE, the negotiated `chat` channel, reconnect generations.

`examples/webmcp-remote-assist` is the contrast: that example
hand-rolls its relay on `core/stream.StateChannel` to teach the
underlying shape for one fixed pair of roles. When you want rooms of
peers, this example is the shape: register the battery, derive the
`rtc.Join` from your own auth, open the Permissions-Policy.

## Run it

```bash
cd examples/rtc-call && gofastr dev   # or: go run .
```

Open <http://localhost:8091> (the framework's dev isolation may remap
the port; the startup log names the real one), pick a name and a room,
and open the same room in a second browser window. Share the camera in
one; the other sees it.

Environment:

- `CALL_STUN` — STUN URL. Unset means `stun:stun.l.google.com:19302`;
  set to an empty string to disable (both peers on one machine). ICE
  URL schemes are validated at `rtc.New`, which panics on a bad one.
- `CALL_TURN_URL` + `CALL_TURN_SECRET` — when both are set, every peer
  gets its own time-limited TURN credential in the coturn
  `static-auth-secret` convention. The secret never leaves the process.

## The boundaries it exists to show

| Boundary | Where |
|---|---|
| Identity is server-derived | The lobby's form sets one HttpOnly `call_name` cookie. The signaler's `Authorize` reads the cookie and `?room=`; a client names nothing about itself on the wire. |
| The gate before the upgrade | `Authorize` answers 401 without the cookie, 400 on a bad room, before any WebSocket upgrade is attempted. The room page answers the same failures with a redirect to the lobby. |
| The camera is open by header, not hope | `rtc.PermissionsPolicy(rtc.Camera, rtc.Microphone)` keeps the framework's defaults and opens both features to this origin only. |
| Media bypasses the server | `getUserMedia` feeds `room.addTrack`; SDP and ICE cross the signaler as opaque addressed frames; audio and video never do. |
| Every state server-rendered | The room page ships the local tile, one remote-tile `<template>`, a chat-line `<template>`, and both pills of each status pair. `app.js` flips `hidden`, sets `textContent`/`srcObject`, and clones templates. No `innerHTML`, no element creation except `template.content.cloneNode(true)`. |
| Mute is two moves, not one | The audio track's `enabled` flips locally (instant, no renegotiation) and `room.setStatus({muted})` tells the room, which renders the other side's pill. |
| Leaving is a real navigation | Leave is a plain link to the lobby. `app.js` is registered with `RegisterDocumentScript` scoped to `/room`, so the runtime loads the lobby as a real document instead of a partial swap, and that teardown retires the script, the sockets, and the tracks together. No `location.href` anywhere. |
| Chat has no server | Messages travel on the negotiated `chat` data channel. The form's GET action is only the no-script fallback; there is no chat endpoint to attack. |

## What is demo-grade on purpose

- **A name cookie is not authentication.** Anyone can set any name;
  the room gate proves a form was filled, not a person. A real app
  puts its session check inside `Authorize` (`handler.GetUser`, a
  session cookie, whatever it already trusts) and the rest stays.
- **Rooms are names, not secrets.** Anyone who guesses the room name
  joins it; `MaxPeers` (default 8) caps the mesh. Room access control
  is the host's `Authorize`, not the room string.
- **No room list, no presence UI.** The roster lives in the signaler;
  this example renders only what the page itself can prove.

## Deployment notes

- **HTTPS and WSS.** `getUserMedia` is a secure-context API. The
  script builds its WebSocket URL from the page scheme (`wss` under
  HTTPS); terminate TLS at your proxy and forward the upgrade headers
  for `/__gofastr/rtc`.
- **STUN/TURN.** The defaults suit the demo. Real networks need STUN
  at minimum, TURN behind symmetric NAT; see `gofastr docs rtc` for
  the coturn setup the TURN env vars expect.
- **Cookies.** `call_name` is `HttpOnly`, `SameSite=Lax`, `Path=/`
  (the signaler lives under `/__gofastr/`, outside the page tree).
  `Secure` is decided per request from TLS or `X-Forwarded-Proto`, so
  plain-HTTP localhost works and a TLS deployment never sends the
  cookie in clear. The join POST is same-origin checked.
- **Mesh limits.** Everyone connects to everyone: O(n²) connections,
  each browser uploads one stream per peer. Past a handful of
  participants you want an SFU, which this battery is not.

## Tests

```bash
go test ./examples/rtc-call/ -count=1           # HTTP level
go test ./examples/rtc-call/ -count=1 -run TestCallFlow -v  # one Chrome, two tabs
```

The browser suite launches Chromium with fake media flags
(`--use-fake-device-for-media-stream`,
`--use-fake-ui-for-media-stream`), plays Ann and Bob in two tabs of
one browser, and covers hydration, the peer-to-peer camera, chat over
the data channel, mute as a status document, and the leave that
removes the tile. It writes `/tmp/rtc-call-A.png` and
`/tmp/rtc-call-B.png` as evidence for the reviewer.
