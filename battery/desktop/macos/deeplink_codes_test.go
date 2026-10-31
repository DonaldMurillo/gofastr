//go:build darwin && arm64

package macos

import "testing"

// fourCC packs a four-character Apple Event code the way AERegistry.h
// spells it ('GURL' is 0x4755524C), from the string, never a hex literal.
func fourCC(s string) uint32 {
	return uint32(s[0])<<24 | uint32(s[1])<<16 | uint32(s[2])<<8 | uint32(s[3])
}

// The GetURL handler was registered for 'GURT' (0x47555254), so
// LaunchServices never delivered a URL to it. The IMP check in TestMain
// builds its event from the same constants, so only a check against the
// spelled-out codes can catch a typo here.
func TestGetURLCodesSpellGURL(t *testing.T) {
	for name, got := range map[string]uint32{"class": aeGetURLClass, "id": aeGetURLID} {
		if got != fourCC("GURL") {
			t.Errorf("GetURL event %s = %#x, want 'GURL' %#x", name, got, fourCC("GURL"))
		}
	}
	if aeKeyDirect != fourCC("----") {
		t.Errorf("direct-object keyword = %#x, want '----' %#x", aeKeyDirect, fourCC("----"))
	}
}
