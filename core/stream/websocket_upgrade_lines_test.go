package stream

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCheckUpgradeReadsEveryUpgradeLine: the handshake read
// Header.Get("Upgrade"), the first field line only, so a client listing
// "h2c" then "websocket" on separate lines (RFC 9110 §5.3) was refused
// as not an upgrade. df472714 fixed the same shape for Connection.
func TestCheckUpgradeReadsEveryUpgradeLine(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Header["Upgrade"] = []string{"h2c", "websocket"}
	r.Header.Set("Connection", "Upgrade")
	r.Header.Set("Sec-WebSocket-Version", "13")
	r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	if err := CheckUpgradeRequest(r, func(*http.Request) bool { return true }); err != nil {
		t.Fatalf("upgrade with websocket on the second Upgrade line refused: %v", err)
	}
}
