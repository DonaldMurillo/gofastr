package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// testUser is a helper User implementation for tests.
type testUser struct {
	id    string
	email string
	roles []string
}

func (u *testUser) GetID() string      { return u.id }
func (u *testUser) GetEmail() string   { return u.email }
func (u *testUser) GetRoles() []string { return u.roles }

func TestHashAndCheckPassword(t *testing.T) {
	password := "super-secret-123!"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if hash == "" {
		t.Fatal("HashPassword returned empty hash")
	}
	if hash == password {
		t.Fatal("HashPassword returned the plaintext password")
	}

	if !CheckPassword(password, hash) {
		t.Fatal("CheckPassword should return true for correct password")
	}
	if CheckPassword("wrong-password", hash) {
		t.Fatal("CheckPassword should return false for incorrect password")
	}
}

func TestJWTGenerateAndValidate(t *testing.T) {
	jwt := NewJWTAuth("test-secret-key", 1*time.Hour)
	user := &testUser{id: "user-1", email: "alice@example.com", roles: []string{"admin", "editor"}}

	token, err := jwt.GenerateToken(user)
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	if token == "" {
		t.Fatal("GenerateToken returned empty token")
	}

	claims, err := jwt.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken returned error: %v", err)
	}
	if claims.UserID != "user-1" {
		t.Errorf("expected UserID 'user-1', got %q", claims.UserID)
	}
	if claims.Email != "alice@example.com" {
		t.Errorf("expected Email 'alice@example.com', got %q", claims.Email)
	}
	if len(claims.Roles) != 2 || claims.Roles[0] != "admin" || claims.Roles[1] != "editor" {
		t.Errorf("expected roles [admin editor], got %v", claims.Roles)
	}
}

func TestJWTExpiredTokenRejected(t *testing.T) {
	jwt := NewJWTAuth("test-secret-key", -1*time.Second) // already expired
	user := &testUser{id: "user-1", email: "alice@example.com", roles: []string{"admin"}}

	token, err := jwt.GenerateToken(user)
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}

	_, err = jwt.ValidateToken(token)
	if err == nil {
		t.Fatal("ValidateToken should reject expired tokens")
	}
}

func TestJWTInvalidSignature(t *testing.T) {
	jwt1 := NewJWTAuth("secret-A", 1*time.Hour)
	jwt2 := NewJWTAuth("secret-B", 1*time.Hour)
	user := &testUser{id: "user-1", email: "alice@example.com", roles: []string{"admin"}}

	token, err := jwt1.GenerateToken(user)
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}

	_, err = jwt2.ValidateToken(token)
	if err == nil {
		t.Fatal("ValidateToken should reject token with wrong signature")
	}
}

func TestRequireAuthRejectsMissingToken(t *testing.T) {
	jwt := NewJWTAuth("test-secret-key", 1*time.Hour)
	called := false

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	mw := RequireAuth(jwt)
	srv := mw(inner)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if called {
		t.Fatal("inner handler should not be called when token is missing")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rec.Code)
	}
}

func TestRequireAuthAcceptsValidToken(t *testing.T) {
	jwt := NewJWTAuth("test-secret-key", 1*time.Hour)
	user := &testUser{id: "user-1", email: "alice@example.com", roles: []string{"admin"}}
	token, err := jwt.GenerateToken(user)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	var gotUser User
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = GetCurrentUser(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	mw := RequireAuth(jwt)
	srv := mw(inner)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if gotUser == nil {
		t.Fatal("expected user in context, got nil")
	}
	if gotUser.GetID() != "user-1" {
		t.Errorf("expected user ID 'user-1', got %q", gotUser.GetID())
	}
}

func TestRequireRoleRejectsWrongRole(t *testing.T) {
	user := &testUser{id: "user-1", email: "alice@example.com", roles: []string{"editor"}}
	called := false

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	mw := RequireRole("admin")
	srv := mw(inner)

	// Set user in context via handler package SetUser
	req := httptest.NewRequest(http.MethodGet, "/admin-only", nil)
	ctx := handler.SetUser(req.Context(), user)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if called {
		t.Fatal("inner handler should not be called for wrong role")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", rec.Code)
	}
}

func TestRequireRoleAcceptsCorrectRole(t *testing.T) {
	user := &testUser{id: "user-1", email: "alice@example.com", roles: []string{"admin", "editor"}}
	called := false

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	mw := RequireRole("admin")
	srv := mw(inner)

	req := httptest.NewRequest(http.MethodGet, "/admin-only", nil)
	ctx := handler.SetUser(req.Context(), user)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if !called {
		t.Fatal("inner handler should be called for correct role")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestGetCurrentUserNil(t *testing.T) {
	ctx := context.Background()
	u := GetCurrentUser(ctx)
	if u != nil {
		t.Fatal("expected nil user from empty context")
	}
}

func TestGenerateTokenNilUser(t *testing.T) {
	jwt := NewJWTAuth("secret", 1*time.Hour)
	_, err := jwt.GenerateToken(nil)
	if err == nil {
		t.Fatal("expected error for nil user")
	}
}
