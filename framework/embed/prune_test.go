package embed

import (
	"context"
	"testing"
	"time"
)

// Prune must keep a burned row for PruneGrace past its retention deadline.
//
// Burn refuses a claim whose deadline has passed, using the calling replica's
// clock; Prune deletes using its own. Two replicas whose clocks disagree can
// therefore race: a fast pruner on replica A deletes a row a clock-skewed
// verifier on replica B still needs — which un-burns the nonce and lets a
// second grant be minted from it, defeating the single-use guarantee across
// replicas. The grace margin is what closes that race, so shrinking it to zero
// must turn a row that should survive into one that gets deleted.
func TestSQLBurnStorePruneKeepsRowsInsideTheGraceWindow(t *testing.T) {
	s := newSQLiteBurnStore(t)
	ctx := context.Background()
	now := time.Now()

	// A row whose retention deadline passed one minute ago — inside the
	// default 5-minute grace. A clock-skewed verifier may still need it.
	if _, _, err := s.Burn(ctx, "within-grace", "g1", now.Add(-time.Minute)); err != nil {
		t.Fatalf("Burn within-grace: %v", err)
	}
	// A row six minutes past its deadline — past the grace, safe to delete.
	if _, _, err := s.Burn(ctx, "past-grace", "g2", now.Add(-6*time.Minute)); err != nil {
		t.Fatalf("Burn past-grace: %v", err)
	}

	if err := s.Prune(ctx, now); err != nil {
		t.Fatalf("Prune: %v", err)
	}

	// The within-grace row must still be burned: re-burning it replays.
	if _, replay, err := s.Burn(ctx, "within-grace", "g3", now.Add(time.Hour)); err != nil || !replay {
		t.Fatalf("Prune deleted a row inside the grace window (replay=%v, err=%v) — "+
			"a clock-skewed verifier could now mint a second grant from this nonce", replay, err)
	}
	// The past-grace row must be gone: re-burning it is a fresh claim.
	if _, replay, _ := s.Burn(ctx, "past-grace", "g4", now.Add(time.Hour)); replay {
		t.Fatal("Prune left a row behind that is past the grace window")
	}
}
