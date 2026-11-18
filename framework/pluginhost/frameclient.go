package pluginhost

import (
	_ "embed"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// FrameClientScriptURL is the route serving frame/frameclient.js.
const FrameClientScriptURL = "/__gofastr/plugin/frame/frameclient.js"

//go:embed frame/frameclient.js
var frameClientJSBytes []byte

// FrameClientRouteMethod is the frame client route's method.
const FrameClientRouteMethod = "GET"

// RegisterFrameClientRoute serves the frame client, framed; idempotent.
func RegisterFrameClientRoute(rt *router.Router) {
	if routeRegistered(rt, FrameClientRouteMethod, FrameClientScriptURL) {
		return
	}
	rt.Get(FrameClientScriptURL, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeAsset(w, r, frameClientJSBytes, "text/javascript; charset=utf-8", responsePolicy{framed: true})
	}))
}

// FrameClientJS returns a copy of the embedded frame client.
func FrameClientJS() []byte {
	b := make([]byte, len(frameClientJSBytes))
	copy(b, frameClientJSBytes)
	return b
}
