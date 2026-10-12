package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// A password reset revoked the user's sessions but left their API tokens
// live. A token minted by whoever held the account before the reset kept
// working afterwards, so the reset did not lock the attacker out. The
// reset now revokes every token the user owns, next to the sessions.
func TestPasswordResetRevokesAPITokens(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)
	defer db.Close()
	tokens, err := NewSQLAPITokenStore(db)
	if err != nil {
		t.Fatalf("token store: %v", err)
	}
	store := newUserStoreWithPassword()
	mgr := New(AuthConfig{
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     store,
		DevMode:       true,
	})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewTokensPlugin(tokens))
	sender := &stubEmailSender{}
	mgr.Use(NewPasswordResetPlugin(PasswordResetConfig{
		BaseURL:     "http://localhost",
		EmailSender: sender,
	}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	oldHash, _ := HashPassword("oldpw123")
	user := &BasicUser{ID: "u-9", Email: "v@example.com", Roles: []string{"user"}}
	store.users["v@example.com"] = &storeEntry{user: user, hash: oldHash}
	store.byID[user.ID] = store.users["v@example.com"]

	// The attacker minted a token while holding the account; a bystander's
	// token must survive the victim's reset.
	_, stolen, err := IssueToken(ctx, tokens, TokenSpec{Name: "x", OwnerKind: "user", OwnerID: user.ID, Scopes: []string{"a:read"}})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	_, other, err := IssueToken(ctx, tokens, TokenSpec{Name: "y", OwnerKind: "user", OwnerID: "u-other", Scopes: []string{"a:read"}})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	r := router.New()
	mgr.RegisterRoutes(r)
	post := func(path string, body any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	if w := post("/auth/forgot-password", map[string]string{"email": "v@example.com"}); w.Code != http.StatusOK {
		t.Fatalf("forgot-password: %d", w.Code)
	}
	_, emailBody := sender.snapshot()
	tok := extractTokenFromBody(emailBody)
	if w := post("/auth/reset-password", map[string]string{"token": tok, "password": "brandnewpw1"}); w.Code != http.StatusOK {
		t.Fatalf("reset-password: %d %s", w.Code, w.Body.String())
	}

	live := func(owner, id string) bool {
		list, err := tokens.List(ctx, "user", owner)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, x := range list {
			if x.ID == id {
				return x.RevokedAt == nil
			}
		}
		t.Fatalf("token %s missing", id)
		return false
	}
	if live(user.ID, stolen.ID) {
		t.Fatal("the user's API token survived the password reset")
	}
	if !live("u-other", other.ID) {
		t.Fatal("another user's API token was revoked")
	}
}
