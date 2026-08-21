package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// These tests pin graceful rotation for AuthConfig.JWTSecret, mirroring the
// CSRF AdditionalKeys idiom and the uihost session-token rotation. New JWTs
// are signed with JWTSecret; tokens signed by a key listed in
// JWTPreviousSecrets still validate for a drain window (one token TTL), so a
// JWTSecret rotation no longer invalidates every session at once.

func jwtRotationUser() User {
	return &BasicUser{ID: "user-1", Email: "alice@example.com", Roles: []string{"user"}}
}

// TestJWT_KeyRotationAcceptsPreviousSecret: a JWT signed by the OLD secret
// still validates once that secret is listed in PreviousSecrets alongside the
// new current secret.
func TestJWT_KeyRotationAcceptsPreviousSecret(t *testing.T) {
	oldSecret := "old-signing-secret-aaaaaaaaaaaaa"
	newSecret := "new-signing-secret-bbbbbbbbbbbbb"

	preRotation := NewJWTAuth(oldSecret, time.Hour)
	tok, err := preRotation.GenerateToken(jwtRotationUser())
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	rotated := NewJWTAuth(newSecret, time.Hour)
	rotated.PreviousSecrets = []string{oldSecret}
	if _, err := rotated.ValidateToken(tok); err != nil {
		t.Fatalf("rotated JWTAuth rejected a token signed by the previous secret: %v", err)
	}
}

// TestJWT_KeyRotationRejectsWhenPreviousMissing: a token whose secret is
// neither current nor listed as previous is rejected, the drain window
// closes.
func TestJWT_KeyRotationRejectsWhenPreviousMissing(t *testing.T) {
	oldSecret := "old-signing-secret-aaaaaaaaaaaaa"
	newSecret := "new-signing-secret-bbbbbbbbbbbbb"

	preRotation := NewJWTAuth(oldSecret, time.Hour)
	tok, err := preRotation.GenerateToken(jwtRotationUser())
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	retired := NewJWTAuth(newSecret, time.Hour) // no PreviousSecrets
	if _, err := retired.ValidateToken(tok); err == nil {
		t.Fatal("JWTAuth with only the new secret accepted a token signed by the retired secret")
	}
}

// TestJWT_KeyRotationSignsWithCurrent: a freshly-minted JWT (signed by the
// current secret) validates against a JWTAuth that knows only the current
// secret, the operator can drop the old key once the window drains.
func TestJWT_KeyRotationSignsWithCurrent(t *testing.T) {
	newSecret := "new-signing-secret-bbbbbbbbbbbbb"

	rotated := NewJWTAuth(newSecret, time.Hour)
	tok, err := rotated.GenerateToken(jwtRotationUser())
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	currentOnly := NewJWTAuth(newSecret, time.Hour)
	claims, err := currentOnly.ValidateToken(tok)
	if err != nil {
		t.Fatalf("post-rotation JWT did not validate against current-only: %v", err)
	}
	if claims.UserID != "user-1" {
		t.Fatalf("claims UserID = %q, want user-1", claims.UserID)
	}

	// And it must NOT validate once its signing secret is fully retired
	// (neither current nor listed previous).
	third := "third-signing-secret-ccccccccccccc"
	fullyRetired := NewJWTAuth(third, time.Hour)
	fullyRetired.PreviousSecrets = []string{"some-other-retired-secret-dddddddddddddd"}
	if _, err := fullyRetired.ValidateToken(tok); err == nil {
		t.Fatal("current-signed JWT validated after its secret was fully retired from current+previous")
	}
}

// TestJWT_KeyRotationMultiplePreviousSecrets: two prior secrets (a long
// rollout) must each still validate.
func TestJWT_KeyRotationMultiplePreviousSecrets(t *testing.T) {
	newSecret := "new-signing-secret-bbbbbbbbbbbbb"
	secretA := "previous-secret-a-aaaaaaaaaaaa"
	secretB := "previous-secret-b-bbbbbbbbbbb"

	rotated := NewJWTAuth(newSecret, time.Hour)
	rotated.PreviousSecrets = []string{secretA, secretB}

	for _, old := range []string{secretA, secretB} {
		oldAuth := NewJWTAuth(old, time.Hour)
		tok, err := oldAuth.GenerateToken(jwtRotationUser())
		if err != nil {
			t.Fatalf("GenerateToken: %v", err)
		}
		if _, err := rotated.ValidateToken(tok); err != nil {
			t.Fatalf("JWT signed by a previous secret in the multi-secret set was rejected: %v", err)
		}
	}
}

// TestJWT_RotationCoversRequireAuthMiddleware: the production verify path is
// RequireAuth middleware, which calls (*JWTAuth).ValidateToken. Pin that a
// previous-secret token passes through the middleware during the drain window.
func TestJWT_RotationCoversRequireAuthMiddleware(t *testing.T) {
	oldSecret := "old-signing-secret-aaaaaaaaaaaaa"
	newSecret := "new-signing-secret-bbbbbbbbbbbbb"

	preRotation := NewJWTAuth(oldSecret, time.Hour)
	tok, err := preRotation.GenerateToken(jwtRotationUser())
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	rotated := NewJWTAuth(newSecret, time.Hour)
	rotated.PreviousSecrets = []string{oldSecret}
	mw := RequireAuth(rotated)

	reached := false
	inner := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { reached = true })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	mw(inner).ServeHTTP(rec, req)
	if !reached {
		t.Fatalf("RequireAuth rejected a previous-secret JWT during the drain window: status %d", rec.Code)
	}
}

// TestJWTConfig_RotationWiredFromAuthConfig: AuthConfig.JWTPreviousSecrets
// flows through Init into the JWTAuth used by the manager.
func TestJWTConfig_RotationWiredFromAuthConfig(t *testing.T) {
	mgr := New(AuthConfig{
		DevMode:            true,
		JWTSecret:          "current-config-secret-eeeeee",
		JWTPreviousSecrets: []string{"previous-config-secret-fffff"},
	})
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	ja := mgr.JWT()
	if ja == nil {
		t.Fatal("JWT() nil after Init")
	}
	if len(ja.PreviousSecrets) != 1 || ja.PreviousSecrets[0] != "previous-config-secret-fffff" {
		t.Fatalf("PreviousSecrets not wired from AuthConfig: %v", ja.PreviousSecrets)
	}

	// A token signed by the configured previous secret validates through the manager.
	oldAuth := NewJWTAuth("previous-config-secret-fffff", time.Hour)
	tok, err := oldAuth.GenerateToken(jwtRotationUser())
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if _, err := ja.ValidateToken(tok); err != nil {
		t.Fatalf("manager JWTAuth rejected a previous-config-secret token: %v", err)
	}
}

// TestJWTConfig_ProdModeRejectsPreviousOnly: production mode with an empty
// JWTSecret must fail closed even when JWTPreviousSecrets is set, a
// verify-only configuration is not a valid signing setup.
func TestJWTConfig_ProdModeRejectsPreviousOnly(t *testing.T) {
	mgr := New(AuthConfig{
		DevMode:            false,
		JWTPreviousSecrets: []string{"a-retired-secret-gggggggggggg"},
	})
	err := mgr.Init(nil)
	if err == nil {
		t.Fatal("Init must fail closed when JWTSecret is empty, even with JWTPreviousSecrets set")
	}
	if !strings.Contains(err.Error(), "JWTSecret") {
		t.Fatalf("error should mention JWTSecret, got: %v", err)
	}
}

// TestJWTConfig_DevModeMintsCurrentIgnoresPreviousOnly: in DevMode with no
// JWTSecret, a random current is minted; a previous-only list is ignored
// rather than crashing (no current would be derived from it).
func TestJWTConfig_DevModeNoCurrentIgnoresPreviousOnly(t *testing.T) {
	mgr := New(AuthConfig{
		DevMode:            true,
		JWTPreviousSecrets: []string{"orphan-previous-secret-hhhhhhh"},
	})
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	ja := mgr.JWT()
	if ja == nil || ja.Secret == "" {
		t.Fatal("DevMode minted no current JWTSecret")
	}
}
