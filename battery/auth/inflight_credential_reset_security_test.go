package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// createHookTokens runs a hook once, right before the token row is
// inserted: the point a request held open across a reset reaches after
// the reset has already listed and revoked the user's tokens.
type createHookTokens struct {
	*SQLAPITokenStore
	beforeCreate func()
}

func (s *createHookTokens) Create(ctx context.Context, t APIToken, hash string) error {
	if hook := s.beforeCreate; hook != nil {
		s.beforeCreate = nil
		hook()
	}
	return s.SQLAPITokenStore.Create(ctx, t, hash)
}

// An attacker with a stolen session held POST /auth/tokens open with the
// body withheld. The reset listed the user's tokens once, before the
// insert, so the token the open request then minted lived on as a
// permanent credential (2026-10-27 A2). The handler snapshots the
// password hash before reading the body and revokes its token when a
// reset moved the hash meanwhile.
func TestTokenCreateOvertakenByReset(t *testing.T) {
	_, sqlTokens, _ := newTokenTestDB(t)
	users := newUserStoreWithPassword()
	hash, _ := HashPassword("oldpw123")
	alice := &BasicUser{ID: "alice", Email: "alice@example.com"}
	users.users[alice.Email] = &storeEntry{user: alice, hash: hash}
	users.byID[alice.ID] = users.users[alice.Email]
	mgr := New(AuthConfig{JWTSecret: "x", DevMode: true, UserStore: users})
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	tokens := &createHookTokens{SQLAPITokenStore: sqlTokens}
	plugin := NewTokensPlugin(tokens)
	if err := plugin.Init(mgr); err != nil {
		t.Fatal(err)
	}
	tokens.beforeCreate = func() {
		resetHash, _ := HashPassword("resetpw123")
		if err := users.SetPassword(context.Background(), alice.ID, resetHash); err != nil {
			t.Errorf("setup: reset write: %v", err)
		}
	}

	req := bearerRequestWithJSON("POST", "/auth/tokens", `{"name":"keep","scopes":["a:b"]}`)
	req = req.WithContext(handler.SetUser(req.Context(), alice))
	rec := httptest.NewRecorder()
	plugin.createTokenHandler().ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Errorf("SECURITY: a token minted across a reset was handed out (body=%s)", rec.Body.String())
	}
	toks, err := sqlTokens.List(context.Background(), OwnerKindUser, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range toks {
		if tok.RevokedAt == nil {
			t.Errorf("SECURITY: token %s minted across a reset is live", tok.Prefix)
		}
	}
}

// addHookPasskeys runs a hook once, right before a passkey is stored.
type addHookPasskeys struct {
	PasskeyStore
	beforeAdd func()
}

func (s *addHookPasskeys) AddPasskey(ctx context.Context, c PasskeyCredential) error {
	if hook := s.beforeAdd; hook != nil {
		s.beforeAdd = nil
		hook()
	}
	return s.PasskeyStore.AddPasskey(ctx, c)
}

// The same race reached passkey registration: a registration finished
// after the victim's reset purged passkeys kept a second persistent
// credential (A2 extension).
func TestPasskeyRegisterOvertakenByReset(t *testing.T) {
	f := newPasskeyFixture(t)
	cookie := f.session(t, "u-alice")
	a := newFakeAuthenticator(t, coseAlgES256)
	ch := challengeOf(t, f.do(t, "POST", "/passkey/register/options", cookie, nil))
	hooked := &addHookPasskeys{PasskeyStore: f.pk.store}
	f.pk.store = hooked
	hooked.beforeAdd = func() { f.resetAlice(t) }

	w := f.do(t, "POST", "/passkey/register", cookie, registerBody(a, ch))
	if w.Code == http.StatusCreated {
		t.Errorf("SECURITY: a passkey registered across a reset answered 201")
	}
	keys, err := hooked.ListPasskeys(context.Background(), "u-alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Errorf("SECURITY: %d passkey(s) registered across a reset survived it", len(keys))
	}
}
