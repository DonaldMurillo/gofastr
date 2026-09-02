package pluginhost

import (
	_ "embed"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// FrameClientScriptURL is the platform route serving the frame-side channel
// client (frame/frameclient.js), the mirror of the host broker. Plugin frame
// documents hot-link it to get the same envelope, source check, and
// request/response semantics as the host side without hand-rolling a
// postMessage RPC per plugin.
const FrameClientScriptURL = "/__gofastr/plugin/frame/frameclient.js"

//go:embed frame/frameclient.js
var frameClientJSBytes []byte

// FrameClientRouteMethod is the router method the frame client route is
// registered under.
const FrameClientRouteMethod = "GET"

// RegisterFrameClientRoute serves the frame client at [FrameClientScriptURL]
// on the given router. Like [RegisterBrokerRoute] it is IDEMPOTENT: multiple
// plugins may call it from their Init and only the first registration lands.
//
// Unlike the broker (a HOST-page script served same-origin, framed=false),
// this script is fetched BY opaque-origin frame documents: the global
// security middleware's Cross-Origin-Resource-Policy: same-origin would
// refuse the "null"-origin frame's <script src>, so it is served with
// framed=true to apply exactly that CORP relaxation. The framedCSP header
// that writeAsset also emits is inert on a script response (CSP governs
// documents, not script bytes) — harmless there; the CORP relaxation is the
// point. The platform's own route serves no per-plugin manifest, so it
// passes no CSP extensions: the default framed policy.
func RegisterFrameClientRoute(rt *router.Router) {
	if routeRegistered(rt, FrameClientRouteMethod, FrameClientScriptURL) {
		return
	}
	rt.Get(FrameClientScriptURL, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeAsset(w, r, frameClientJSBytes, "text/javascript; charset=utf-8", responsePolicy{framed: true})
	}))
}

// FrameClientJS returns a copy of the embedded frame client, for plugins
// that bundle the script into their own frame document instead of
// hot-linking [FrameClientScriptURL].
func FrameClientJS() []byte {
	b := make([]byte, len(frameClientJSBytes))
	copy(b, frameClientJSBytes)
	return b
}
