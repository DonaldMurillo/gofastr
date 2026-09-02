package stream

import (
	"net/http"
	"testing"
	"time"
)

// The StateChannel work (issues #375 / #377) adds helpers ABOVE the
// low-level Hub and WebSocket primitives without changing them. These
// pins are compile-time assertions over every exported symbol of both
// APIs: a signature change, a rename, or a removal fails the build
// here first, before any caller downstream notices.
//
// Byte-compatibility has two halves: the signatures below, and the
// existing behavior suites (hub_test.go, hub_hardening_test.go,
// websocket_*_test.go) which pin the semantics. Additions to either
// API are fine; anything captured here is not allowed to move.

// --- Hub API pins ---

var (
	_ func() *Hub                = NewHub
	_ func(*Hub)                 = (*Hub).Run
	_ func(*Hub, *WebSocketConn) = (*Hub).Register
	_ func(*Hub, *WebSocketConn) = (*Hub).Unregister
	_ func(*Hub, []byte)         = (*Hub).Broadcast
	_ func(*Hub, []byte)         = (*Hub).BroadcastWait
	_ func(*Hub)                 = (*Hub).Stop
	_ func(*Hub) int             = (*Hub).Count
)

// --- WebSocket API pins ---

var (
	_ func(http.ResponseWriter, *http.Request, WSConfig) (*WebSocketConn, error) = Upgrade
	_ func(*WebSocketConn, []byte) error                                         = (*WebSocketConn).Write
	_ func(*WebSocketConn, string) error                                         = (*WebSocketConn).WriteString
	_ func(*WebSocketConn) ([]byte, error)                                       = (*WebSocketConn).Read
	_ func(*WebSocketConn) error                                                 = (*WebSocketConn).Close
	_ func(*WebSocketConn, func())                                               = (*WebSocketConn).OnClose
	_ func(*WebSocketConn) <-chan struct{}                                       = (*WebSocketConn).Closed
)

// wsConfigShape pins WSConfig's exported field names and types via a
// fully keyed composite literal: removing or renaming a field fails to
// compile, and a type change fails the assignment.
var _ = WSConfig{
	ReadLimit:       int64(0),
	SendBuffer:      0,
	WriteTimeout:    time.Duration(0),
	CheckOrigin:     func(*http.Request) bool { return true },
	ReadIdleTimeout: time.Duration(0),
	PongTimeout:     time.Duration(0),
	CloseTimeout:    time.Duration(0),
	Subprotocols:    []string(nil),
	OnClose:         func() {},
}

// TestHubAndWebSocketAPIPinned keeps the pins referenced by the test
// binary (the var blocks above are the actual gate; this test exists so
// the pins run under -run and show up as a named result).
func TestHubAndWebSocketAPIPinned(t *testing.T) {
	if NewHub() == nil {
		t.Fatal("NewHub returned nil")
	}
}
