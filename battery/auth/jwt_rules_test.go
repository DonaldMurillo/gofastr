package auth

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"
)

type otherUserByEmailStore struct{ *memoryUserStore }

func (s otherUserByEmailStore) FindByEmail(ctx context.Context, email string) (User, string, error) {
	_, hash, err := s.memoryUserStore.FindByEmail(ctx, email)
	return &BasicUser{ID: "someone-else", Email: email}, hash, err
}

func TestJWTWithoutCredentialTagRefused(t *testing.T) {
	store := newMemoryUserStore()
	u := seedUser(t, store, "a@example.com", "supersecret1")
	bare := NewJWTAuth("s", time.Hour)
	tok, err := bare.GenerateToken(u)
	if err != nil {
		t.Fatal(err)
	}
	j := NewJWTAuth("s", time.Hour)
	j.SetUserStore(store)
	c, err := j.ValidateToken(tok)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := j.resolveOwner(context.Background(), c); ok {
		t.Error("a token with no credential tag resolved an owner through a store-backed JWTAuth")
	}
}

func TestJWTCredentialLookupOtherUserRefused(t *testing.T) {
	store := newMemoryUserStore()
	u := seedUser(t, store, "a@example.com", "supersecret1")
	j := NewJWTAuth("s", time.Hour)
	j.SetUserStore(store)
	tok, err := j.GenerateToken(u)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := j.ValidateToken(tok)
	j.SetUserStore(otherUserByEmailStore{store})
	if _, ok := j.resolveOwner(context.Background(), c); ok {
		t.Error("a credential read from another user's row vouched for the token")
	}
}

func TestGenerateTokenRefusesUnknownUser(t *testing.T) {
	j := NewJWTAuth("s", time.Hour)
	j.SetUserStore(newMemoryUserStore())
	if _, err := j.GenerateToken(&BasicUser{ID: "ghost", Email: "ghost@example.com"}); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("GenerateToken for a user the store lacks = %v, want ErrUnauthorized", err)
	}
}

func TestJWTSignsOnlyWithCurrentSecret(t *testing.T) {
	j := NewJWTAuth("current-secret", time.Hour)
	j.PreviousSecrets = []string{"old-secret"}
	tok, err := j.GenerateToken(&BasicUser{ID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewJWTAuth("current-secret", time.Hour).ValidateToken(tok); err != nil {
		t.Errorf("a new token does not verify under the current secret alone: %v", err)
	}
}

func TestExtractBearerTokenNeedsBearerScheme(t *testing.T) {
	for hdr, want := range map[string]string{"Bearer abc": "abc", "bearer  abc ": "abc", "Basic abc": "", "abc": "", "": ""} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Authorization", hdr)
		if got := extractBearerToken(r); got != want {
			t.Errorf("extractBearerToken(%q) = %q, want %q", hdr, got, want)
		}
	}
}
