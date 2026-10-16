//go:build darwin || linux

package service

import "testing"

// The uid comparison behind peerIsSelf: only the daemon's own uid
// passes, root included in the refusals.
func TestCheckPeerUID(t *testing.T) {
	if err := checkPeerUID(501, 501); err != nil {
		t.Fatalf("own uid refused: %v", err)
	}
	cases := []struct {
		peer uint32
		self int
	}{
		{502, 501},
		{0, 501},
		{501, 0},
		{1<<32 - 1, -1},
	}
	for _, c := range cases {
		if err := checkPeerUID(c.peer, c.self); err == nil {
			t.Errorf("checkPeerUID(%d, %d) accepted a foreign peer", c.peer, c.self)
		}
	}
}
