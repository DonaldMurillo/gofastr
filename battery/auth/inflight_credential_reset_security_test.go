package auth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

// A reset that finished after the session check but before the snapshot
// left the snapshot reading the new hash, so snap.moved never fired
// (#621 review). The reset deleted the session the request came in on,
// and the handler re-checks it after the insert.
func TestTokenCreateAfterResetBeforeSnapshot(t *testing.T) {
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
	plugin := NewTokensPlugin(sqlTokens)
	if err := plugin.Init(mgr); err != nil {
		t.Fatal(err)
	}
	sess, _, err := mgr.MintSession(context.Background(), alice.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// The session middleware resolved alice, then the reset ran in full:
	// new hash, session deleted. The snapshot is taken after it.
	ctx := handler.WithPrincipalCheck(handler.SetUser(context.Background(), alice),
		sessionRecheck(mgr, sess.Token, slog.Default()))
	resetHash, _ := HashPassword("resetpw123")
	if err := users.SetPassword(context.Background(), alice.ID, resetHash); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SessionStore().Delete(context.Background(), sess.Token); err != nil {
		t.Fatal(err)
	}

	req := bearerRequestWithJSON("POST", "/auth/tokens", `{"name":"keep","scopes":["a:b"]}`).WithContext(ctx)
	rec := httptest.NewRecorder()
	plugin.createTokenHandler().ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Errorf("SECURITY: a token minted on a session the reset deleted was handed out (body=%s)", rec.Body.String())
	}
	toks, err := sqlTokens.List(context.Background(), OwnerKindUser, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range toks {
		if tok.RevokedAt == nil {
			t.Errorf("SECURITY: token %s minted on a deleted session is live", tok.Prefix)
		}
	}
}

// A live session still mints: the re-check passes for the session the
// request came in on.
func TestTokenCreateOnLiveSession(t *testing.T) {
	_, sqlTokens, _ := newTokenTestDB(t)
	users := newUserStoreWithPassword()
	alice := &BasicUser{ID: "alice", Email: "alice@example.com"}
	users.users[alice.Email] = &storeEntry{user: alice}
	users.byID[alice.ID] = users.users[alice.Email]
	mgr := New(AuthConfig{JWTSecret: "x", DevMode: true, UserStore: users})
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	plugin := NewTokensPlugin(sqlTokens)
	if err := plugin.Init(mgr); err != nil {
		t.Fatal(err)
	}
	sess, _, err := mgr.MintSession(context.Background(), alice.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ctx := handler.WithPrincipalCheck(handler.SetUser(context.Background(), alice),
		sessionRecheck(mgr, sess.Token, slog.Default()))
	req := bearerRequestWithJSON("POST", "/auth/tokens", `{"name":"keep","scopes":["a:b"]}`).WithContext(ctx)
	rec := httptest.NewRecorder()
	plugin.createTokenHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("a live session's token mint = %d %s", rec.Code, rec.Body.String())
	}
}

// ctxStrictPasskeys refuses a delete on a cancelled context, as a SQL
// store does, and runs one hook right before a passkey is stored and one
// right after.
type ctxStrictPasskeys struct {
	PasskeyStore
	beforeAdd, afterAdd func()
}

func (s *ctxStrictPasskeys) AddPasskey(ctx context.Context, c PasskeyCredential, maxPerUser int) error {
	if hook := s.beforeAdd; hook != nil {
		s.beforeAdd = nil
		hook()
	}
	err := s.PasskeyStore.AddPasskey(ctx, c, maxPerUser)
	if hook := s.afterAdd; hook != nil && err == nil {
		s.afterAdd = nil
		hook()
	}
	return err
}

func (s *ctxStrictPasskeys) DeletePasskey(ctx context.Context, userID string, id []byte) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return s.PasskeyStore.DeletePasskey(ctx, userID, id)
}

// The fence's undo ran on the request context, so an attacker who hung
// up right after the insert cancelled the delete and kept a passkey
// registered across the reset (#621 review). The undo is detached now.
func TestPasskeyUndoSurvivesClientHangup(t *testing.T) {
	f := newPasskeyFixture(t)
	cookie := f.session(t, "u-alice")
	a := newFakeAuthenticator(t, coseAlgES256)
	ch := challengeOf(t, f.do(t, "POST", "/passkey/register/options", cookie, nil))
	ctx, hangUp := context.WithCancel(context.Background())
	defer hangUp()
	strict := &ctxStrictPasskeys{PasskeyStore: f.pk.store}
	f.pk.store = strict
	// The reset purges passkeys before this one lands; the client hangs
	// up once it has.
	strict.beforeAdd = func() { f.resetAlice(t) }
	strict.afterAdd = hangUp

	b, _ := json.Marshal(registerBody(a, ch))
	req := httptest.NewRequest("POST", "https://example.com/auth/passkey/register", strings.NewReader(string(b))).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	f.r.ServeHTTP(httptest.NewRecorder(), req)

	keys, err := strict.ListPasskeys(context.Background(), "u-alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Errorf("SECURITY: a client hangup kept %d passkey(s) registered across a reset", len(keys))
	}
}

// createThenHangUpTokens stores the token, then runs a hook once: the
// reset lands and the client hangs up before the fence's undo.
type createThenHangUpTokens struct {
	*SQLAPITokenStore
	afterCreate func()
}

func (s *createThenHangUpTokens) Create(ctx context.Context, t APIToken, hash string) error {
	err := s.SQLAPITokenStore.Create(ctx, t, hash)
	if hook := s.afterCreate; hook != nil && err == nil {
		s.afterCreate = nil
		hook()
	}
	return err
}

func TestTokenUndoSurvivesClientHangup(t *testing.T) {
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
	tokens := &createThenHangUpTokens{SQLAPITokenStore: sqlTokens}
	plugin := NewTokensPlugin(tokens)
	if err := plugin.Init(mgr); err != nil {
		t.Fatal(err)
	}
	ctx, hangUp := context.WithCancel(handler.SetUser(context.Background(), alice))
	defer hangUp()
	tokens.afterCreate = func() {
		resetHash, _ := HashPassword("resetpw123")
		if err := users.SetPassword(context.Background(), alice.ID, resetHash); err != nil {
			t.Errorf("setup: reset write: %v", err)
		}
		hangUp()
	}
	req := bearerRequestWithJSON("POST", "/auth/tokens", `{"name":"keep","scopes":["a:b"]}`).WithContext(ctx)
	plugin.createTokenHandler().ServeHTTP(httptest.NewRecorder(), req)
	toks, err := sqlTokens.List(context.Background(), OwnerKindUser, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range toks {
		if tok.RevokedAt == nil {
			t.Errorf("SECURITY: a client hangup kept token %s minted across a reset live", tok.Prefix)
		}
	}
}

// addHookPasskeys runs a hook once, right before a passkey is stored.
type addHookPasskeys struct {
	PasskeyStore
	beforeAdd func()
}

func (s *addHookPasskeys) AddPasskey(ctx context.Context, c PasskeyCredential, maxPerUser int) error {
	if hook := s.beforeAdd; hook != nil {
		s.beforeAdd = nil
		hook()
	}
	return s.PasskeyStore.AddPasskey(ctx, c, maxPerUser)
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
