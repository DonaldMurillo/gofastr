package webhook

import (
	"context"
	"testing"
)

func findDelivery(t *testing.T, s Store, id string) Delivery {
	t.Helper()
	all, err := s.ListDeliveries(context.Background(), "", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range all {
		if d.ID == id {
			return d
		}
	}
	t.Fatalf("delivery %q not found", id)
	return Delivery{}
}

// compile-time: both shipped stores support dead-letter replay.
var (
	_ ReplayableStore = (*MemoryStore)(nil)
	_ ReplayableStore = (*SQLStore)(nil)
)

func TestMemoryStore_DeadAndReset(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	if err := s.AddDelivery(ctx, Delivery{ID: "d1", Status: StatusDead, Attempts: 3, LastError: "boom"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddDelivery(ctx, Delivery{ID: "d2", Status: StatusPending}); err != nil {
		t.Fatal(err)
	}

	dead, err := s.ListDeadDeliveries(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(dead) != 1 || dead[0].ID != "d1" {
		t.Fatalf("ListDeadDeliveries = %+v, want [d1]", dead)
	}

	if err := s.ResetDelivery(ctx, "d1"); err != nil {
		t.Fatalf("ResetDelivery: %v", err)
	}
	got := findDelivery(t, s, "d1")
	if got.Status != StatusPending || got.Attempts != 0 || got.LastError != "" {
		t.Fatalf("after reset: status=%s attempts=%d err=%q; want pending/0/empty", got.Status, got.Attempts, got.LastError)
	}
	// Reset on a non-dead delivery is a no-op (no error, no change).
	if err := s.ResetDelivery(ctx, "d2"); err != nil {
		t.Fatalf("ResetDelivery non-dead should be a no-op: %v", err)
	}
}

func TestManager_ReplayViaStore(t *testing.T) {
	s := NewMemoryStore()
	mgr := New(s, Options{})
	ctx := context.Background()
	if err := s.AddDelivery(ctx, Delivery{ID: "d1", Status: StatusDead, Attempts: 5}); err != nil {
		t.Fatal(err)
	}

	dead, err := mgr.DeadDeliveries(ctx, 10)
	if err != nil {
		t.Fatalf("DeadDeliveries: %v", err)
	}
	if len(dead) != 1 || dead[0].ID != "d1" {
		t.Fatalf("DeadDeliveries = %+v, want [d1]", dead)
	}
	if err := mgr.Replay(ctx, "d1"); err != nil {
		t.Fatalf("Replay: %v", err)
	}
	got := findDelivery(t, s, "d1")
	if got.Status != StatusPending {
		t.Fatalf("after Replay status=%s, want pending", got.Status)
	}
}
