package service

import "testing"

// The SID comparison behind the Windows peerIsSelf: only the daemon's
// own SID passes. SYSTEM and an elevated admin are other users too.
func TestCheckPeerSID(t *testing.T) {
	self := "S-1-5-21-1004336348-1177238915-682003330-1001"
	if err := checkPeerSID(self, self, 7); err != nil {
		t.Fatalf("own SID refused: %v", err)
	}
	for _, peer := range []string{
		"S-1-5-21-1004336348-1177238915-682003330-1002",
		"S-1-5-18",
		"S-1-5-32-544",
		"",
	} {
		if err := checkPeerSID(peer, self, 7); err == nil {
			t.Errorf("checkPeerSID(%q) accepted a foreign peer", peer)
		}
	}
	if err := checkPeerSID("", "", 7); err == nil {
		t.Error("an unreadable own SID accepted a peer")
	}
}
