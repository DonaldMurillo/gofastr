package framework

import (
	"sync/atomic"
	"testing"
	"time"
)

// WithBrokerHandleTTL(0) stored the zero, so every delegation handle was
// minted already expired and the next reverse call was denied: a module
// could never act for its caller. A negative TTL did the same. A
// non-positive TTL now keeps the five-minute default.
func TestBrokerNonPositiveHandleTTLKeepsDefault(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Minute} {
		reg := brokerRegistry(brokerEntity("articles", "articles", nil))
		var hit atomic.Bool
		now := time.Now()
		b := NewBroker(fakeSuccessRouter(&hit), reg, nil, "",
			WithBrokerHandleTTL(d),
			WithBrokerClock(func() time.Time { return now }))
		handle, release := b.MintDelegation(nil, 1)
		b.mu.Lock()
		expires := b.handles[handle].expires
		b.mu.Unlock()
		release()
		if want := now.Add(5 * time.Minute); !expires.Equal(want) {
			t.Errorf("WithBrokerHandleTTL(%s): handle expires %s after mint, want the default %s", d, expires.Sub(now), want.Sub(now))
		}
	}
}
