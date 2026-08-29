package mcp

import (
	_ "embed"
	"net/http"
)

// WidgetClientScriptURL is the path serving the MCP Apps widget client
// (widgetclient.js). Widget documents hot-link it to get the ext-apps
// handshake, JSON-RPC envelope, source check, and request/response
// semantics without hand-rolling a postMessage RPC per app.
const WidgetClientScriptURL = "/__gofastr/mcp/app/widgetclient.js"

//go:embed widgetclient.js
var widgetClientJSBytes []byte

// WidgetClientHandler returns the handler serving the widget client at
// [WidgetClientScriptURL]. Mount it wherever the HTTP surface is assembled:
//
//	rt.Get(mcp.WidgetClientScriptURL, mcp.WidgetClientHandler())
//
// It hands back a handler rather than taking a router for the same reason
// [Server.ServeSSE] does: the protocol package stays decoupled from any
// particular router.
//
// Like pluginhost's frame client, this script is fetched BY opaque-origin
// frame documents (the host renders the widget in a sandboxed iframe), so it
// carries the Cross-Origin-Resource-Policy relaxation: the same-origin
// default would refuse the "null"-origin frame's <script src>. See
// writeWidgetClientAsset for why that is the only framing header it sets.
func WidgetClientHandler() http.Handler {
	return http.HandlerFunc(writeWidgetClientAsset)
}

// writeWidgetClientAsset emits the client with the framing headers a script
// fetched by an opaque-origin iframe needs. X-Frame-Options and CSP
// frame-ancestors govern document embedding, not script bytes, so they are
// deliberately not touched here; Cross-Origin-Resource-Policy is the one
// that gates the frame's cross-origin <script src>, and it must be
// "cross-origin" or the sandboxed widget can never load its client.
func writeWidgetClientAsset(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "text/javascript; charset=utf-8")
	// Never let a browser MIME-sniff this into a more dangerous type.
	h.Set("X-Content-Type-Options", "nosniff")
	// The URL is unversioned and the bytes carry no validator; no-store
	// forces a fresh fetch per widget load instead of a stale copy across
	// server upgrades. (A prod build would content-hash the URL instead.)
	h.Set("Cache-Control", "no-store, max-age=0")
	h.Set("Cross-Origin-Resource-Policy", "cross-origin")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(widgetClientJSBytes)
}

// WidgetClientJS returns a copy of the embedded widget client, for authors
// who fold the script into their own widget HTML instead of hot-linking
// [WidgetClientScriptURL].
func WidgetClientJS() []byte {
	b := make([]byte, len(widgetClientJSBytes))
	copy(b, widgetClientJSBytes)
	return b
}
