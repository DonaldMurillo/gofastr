package auth_test

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/auth"
)

// OAuth2Plugin.Init must fail closed when the configured UserStore is not
// an OAuthLinker. The legacy email-trust fallback is gone — without a
// durable (provider, provider_id) → user_id store, the OAuth callback
// cannot bind identity safely, and an IdP emitting an unverified email
// could otherwise sign in as an existing account. Production must refuse
// to boot.
func (minimumNonLinkerStore) UpdateRoles(context.Context, string, []string) error {
	return nil
}

// minimumNonLinkerStore implements auth.UserStore and nothing else. The
// methods never run in these tests — Init only type-asserts.
type minimumNonLinkerStore struct{}

func (minimumNonLinkerStore) FindByEmail(context.Context, string) (auth.User, string, error) {
	return nil, "", auth.ErrUserNotFound
}
func (minimumNonLinkerStore) FindByID(context.Context, string) (auth.User, error) {
	return nil, auth.ErrUserNotFound
}
func (minimumNonLinkerStore) CreateUser(context.Context, string, string, []string) (auth.User, error) {
	return nil, auth.ErrEmailTaken
}

// prodOAuth2Manager builds an AuthManager in production posture
// (DevMode=false, AllowInMemoryStores=false) with the given user store.
// Used by the fail-closed / dev-mode / ack-mode matrix below.
func prodOAuth2Manager(store auth.UserStore) *auth.AuthManager {
	mgr := auth.New(auth.AuthConfig{
		JWTSecret: "test-secret", // prod-mode Init fails closed without one
		UserStore: store,
	})
	mgr.Use(auth.NewOAuth2Plugin(auth.OAuth2Config{StateSecret: "test"}))
	return mgr
}

// TestOAuth2Plugin_Init_FailsClosedWithoutLinker: production Init must
// refuse to boot when the UserStore is not an OAuthLinker. This is the
// load-bearing gate — without it, every host on the recommended
// EntityUserStore would silently fall back to email-trust.
func TestOAuth2Plugin_Init_FailsClosedWithoutLinker(t *testing.T) {
	mgr := prodOAuth2Manager(minimumNonLinkerStore{})
	if err := mgr.Init(nil); err == nil {
		t.Fatal("production Init must fail closed when UserStore is not an OAuthLinker")
	} else {
		// The message must name the cause and the fix — a generic "init
		// failed" leaves the operator guessing.
		msg := err.Error()
		for _, want := range []string{"OAuthLinker", "NewEntityUserStore", "AllowInMemoryStores"} {
			if !strings.Contains(msg, want) {
				t.Errorf("error message must mention %q to be actionable; got: %v", want, err)
			}
		}
	}
}

// TestOAuth2Plugin_Init_AllowedInDevMode: DevMode keeps the no-linker path
// reachable so the rest of the OAuth plumbing (redirect, state, callback
// errors) stays unit-testable. The path logs a WARN rather than failing —
// resolveOAuthUser itself still returns errOAuthNoLinker at request time
// (pinned in oauth2_resolve_security_test.go).
func TestOAuth2Plugin_Init_AllowedInDevMode(t *testing.T) {
	mgr := auth.New(auth.AuthConfig{
		DevMode:   true,
		UserStore: minimumNonLinkerStore{},
	})
	mgr.Use(auth.NewOAuth2Plugin(auth.OAuth2Config{StateSecret: "test"}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("DevMode must allow a non-linker store (legacy path reachable for testing); got %v", err)
	}
}

// TestOAuth2Plugin_Init_AllowedWithInMemoryAck: AllowInMemoryStores is the
// production opt-in for a deliberate single-node deployment. It downgrades
// the failure to a WARN — the host has acknowledged the risk.
func TestOAuth2Plugin_Init_AllowedWithInMemoryAck(t *testing.T) {
	mgr := auth.New(auth.AuthConfig{
		JWTSecret:           "test-secret",
		AllowInMemoryStores: true,
		UserStore:           minimumNonLinkerStore{},
	})
	mgr.Use(auth.NewOAuth2Plugin(auth.OAuth2Config{StateSecret: "test"}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("AllowInMemoryStores must allow a non-linker store (acknowledged single-node); got %v", err)
	}
}

// TestOAuth2Plugin_Init_PassesWithLinker: sanity — when the store DOES
// implement OAuthLinker (EntityUserStore, the recommended production
// store), production Init succeeds. Without this gate the fail-closed
// branch could false-positive on a correctly-wired host.
func TestOAuth2Plugin_Init_PassesWithLinker(t *testing.T) {
	store := newLinkerUserStore()
	mgr := prodOAuth2Manager(store)
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init with a linker store must succeed in production; got %v", err)
	}
}

// linkerUserStore is the smallest store that satisfies auth.UserStore AND
// auth.OAuthLinker — used by TestOAuth2Plugin_Init_PassesWithLinker to
// confirm the gate admits a correctly-wired host.
type linkerUserStore struct{ minimumNonLinkerStore }

func newLinkerUserStore() *linkerUserStore { return &linkerUserStore{} }

func (linkerUserStore) FindByOAuth(context.Context, string, string) (auth.User, error) {
	return nil, auth.ErrUserNotFound
}
func (linkerUserStore) LinkOAuth(context.Context, string, string, string) error {
	return nil
}
