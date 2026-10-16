package webhook

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// A claim copies the payload of each delivery it hands out once, and
// copies nothing for the pending rows it leaves.
func TestClaimCopiesOnlyClaimedPayloads(t *testing.T) {
	const n = 300
	ctx := context.Background()
	now := time.Now()
	measure := func() float64 {
		m := NewMemoryStore()
		for i := range n {
			d := Delivery{ID: fmt.Sprintf("d%03d", i), SubscriberID: "s", Payload: make([]byte, 64), Status: StatusPending, NextAttemptAt: now.Add(-time.Minute)}
			if err := m.AddDelivery(ctx, d); err != nil {
				t.Fatal(err)
			}
		}
		return testing.AllocsPerRun(1, func() {
			if _, err := m.ClaimDueDeliveries(ctx, now, 1, time.Minute); err != nil {
				t.Fatal(err)
			}
		})
	}
	if got := measure(); got > n/2 {
		t.Fatalf("claiming 1 of %d pending deliveries made %.0f allocations", n, got)
	}
}
