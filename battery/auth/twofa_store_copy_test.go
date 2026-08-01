package auth

import (
	"context"
	"sync"
	"testing"
)

// MemorySessionStore was fixed to return copies because handing out the
// stored pointer shares it with callers that read and mutate it without
// the lock. MemoryTwoFAStore in the same package has the identical shape
// and was not touched: two concurrent backup-code regenerations both
// received the same *TwoFAState and both assigned BackupCodes on it.
func TestMemoryTwoFAStoreReturnsCopies(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryTwoFAStore()
	if err := store.SetTwoFA(ctx, "u1", &TwoFAState{
		Enabled:     true,
		Secret:      "SEED",
		BackupCodes: []string{"a", "b"},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetTwoFA(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	// A caller mutating what it was handed must not reach into the store.
	got.Secret = "MUTATED"
	got.BackupCodes[0] = "clobbered"

	fresh, err := store.GetTwoFA(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Secret != "SEED" {
		t.Errorf("caller mutation reached the stored Secret: %q", fresh.Secret)
	}
	if fresh.BackupCodes[0] != "a" {
		t.Errorf("caller mutation reached the stored BackupCodes: %v — a struct copy still shares the slice", fresh.BackupCodes)
	}
}

// The race the copy prevents. Run with -race.
func TestMemoryTwoFAStoreConcurrentReadWrite(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryTwoFAStore()
	if err := store.SetTwoFA(ctx, "u1", &TwoFAState{Enabled: true, BackupCodes: []string{"a"}}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st, err := store.GetTwoFA(ctx, "u1")
			if err != nil || st == nil {
				return
			}
			// What backupCodesHandler does: overwrite the codes on the
			// value the store handed back.
			st.BackupCodes = []string{"regenerated"}
			st.Verified = true
		}()
	}
	wg.Wait()
}
