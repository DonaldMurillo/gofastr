package auth

import (
	"fmt"
	"testing"
	"time"
)

func TestOAuth2NonceMapHasHardCap(t *testing.T) {
	p := NewOAuth2Plugin(OAuth2Config{StateSecret: "test-state-secret-01"})
	liveUntil := time.Now().Add(time.Hour)
	for i := 0; i < nonceMaxEntries; i++ {
		p.usedNonces[fmt.Sprintf("used-%d", i)] = liveUntil
	}
	p.lastNonceSweep = time.Now()

	state, err := p.generateState("mock", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.validateAndConsumeState(state, "mock"); ok {
		t.Fatal("callback was accepted while the replay-protection map was full")
	}
	if got := len(p.usedNonces); got != nonceMaxEntries {
		t.Fatalf("nonce map grew past its cap: got %d entries", got)
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
