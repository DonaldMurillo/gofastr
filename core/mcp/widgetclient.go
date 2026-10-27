package mcp

import (
	_ "embed"
)

// widgetClientJSBytes is the MCP Apps widget client (widgetclient.js): the
// ext-apps handshake, JSON-RPC envelope, source check, and request/response
// semantics a widget would otherwise hand-roll as a postMessage RPC.
// WidgetDocument inlines it into every document it builds.
//
//go:embed widgetclient.js
var widgetClientJSBytes []byte

// WidgetClientJS returns a copy of the embedded widget client, for authors
// who write their own widget HTML instead of using [WidgetDocument]. Inline
// it in a <script> element ahead of the widget's own code. Do not serve it
// from a URL and link it: the chat host renders the document on its own
// origin, so a path on this server never resolves from inside the widget.
func WidgetClientJS() []byte {
	b := make([]byte, len(widgetClientJSBytes))
	copy(b, widgetClientJSBytes)
	return b
}
