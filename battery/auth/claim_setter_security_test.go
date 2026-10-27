package auth

import (
	"context"
	"testing"
	"time"
)

// setterOnlyStore implements PasswordSetter but not PasswordClearer: the
// field shadows the ClearPassword the embedded memory store would promote.
type setterOnlyStore struct {
	*userStoreWithPassword
	ClearPassword struct{}
}

// A store with only SetPassword gets a fresh unusable hash on every claim,
// so the JWTs bound to the hash before it end, even when it was already a
// placeholder.
func TestClaimViaSetterWritesFreshHash(t *testing.T) {
	store := &setterOnlyStore{userStoreWithPassword: newUserStoreWithPassword()}
	if _, ok := any(store).(PasswordClearer); ok {
		t.Fatal("setterOnlyStore still implements PasswordClearer")
	}
	mgr := New(AuthConfig{
		JWTSecret: "test-secret", SessionTTL: time.Hour, SessionCookie: "session_id",
		UserStore: store, AllowInMemoryStores: true,
	})
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	u, err := store.CreateUser(ctx, "s@example.com", passwordPlaceholderHash, nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{passwordPlaceholderHash: true}
	for range 2 {
		if err := mgr.claimAccount(ctx, u.GetID(), "magic_link", "", true); err != nil {
			t.Fatal(err)
		}
		_, hash, err := store.FindByEmail(ctx, "s@example.com")
		if err != nil {
			t.Fatal(err)
		}
		if seen[hash] {
			t.Fatalf("claim left a hash already seen: %q", hash)
		}
		seen[hash] = true
	}
}
