package mcp

import (
	_ "embed"
)

//go:embed widgetclient.js
var widgetClientJSBytes []byte

// WidgetClientJS returns a copy of the embedded widget client, to inline in a <script> element of a hand-written widget.
func WidgetClientJS() []byte {
	b := make([]byte, len(widgetClientJSBytes))
	copy(b, widgetClientJSBytes)
	return b
}
