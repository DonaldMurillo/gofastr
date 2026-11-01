package harness

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/session"
)

// slowStore persists each event after a delay, failing an append whose
// context has ended, the way the SQLite store's ExecContext does.
type slowStore struct {
	session.Store
	delay   time.Duration
	fail    error
	mu      sync.Mutex
	saved   int
	closed  bool
	lateErr bool
}

func (s *slowStore) AppendEvent(ctx context.Context, _ control.EventEnvelope) error {
	select {
	case <-time.After(s.delay):
	case <-ctx.Done():
		return ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		s.lateErr = true
		return errors.New("append after close")
	}
	if s.fail != nil {
		return s.fail
	}
	s.saved++
	return nil
}

func (s *slowStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// publishN puts n events on the only session's bus.
func publishN(t *testing.T, h *Harness, n int) {
	t.Helper()
	h.sessionMu.Lock()
	bus := h.sessionRuns[0].bus
	h.sessionMu.Unlock()
	for range n {
		if _, err := bus.Publish(control.TextDelta{Text: "x"}, ids.NewClientID()); err != nil {
			t.Fatal(err)
		}
	}
}

// Shutdown closed the bus, cancelled the persist context at once and
// closed the store without joining the persist loop, so queued events
// failed on the cancelled context and were dropped in silence
// (2026-10-27 bug bash H11). Shutdown now drains the loop first.
func TestShutdownPersistsQueuedEvents(t *testing.T) {
	h := newShutdownTestHarness(t)
	store := &slowStore{delay: 5 * time.Millisecond}
	_ = h.Sessions.Close()
	h.Sessions = store
	h.CreateSession(h.Providers[0], "openrouter:test")
	publishN(t, h, 20)
	if err := h.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.saved != 20 || store.lateErr {
		t.Fatalf("saved %d of 20 events (append after close: %v)", store.saved, store.lateErr)
	}
}

// A store that refuses the events makes Shutdown say so.
func TestShutdownReturnsStoreErrors(t *testing.T) {
	h := newShutdownTestHarness(t)
	boom := errors.New("disk full")
	store := &slowStore{fail: boom}
	_ = h.Sessions.Close()
	h.Sessions = store
	h.CreateSession(h.Providers[0], "openrouter:test")
	publishN(t, h, 3)
	if err := h.Shutdown(); !errors.Is(err, boom) {
		t.Fatalf("Shutdown error = %v, want the store's %v", err, boom)
	}
}

// A store that never finishes cannot hold Shutdown past its limit; the
// events it did not take are reported.
func TestShutdownDrainIsBounded(t *testing.T) {
	prev := shutdownDrainTimeout
	shutdownDrainTimeout = 100 * time.Millisecond
	t.Cleanup(func() { shutdownDrainTimeout = prev })
	h := newShutdownTestHarness(t)
	store := &slowStore{delay: time.Hour}
	_ = h.Sessions.Close()
	h.Sessions = store
	h.CreateSession(h.Providers[0], "openrouter:test")
	publishN(t, h, 2)
	start := time.Now()
	err := h.Shutdown()
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("Shutdown took %s", took)
	}
	if err == nil {
		t.Fatal("Shutdown reported success with events unpersisted")
	}
}
