package auth

import (
	"fmt"
	"testing"
	"time"
)

// The map is capped, and at the cap it evicts its earliest-expiring
// entries instead of refusing the callback. This test pinned the old
// fail-closed answer until the 2026-11-01 bug bash: anyone can mint a
// valid bound state, so two requests per entry filled the map and locked
// every user out of OAuth sign-in for the state TTL.
func TestOAuth2NonceMapHasHardCap(t *testing.T) {
	p := NewOAuth2Plugin(OAuth2Config{StateSecret: "test-state-secret-01"})
	now := time.Now()
	for i := 0; i < nonceMaxEntries; i++ {
		p.usedNonces[fmt.Sprintf("used-%d", i)] = now.Add(time.Hour)
	}
	p.usedNonces["used-0"] = now.Add(time.Minute) // closest to expiry
	p.lastNonceSweep = now

	state, err := p.generateState("mock", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.validateAndConsumeState(state, "mock"); !ok {
		t.Fatal("callback refused while the replay-protection map was full")
	}
	if got := len(p.usedNonces); got > nonceMaxEntries {
		t.Fatalf("nonce map grew past its cap: got %d entries", got)
	}
	if _, kept := p.usedNonces["used-0"]; kept {
		t.Error("the earliest-expiring entry survived eviction")
	}
	if got, want := len(p.usedNonces), nonceMaxEntries-nonceEvictBatch+1; got != want {
		t.Errorf("after eviction the map holds %d entries, want %d", got, want)
	}
}

func TestOAuth2NonceSweepIsRateLimitedAndReclaimsExpired(t *testing.T) {
	p := NewOAuth2Plugin(OAuth2Config{StateSecret: "test-state-secret-01"})
	now := time.Now()
	p.lastNonceSweep = now
	for i := 0; i < nonceGCThreshold+1; i++ {
		p.usedNonces[fmt.Sprintf("live-%d", i)] = now.Add(time.Hour)
	}
	state, err := p.generateState("mock", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.validateAndConsumeState(state, "mock"); !ok {
		t.Fatal("fresh valid state was rejected below the nonce cap")
	}
	if !p.lastNonceSweep.Equal(now) {
		t.Fatal("nonce map was rescanned before the sweep interval elapsed")
	}

	p.lastNonceSweep = now.Add(-nonceGCSweepInterval)
	for nonce := range p.usedNonces {
		p.usedNonces[nonce] = now.Add(-time.Second)
	}
	state, err = p.generateState("mock", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.validateAndConsumeState(state, "mock"); !ok {
		t.Fatal("expired entries were not reclaimed before enforcing the cap")
	}
	if got := len(p.usedNonces); got != 1 {
		t.Fatalf("expired nonce entries survived sweep: got %d, want 1", got)
	}
}
