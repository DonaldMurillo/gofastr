package ratelimit

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// A client-chosen key a SQL store cannot hold (invalid UTF-8, a NUL) is
// digested before it reaches the store: Postgres answers such a bind with
// 22021, which the limiter reads as an outage and turns into a 429 for
// that client plus an outage warning. Each such key keeps its own bucket.
func TestStoreKeyUnstorableIsDigested(t *testing.T) {
	var seen []string
	store := recordingStore{allow: true, seen: &seen}
	rl := NewLimiter(Config{MaxAttempts: 5, Window: time.Hour, BlockDuration: time.Hour, Store: store, Scope: "api"})

	for _, key := range []string{"key-\xff", "key-\x00", "key-\xfe", "key-\xff"} {
		rl.Allow(key)
	}
	rl.Allow("ordinary-key")
	for _, k := range seen {
		if !utf8.ValidString(k) || strings.ContainsRune(k, 0) {
			t.Fatalf("store saw unstorable key %q", k)
		}
	}
	if seen[0] != seen[3] {
		t.Errorf("one key digested two ways: %q vs %q", seen[0], seen[3])
	}
	if seen[0] == seen[1] || seen[0] == seen[2] {
		t.Errorf("distinct unstorable keys share a bucket: %q", seen[:3])
	}
	if seen[4] != "api|ordinary-key" {
		t.Errorf("a storable key changed: %q", seen[4])
	}
}
