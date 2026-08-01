package embed

import (
	"context"
	"testing"
	"time"
)

// The embed host schedules no background sweeper, so MemoryBurnStore.Prune had
// no driver: rows grew by one entry per nonce exchange for the life of the
// process. Burn now self-prunes past a high-water mark, so an expired row must
// vanish once enough writes pile up to trigger a sweep.

// TestMemoryBurnStoreSelfPrunesExpiredRows: with a low threshold (and no
// interval gate), piling a live burn on top of an expired one triggers a sweep
// that deletes the expired row. Re-burning the expired nonce then looks like a
// fresh claim (replay=false) — proving the row vanished instead of lingering.
func TestMemoryBurnStoreSelfPrunesExpiredRows(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	s := NewMemoryBurnStore(WithBurnPrunePolicy(2, 0)) // threshold=2, prune immediately when over

	if _, _, err := s.Burn(ctx, "dead", "g-dead", now.Add(-time.Hour)); err != nil {
		t.Fatalf("Burn dead: %v", err)
	}
	// A live burn pushes the row count to the threshold and trips the sweep.
	if _, _, err := s.Burn(ctx, "trigger", "g-trigger", now.Add(time.Hour)); err != nil {
		t.Fatalf("Burn trigger: %v", err)
	}

	if _, replay, err := s.Burn(ctx, "dead", "g2", now.Add(time.Hour)); err != nil || replay {
		t.Fatalf("expired burn row survived — opportunistic prune did not run "+
			"(replay=%v err=%v); rows grow per nonce exchange forever", replay, err)
	}
}

// TestMemoryBurnStoreSelfPruneKeepsLiveRows: the sweep deletes only expired
// rows; a live burn survives the prune triggered by another write.
func TestMemoryBurnStoreSelfPruneKeepsLiveRows(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	s := NewMemoryBurnStore(WithBurnPrunePolicy(2, 0))

	if _, _, err := s.Burn(ctx, "live", "g-live", now.Add(time.Hour)); err != nil {
		t.Fatalf("Burn live: %v", err)
	}
	if _, _, err := s.Burn(ctx, "trigger", "g-trigger", now.Add(time.Hour)); err != nil {
		t.Fatalf("Burn trigger: %v", err)
	}

	// "live" is unexpired — the sweep must NOT have removed it.
	if _, replay, _ := s.Burn(ctx, "live", "g2", now.Add(time.Hour)); !replay {
		t.Fatal("opportunistic prune removed a still-live burn — the nonce became reusable")
	}
}

// TestMemoryBurnStorePruneRespectsIntervalGate: with a non-zero interval, the
// sweep runs at most once per interval even while over the threshold, so a
// workload of all-live nonces does not scan the whole map on every write.
func TestMemoryBurnStorePruneRespectsIntervalGate(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	s := NewMemoryBurnStore(WithBurnPrunePolicy(1, time.Hour)) // threshold=1, gated to 1/hour

	// First write over the threshold primes lastPrune.
	if _, _, err := s.Burn(ctx, "a", "ga", now.Add(time.Hour)); err != nil {
		t.Fatalf("Burn a: %v", err)
	}
	// Second write is over the threshold but inside the interval gate, so no
	// sweep runs. An expired row added here must therefore survive (the gate
	// suppressed the prune).
	if _, _, err := s.Burn(ctx, "dead", "gd", now.Add(-time.Hour)); err != nil {
		t.Fatalf("Burn dead: %v", err)
	}
	if _, replay, _ := s.Burn(ctx, "dead", "g2", now.Add(time.Hour)); !replay {
		t.Fatal("prune ran inside its interval gate — the all-live-workload scan-per-write regression")
	}
}
