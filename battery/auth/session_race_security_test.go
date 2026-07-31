package auth_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/auth"
)

// MemorySessionStore.Get must not hand back the same *Session pointer the
// 2FA marker methods mutate in place under the lock. Before the fix, Get
// returned the stored pointer after releasing RLock while
// MarkTwoFactorVerified / MarkPendingTwoFactor flipped fields on that same
// object under Lock — a -race run with concurrent Get + both markers
// reports three data races. This contract test reproduces it under the
// race detector.
func TestMemSession_GetVsMarkersNoRace(t *testing.T) {
	store := auth.NewMemorySessionStore()
	ctx := context.Background()

	sess, err := store.Create(ctx, "user-1", time.Hour)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tok := sess.Token

	const iterations = 400
	var wg sync.WaitGroup
	wg.Add(3)

	// Reader: Get returns a session whose fields the other goroutines are
	// concurrently mutating. If Get shares the stored pointer, reading
	// TwoFactorVerified / PendingTwoFactor here races with the writes.
	go func() {
		defer wg.Done()
		for range iterations {
			got, err := store.Get(ctx, tok)
			if err != nil {
				t.Errorf("Get: %v", err)
				return
			}
			// Touch the fields the markers mutate to force a read that the
			// race detector can observe.
			_ = got.TwoFactorVerified
			_ = got.PendingTwoFactor
		}
	}()

	// Writer A: flips TwoFactorVerified true and clears Pending.
	go func() {
		defer wg.Done()
		for range iterations {
			if err := store.MarkTwoFactorVerified(ctx, tok); err != nil {
				t.Errorf("MarkTwoFactorVerified: %v", err)
				return
			}
		}
	}()

	// Writer B: flips PendingTwoFactor true.
	go func() {
		defer wg.Done()
		for range iterations {
			if err := store.MarkPendingTwoFactor(ctx, tok); err != nil {
				t.Errorf("MarkPendingTwoFactor: %v", err)
				return
			}
		}
	}()

	wg.Wait()
}
