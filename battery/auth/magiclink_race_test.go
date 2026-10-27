package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// staleFindStore misses the first FindByEmail, the read a concurrent
// redeem makes before the other one's CreateUser lands.
type staleFindStore struct {
	*memoryUserStore
	missed atomic.Bool
}

func (s *staleFindStore) FindByEmail(ctx context.Context, email string) (User, string, error) {
	if s.missed.CompareAndSwap(false, true) {
		return nil, "", ErrUserNotFound
	}
	return s.memoryUserStore.FindByEmail(ctx, email)
}

// Two magic links for one new address redeemed at once: the one whose
// create loses to the other's signs in to the account the winner made,
// rather than spending its token on a 500.
func TestMagicLinkCreateRaceSignsIn(t *testing.T) {
	inner := newMemoryUserStore()
	store := &staleFindStore{memoryUserStore: inner}
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret",
		SessionTTL:          24 * time.Hour,
		SessionCookie:       "session_id",
		UserStore:           store,
		AllowInMemoryStores: true,
	})
	plugin := NewMagicLinkPlugin(MagicLinkConfig{
		BaseURL: "http://localhost:8080", OnSuccessURL: "/dashboard",
		TokenTTL: 15 * time.Minute, DevMode: true,
	})
	mgr.Use(plugin)
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	ctx := context.Background()
	email := "race@example.com"
	// The winner's redeem already created and verified the account.
	u, err := inner.CreateUserNoPassword(ctx, email, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := inner.MarkEmailVerified(ctx, u.GetID()); err != nil {
		t.Fatal(err)
	}

	token, _ := createPurposeToken(ctx, plugin.tokenStore, purposeMagicLink, email, 15*time.Minute)
	w := httptest.NewRecorder()
	mountMagicLinkRoutes(mgr).ServeHTTP(w, magicConfirmReq(token))
	if w.Code != http.StatusFound {
		t.Fatalf("losing redeem = %d: %s, want 302 into the winner's account", w.Code, w.Body.String())
	}
}
