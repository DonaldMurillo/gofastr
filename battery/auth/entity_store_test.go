package auth

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
	_ "github.com/mattn/go-sqlite3"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// Create users table
	_, err = db.Exec(`CREATE TABLE users (
		id TEXT PRIMARY KEY,
		email TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		roles TEXT DEFAULT '["user"]',
		password_set BOOLEAN NOT NULL DEFAULT FALSE
	)`)
	if err != nil {
		t.Fatalf("create users table: %v", err)
	}
	// Create sessions table
	_, err = db.Exec(`CREATE TABLE sessions (
		id TEXT PRIMARY KEY,
		token TEXT NOT NULL UNIQUE,
		user_id TEXT NOT NULL,
		created_at DATETIME NOT NULL,
		expires_at DATETIME NOT NULL,
		two_factor_verified BOOLEAN NOT NULL DEFAULT FALSE,
		pending_two_factor BOOLEAN NOT NULL DEFAULT FALSE
	)`)
	if err != nil {
		t.Fatalf("create sessions table: %v", err)
	}
	return db
}

func TestEntityUserStore_CreateAndFind(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewEntityUserStore(db, "users")
	ctx := context.Background()

	hash, err := HashPassword("secret123")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	user, err := store.CreateUser(ctx, "alice@test.com", hash, []string{"admin", "editor"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if user.GetID() == "" {
		t.Fatal("expected non-empty user ID")
	}
	if user.GetEmail() != "alice@test.com" {
		t.Fatalf("expected alice@test.com, got %q", user.GetEmail())
	}

	// FindByEmail
	found, foundHash, err := store.FindByEmail(ctx, "alice@test.com")
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
	if found.GetID() != user.GetID() {
		t.Fatalf("expected ID %q, got %q", user.GetID(), found.GetID())
	}
	if !CheckPassword("secret123", foundHash) {
		t.Fatal("password hash mismatch")
	}

	// FindByID
	byID, err := store.FindByID(ctx, user.GetID())
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if byID.GetEmail() != "alice@test.com" {
		t.Fatalf("expected alice@test.com, got %q", byID.GetEmail())
	}
}

func TestEntityUserStore_FindByEmailNotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewEntityUserStore(db, "users")
	_, _, err := store.FindByEmail(context.Background(), "nobody@test.com")
	if err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestEntityUserStore_FindByIDNotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewEntityUserStore(db, "users")
	_, err := store.FindByID(context.Background(), "nonexistent")
	if err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestEntityUserStore_DuplicateEmail(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewEntityUserStore(db, "users")
	ctx := context.Background()

	hash, _ := HashPassword("pass1")
	_, err := store.CreateUser(ctx, "dup@test.com", hash, []string{"user"})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}

	hash2, _ := HashPassword("pass2")
	_, err = store.CreateUser(ctx, "dup@test.com", hash2, []string{"user"})
	if err == nil {
		t.Fatal("expected error on duplicate email")
	}
}

func TestEntityUserStore_RolesParsing(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewEntityUserStore(db, "users")
	ctx := context.Background()

	hash, _ := HashPassword("password1")
	user, err := store.CreateUser(ctx, "roles@test.com", hash, []string{"admin", "editor"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_ = user

	found, _, err := store.FindByEmail(ctx, "roles@test.com")
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
	roles := found.GetRoles()
	if len(roles) != 2 || roles[0] != "admin" || roles[1] != "editor" {
		t.Fatalf("expected [admin editor], got %v", roles)
	}
}

func TestEntitySessionStore_CreateAndGet(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewEntitySessionStore(db, "sessions")
	ctx := context.Background()

	sess, err := store.Create(ctx, "user-1", time.Hour)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sess.Token == "" {
		t.Fatal("expected non-empty token")
	}
	if sess.UserID != "user-1" {
		t.Fatalf("expected user-1, got %q", sess.UserID)
	}

	// Get
	found, err := store.Get(ctx, sess.Token)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if found.UserID != "user-1" {
		t.Fatalf("expected user-1, got %q", found.UserID)
	}
}

func TestEntitySessionStore_GetNotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewEntitySessionStore(db, "sessions")
	_, err := store.Get(context.Background(), "nonexistent")
	if err != ErrSessionNotFound {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestEntitySessionStore_Delete(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewEntitySessionStore(db, "sessions")
	ctx := context.Background()

	sess, _ := store.Create(ctx, "user-1", time.Hour)

	if err := store.Delete(ctx, sess.Token); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := store.Get(ctx, sess.Token)
	if err != ErrSessionNotFound {
		t.Fatalf("expected ErrSessionNotFound after delete, got %v", err)
	}
}

// insertExpiredSession bypasses Create (which now rejects ttl<=0) to plant
// an already-expired row for tests that exercise expiry handling.
func insertExpiredSession(t *testing.T, store *EntitySessionStore, userID string) string {
	t.Helper()
	tok, err := newSessionToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	now := time.Now()
	expired := now.Add(-time.Hour)
	q := store.qTable("INSERT INTO %s (token, user_id, created_at, expires_at) VALUES ($1, $2, $3, $4)")
	if _, err := store.db.ExecContext(context.Background(), q, tok, userID, now, expired); err != nil {
		t.Fatalf("insert expired: %v", err)
	}
	return tok
}

func TestEntitySessionStore_ExpiredSession(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewEntitySessionStore(db, "sessions")
	tok := insertExpiredSession(t, store, "user-1")

	_, err := store.Get(context.Background(), tok)
	if err != ErrSessionNotFound {
		t.Fatalf("expected ErrSessionNotFound for expired session, got %v", err)
	}
}

func TestEntitySessionStore_Cleanup(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewEntitySessionStore(db, "sessions")
	ctx := context.Background()

	// One expired, one fresh
	insertExpiredSession(t, store, "user-1")
	fresh, _ := store.Create(ctx, "user-2", time.Hour)

	n, err := store.Cleanup(ctx)
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 cleaned up, got %d", n)
	}

	// Fresh session should still exist
	_, err = store.Get(ctx, fresh.Token)
	if err != nil {
		t.Fatalf("fresh session should remain: %v", err)
	}
}

func TestUserEntityFields(t *testing.T) {
	fields := UserEntityFields()
	if len(fields) != 4 {
		t.Fatalf("expected 4 fields (email, password_hash, roles, password_set), got %d", len(fields))
	}
	names := make(map[string]bool)
	for _, f := range fields {
		names[f.Name] = true
	}
	for _, want := range []string{"email", "password_hash", "roles", "password_set"} {
		if !names[want] {
			t.Fatalf("missing expected field %q", want)
		}
	}
}

func TestSessionEntityFields(t *testing.T) {
	fields := SessionEntityFields()
	if len(fields) != 6 {
		t.Fatalf("expected 6 fields (incl. two_factor_verified + pending_two_factor), got %d", len(fields))
	}
}

// TestEntitySessionStore_ImplementsSessionTwoFAMarker pins the contract
// that production-grade session stores can participate in 2FA enforcement.
// Without this, RequireTwoFA fails closed for every EntitySessionStore-
// backed deployment because markSessionTwoFA falls through silently.
func TestEntitySessionStore_ImplementsSessionTwoFAMarker(t *testing.T) {
	var iface any = (*EntitySessionStore)(nil)
	if _, ok := iface.(SessionTwoFAMarker); !ok {
		t.Fatalf("*EntitySessionStore must implement SessionTwoFAMarker for RequireTwoFA to function")
	}
}

// TestEntitySessionStore_MarkAndGetTwoFA exercises the round-trip:
// MarkTwoFactorVerified flips the persisted column, Get returns the
// updated session.
func TestEntitySessionStore_MarkAndGetTwoFA(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewEntitySessionStore(db, "sessions")
	ctx := context.Background()

	sess, err := store.Create(ctx, "user-x", time.Hour)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sess.TwoFactorVerified {
		t.Fatalf("new session must start TwoFactorVerified=false")
	}

	if err := store.MarkTwoFactorVerified(ctx, sess.Token); err != nil {
		t.Fatalf("MarkTwoFactorVerified: %v", err)
	}

	loaded, err := store.Get(ctx, sess.Token)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !loaded.TwoFactorVerified {
		t.Fatalf("after MarkTwoFactorVerified, Get must return TwoFactorVerified=true")
	}
}

// TestEntitySessionStore_RequireTwoFA_EndToEnd is the integration test:
// using the DB-backed store, after a successful /2fa/challenge the user
// can pass RequireTwoFA. Without P0-2, this test fails because
// markSessionTwoFA is a no-op.
func TestEntitySessionStore_RequireTwoFA_EndToEnd(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	mgr := New(AuthConfig{
		JWTSecret:     "test-secret", // prod-mode Init fails closed without one
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     newMemoryUserStore(),
		SessionStore:  NewEntitySessionStore(db, "sessions"),
	})
	twofa := NewTwoFAPlugin(TwoFAConfig{})
	mgr.Use(NewCorePlugin())
	mgr.Use(twofa)
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	userID := "user-x"
	secret := GenerateSecret()
	if err := twofa.store.SetTwoFA(context.Background(), userID, &TwoFAState{
		Enabled: true, Secret: secret, Verified: true,
	}); err != nil {
		t.Fatalf("SetTwoFA: %v", err)
	}

	sess, err := mgr.SessionStore().Create(context.Background(), userID, time.Hour)
	if err != nil {
		t.Fatalf("session create: %v", err)
	}

	r := router.New()
	mgr.RegisterRoutes(r)
	r.Get("/protected", twofa.RequireTwoFA()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).(http.HandlerFunc))

	// Submit valid TOTP via /2fa/challenge
	step := uint64(time.Now().Unix()) / 30
	code := GenerateTOTP(secret, step)
	body := []byte(`{"code":"` + code + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/auth/2fa/challenge", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sess.Token})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("challenge: expected 200, got %d (body=%s)", w.Code, w.Body.String())
	}

	// Now hit RequireTwoFA — must succeed because EntitySessionStore.MarkTwoFactorVerified ran.
	req2 := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req2.AddCookie(&http.Cookie{Name: "session_id", Value: sess.Token})
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("RequireTwoFA: expected 200 after challenge with EntitySessionStore, got %d (body=%s)", w2.Code, w2.Body.String())
	}
}

// TestSessionTTL_DefaultIsNonZero pins the AuthConfig contract:
// without explicit SessionTTL, defaults() must yield a usable lifetime.
// Today defaults() leaves SessionTTL=0, which silently gives every
// EntitySessionStore-backed login an already-expired session.
func TestSessionTTL_DefaultIsNonZero(t *testing.T) {
	cfg := AuthConfig{}
	cfg.defaults()
	if cfg.SessionTTL <= 0 {
		t.Fatalf("SessionTTL after defaults() must be > 0; got %v", cfg.SessionTTL)
	}
}

// TestEntitySessionStore_RejectsNonPositiveTTL pins the EntitySessionStore
// contract: a TTL of 0 (or less) is a programming error, not a request to
// create an instantly-expired session.
func TestEntitySessionStore_RejectsNonPositiveTTL(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewEntitySessionStore(db, "sessions")
	_, err := store.Create(context.Background(), "user-1", 0)
	if err == nil {
		t.Fatalf("Create(ttl=0) must error; previous behavior silently created an expired session")
	}
	_, err = store.Create(context.Background(), "user-1", -time.Second)
	if err == nil {
		t.Fatalf("Create(ttl<0) must error")
	}
}

// EntityUserStore.CreateUser must distinguish "email already exists"
// from generic DB errors so callers can return 409 vs 500 correctly.

// TestEntityUserStore_HasPasswordDistinguishesNoPasswordCreate pins the
// PasswordChecker contract: CreateUser ⇒ password_set=true ⇒ HasPassword
// returns true; CreateUserNoPassword ⇒ password_set=false ⇒ HasPassword
// returns false. AccountsPlugin's unlink guard relies on this.
func TestEntityUserStore_HasPasswordDistinguishesNoPasswordCreate(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewEntityUserStore(db, "users")
	ctx := context.Background()

	hash, _ := HashPassword("real-password")
	pwUser, err := store.CreateUser(ctx, "pw@test.com", hash, []string{"user"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	oauthUser, err := store.CreateUserNoPassword(ctx, "oauth@test.com", []string{"user"})
	if err != nil {
		t.Fatalf("CreateUserNoPassword: %v", err)
	}

	gotPw, err := store.HasPassword(ctx, pwUser.GetID())
	if err != nil {
		t.Fatalf("HasPassword(pw): %v", err)
	}
	if !gotPw {
		t.Fatal("CreateUser must yield HasPassword=true")
	}

	gotOAuth, err := store.HasPassword(ctx, oauthUser.GetID())
	if err != nil {
		t.Fatalf("HasPassword(oauth): %v", err)
	}
	if gotOAuth {
		t.Fatal("CreateUserNoPassword must yield HasPassword=false")
	}
}

// TestEntityUserStore_SetPasswordFlipsHasPassword pins that the password
// reset flow flips the flag: an OAuth-only user who completes a password
// reset gains a real password and HasPassword reports true.
func TestEntityUserStore_SetPasswordFlipsHasPassword(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewEntityUserStore(db, "users")
	ctx := context.Background()

	u, err := store.CreateUserNoPassword(ctx, "oauth@test.com", []string{"user"})
	if err != nil {
		t.Fatalf("CreateUserNoPassword: %v", err)
	}
	has, _ := store.HasPassword(ctx, u.GetID())
	if has {
		t.Fatal("precondition: expected HasPassword=false")
	}

	newHash, _ := HashPassword("new-real-password")
	if err := store.SetPassword(ctx, u.GetID(), newHash); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}

	hasNow, err := store.HasPassword(ctx, u.GetID())
	if err != nil {
		t.Fatalf("HasPassword post-SetPassword: %v", err)
	}
	if !hasNow {
		t.Fatal("SetPassword must flip HasPassword to true")
	}

	if err := store.SetPassword(ctx, "no-such-user", newHash); err != ErrUserNotFound {
		t.Fatalf("SetPassword for missing user must return ErrUserNotFound; got %v", err)
	}
}

func TestEntityUserStore_DuplicateEmail_ReturnsErrEmailTaken(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewEntityUserStore(db, "users")
	ctx := context.Background()
	hash, _ := HashPassword("pwlong123")

	_, err := store.CreateUser(ctx, "alice@test.com", hash, []string{"user"})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err = store.CreateUser(ctx, "alice@test.com", hash, []string{"user"})
	if err == nil {
		t.Fatalf("second create with same email must error")
	}
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("expected ErrEmailTaken, got %v", err)
	}
}

// TestEntityStoreListUsersPaginates pins the UserLister contract on
// EntityUserStore: stable email-ordered pages, an accurate total
// count, and roles round-tripped through parseRoles. SELECTs only
// id/email/roles — password_hash never appears in the listing.
func TestEntityStoreListUsersPaginates(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewEntityUserStore(db, "users")
	ctx := context.Background()
	hash, _ := HashPassword("password123")

	// Emails sort lexicographically; seed in shuffled order to prove
	// the ORDER BY email (not insertion order) drives paging.
	emails := []string{"u2@x.com", "u0@x.com", "u4@x.com", "u1@x.com", "u3@x.com"}
	for _, e := range emails {
		if _, err := store.CreateUser(ctx, e, hash, []string{"editor", "viewer"}); err != nil {
			t.Fatalf("CreateUser %s: %v", e, err)
		}
	}

	// Page 1 (offset 0, limit 2) → u0, u1.
	page1, total, err := store.ListUsers(ctx, ListUsersOptions{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("ListUsers page1: %v", err)
	}
	if total != 5 {
		t.Fatalf("total = %d, want 5", total)
	}
	if len(page1) != 2 || page1[0].GetEmail() != "u0@x.com" || page1[1].GetEmail() != "u1@x.com" {
		t.Fatalf("page1 = %v", emailsOf(page1))
	}

	// Page 2 (offset 2, limit 2) → u2, u3.
	page2, _, err := store.ListUsers(ctx, ListUsersOptions{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("ListUsers page2: %v", err)
	}
	if len(page2) != 2 || page2[0].GetEmail() != "u2@x.com" || page2[1].GetEmail() != "u3@x.com" {
		t.Fatalf("page2 = %v", emailsOf(page2))
	}

	// Page 3 (offset 4, limit 2) → u4 only (last partial page).
	page3, total3, err := store.ListUsers(ctx, ListUsersOptions{Limit: 2, Offset: 4})
	if err != nil {
		t.Fatalf("ListUsers page3: %v", err)
	}
	if total3 != 5 {
		t.Fatalf("total3 = %d, want 5", total3)
	}
	if len(page3) != 1 || page3[0].GetEmail() != "u4@x.com" {
		t.Fatalf("page3 = %v", emailsOf(page3))
	}

	// Roles round-trip.
	if r := page1[0].GetRoles(); len(r) != 2 || r[0] != "editor" || r[1] != "viewer" {
		t.Fatalf("roles = %v, want [editor viewer]", r)
	}

	// Offset past the end → empty page, total unchanged.
	empty, totalE, err := store.ListUsers(ctx, ListUsersOptions{Limit: 10, Offset: 100})
	if err != nil {
		t.Fatalf("ListUsers past-end: %v", err)
	}
	if len(empty) != 0 || totalE != 5 {
		t.Fatalf("past-end page = %v (len %d), total %d", emailsOf(empty), len(empty), totalE)
	}
}

func emailsOf(users []User) []string {
	out := make([]string, len(users))
	for i, u := range users {
		out[i] = u.GetEmail()
	}
	return out
}
