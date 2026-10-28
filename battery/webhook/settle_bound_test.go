package webhook

import (
	"context"
	"sync"
	"testing"
	"time"
)

// deadlineStore records whether each settle write ran under a deadline.
// A settle on a bare background context has none, so a store blocked on
// a dead database holds the worker, and Stop, forever (B27).
type deadlineStore struct {
	*MemoryStore
	mu    sync.Mutex
	calls map[string]bool
}

func (s *deadlineStore) note(name string, ctx context.Context) {
	_, ok := ctx.Deadline()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[name] = ok
}

func (s *deadlineStore) UpdateDelivery(ctx context.Context, d Delivery) error {
	s.note("update", ctx)
	return s.MemoryStore.UpdateDelivery(ctx, d)
}

func (s *deadlineStore) releaseClaim(ctx context.Context, d Delivery, now time.Time) error {
	s.note("release", ctx)
	return s.MemoryStore.releaseClaim(ctx, d, now)
}

func TestSettleWritesAreBounded(t *testing.T) {
	store := &deadlineStore{MemoryStore: NewMemoryStore(), calls: map[string]bool{}}
	m := New(store, Options{})
	m.saveDelivery(Delivery{ID: "d1", Status: StatusSuccess})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.releaseClaims(ctx, store, []Delivery{{ID: "d2"}})
	for _, name := range []string{"update", "release"} {
		bounded, ran := store.calls[name]
		if !ran {
			t.Fatalf("%s settle never ran", name)
		}
		if !bounded {
			t.Errorf("%s settle ran with no deadline", name)
		}
	}
}
