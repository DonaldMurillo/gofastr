package cache

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// A loader stuck on a locked table held every GetOrSet caller for the key
// past its own deadline, long after the timeout middleware had answered
// (2026-10-27 B17). Each caller leaves at its deadline; the fill is
// cancelled once no caller is left, and the next caller fills afresh.
func TestGetOrSetCallerLeavesAtDeadline(t *testing.T) {
	mc := NewMemoryCache(WithMaxEntries(16))
	defer mc.Close()

	release := make(chan struct{})
	defer close(release)
	// linger holds the cancelled fill open, so the next caller arrives
	// while it is still running and must not join it.
	linger := make(chan struct{})
	fillGone := make(chan struct{}, 4)
	var hang atomic.Bool
	hang.Store(true)
	loader := func(ctx context.Context) (any, error) {
		if !hang.Load() {
			return "fresh", nil
		}
		select {
		case <-ctx.Done():
			fillGone <- struct{}{}
			<-linger
			return nil, ctx.Err()
		case <-release:
			return "late", nil
		}
	}

	errs := make(chan error, 2)
	for range 2 {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			var v string
			errs <- GetOrSet(ctx, mc, "hot", time.Minute, &v, loader)
		}()
	}
	for range 2 {
		select {
		case err := <-errs:
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("caller err = %v, want DeadlineExceeded", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("a caller stayed in the fill past its deadline")
		}
	}
	select {
	case <-fillGone:
	case <-time.After(3 * time.Second):
		t.Fatal("the fill kept running with no caller left")
	}

	hang.Store(false)
	next := make(chan error, 1)
	var v string
	go func() { next <- GetOrSet(context.Background(), mc, "hot", time.Minute, &v, loader) }()
	select {
	case err := <-next:
		if err != nil || v != "fresh" {
			t.Fatalf("next caller: v=%q err=%v, want a fresh fill", v, err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the next caller joined the cancelled fill")
	}
	close(linger)
}

// A loader panic ran on the caller's goroutine and reached its recover;
// the fill now runs on its own goroutine, where a panic would kill the
// process. It comes back as each caller's error.
func TestGetOrSetLoaderPanicIsAnError(t *testing.T) {
	mc := NewMemoryCache(WithMaxEntries(16))
	defer mc.Close()
	var v string
	err := GetOrSet(context.Background(), mc, "boom", time.Minute, &v, func(context.Context) (any, error) {
		panic("loader blew up")
	})
	if err == nil {
		t.Fatal("a panicking loader returned no error")
	}
}
