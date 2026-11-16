package sessiontoken

import (
	"strconv"
	"testing"
)

// A token correctly signed under a 15-byte key still fails Verify.
func TestSweepAssetsVerifyRefusesShortKey(t *testing.T) {
	short := []byte("123456789012345")
	created := strconv.FormatInt(now.Unix(), 10)
	tok := "sess-abc." + created + "." + sign(short, "sess-abc", created)
	if _, ok := Verify(short, tok, now, maxAge); ok {
		t.Fatal("Verify accepted a token signed and checked under a 15-byte key")
	}
}
