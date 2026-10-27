package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// Claims represents the data embedded in a JWT token.
type Claims struct {
	UserID    string
	Email     string
	Roles     []string
	ExpiresAt time.Time
	IssuedAt  time.Time

	// cred is the credential tag the token was issued under; see
	// credentialTag.
	cred string
}

// JWTAuth manages JWT token generation and validation.
type JWTAuth struct {
	// Secret is the current signing key; new tokens are always signed
	// with it.
	Secret string
	// PreviousSecrets are verify-only signing keys retained for a drain
	// window after a JWTSecret rotation, mirroring the CSRF AdditionalKeys
	// idiom. Tokens signed by any key here still validate; the operator
	// drops them once old tokens have expired. GenerateToken never signs
	// with these.
	PreviousSecrets []string
	Expiry          time.Duration
	Issuer          string

	// users, when set, is the store RequireAuth re-resolves every token
	// subject against, so deletion, erasure, and role downgrades reach
	// outstanding JWTs at request time instead of at token TTL. Set by
	// AuthManager.Init; a bare NewJWTAuth (service-to-server deployments
	// with no user table) leaves it nil and RequireAuth falls back to the
	// historical claims-derived principal.
	users UserStore

	// onResolve, set by AuthManager.Init, tells the manager a resolving
	// middleware exists when RequireAuth is built over this helper.
	onResolve func()
}

// NewJWTAuth creates a new JWTAuth with the given secret and token expiry duration.
func NewJWTAuth(secret string, expiry time.Duration) *JWTAuth {
	return &JWTAuth{
		Secret: secret,
		Expiry: expiry,
		Issuer: "gofastr",
	}
}

// SetUserStore wires the user store whose fresh row becomes the request
// principal. Identity AND roles are taken from the row; the token's copy is
// trusted for nothing but the subject. Call before serving requests;
// AuthManager.Init does it for manager-built JWTAuths.
func (j *JWTAuth) SetUserStore(users UserStore) {
	j.users = users
}

// GenerateToken creates a signed JWT token for the given user.
// The token contains the user's ID, email, and roles. With a user store
// wired, it is also bound to the user's current password hash (see
// credentialTag), read from the store; a user the store cannot find gets
// no token.
func (j *JWTAuth) GenerateToken(user User) (string, error) {
	if user == nil {
		return "", ErrUnauthorized
	}
	hash := ""
	if j.users != nil {
		h, err := j.storedHash(context.Background(), user)
		if err != nil {
			return "", err
		}
		hash = h
	}
	return j.generateToken(user, hash)
}

// generateToken signs a token for user bound to hash, the password hash
// the caller verified. Login passes the hash it checked, so a reset that
// lands between the check and the signing leaves the token already dead.
func (j *JWTAuth) generateToken(user User, hash string) (string, error) {
	if user == nil {
		return "", ErrUnauthorized
	}

	now := time.Now()
	claims := Claims{
		UserID:    user.GetID(),
		Email:     user.GetEmail(),
		Roles:     user.GetRoles(),
		ExpiresAt: now.Add(j.Expiry),
		IssuedAt:  now,
	}
	if j.users != nil {
		claims.cred = credentialTag(j.Secret, claims.UserID, hash)
	}

	return encodeToken(j.Secret, j.Issuer, claims)
}

// credentialTag binds a token to the credential it was issued under: an
// HMAC, under the signing key, of the user id and the stored password
// hash. Every event that must end outstanding JWTs writes a new hash: a
// password change or reset (bcrypt salts every hash), and an account claim
// (the squatter's password becomes the placeholder). resolveOwner
// recomputes the tag from the fresh row, so a token issued before any of
// them fails on its next request. The tag is keyed, so the payload, which
// anyone holding the token can read, says nothing about the hash.
func credentialTag(secret, userID, hash string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("gofastr-jwt-credential\x00"))
	mac.Write([]byte(userID))
	mac.Write([]byte{0})
	mac.Write([]byte(hash))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:16])
}

// storedHash reads user's current password hash. UserStore returns a
// hash only from FindByEmail, so the row is looked up by the user's
// email and must be the same user.
func (j *JWTAuth) storedHash(ctx context.Context, user User) (string, error) {
	row, hash, err := j.users.FindByEmail(ctx, user.GetEmail())
	if err != nil {
		return "", fmt.Errorf("%w: credential lookup: %v", ErrUnauthorized, err)
	}
	if row == nil || row.GetID() != user.GetID() {
		return "", fmt.Errorf("%w: credential lookup found another user", ErrUnauthorized)
	}
	return hash, nil
}

// credentialCurrent reports whether c was issued under user's current
// password hash, under the signing key or any rotation key.
func (j *JWTAuth) credentialCurrent(ctx context.Context, c Claims, user User) bool {
	if c.cred == "" {
		return false
	}
	hash, err := j.storedHash(ctx, user)
	if err != nil {
		return false
	}
	ok := false
	for _, secret := range append([]string{j.Secret}, j.PreviousSecrets...) {
		if secret == "" {
			continue
		}
		if hmac.Equal([]byte(c.cred), []byte(credentialTag(secret, user.GetID(), hash))) {
			ok = true
		}
	}
	return ok
}

// ValidateToken parses and validates a JWT token string. It accepts the
// current signing key OR any previous (rotation) key, so a JWTSecret
// rotation drains over a token TTL instead of invalidating every session at
// once. Each candidate signature is compared with hmac.Equal via
// decodeToken (constant-time); the count and order of keys is not secret, so
// first-match-return is sound. Returns the claims if valid, or an error.
func (j *JWTAuth) ValidateToken(tokenString string) (Claims, error) {
	// Try the current secret first, then each previous key. decodeToken is
	// a full parse (signature + header + payload + issuer), and only a key
	// that produces a valid signature can pass it, so re-parsing per key is
	// safe and bounded by the (small) rotation set.
	candidates := make([]string, 0, 1+len(j.PreviousSecrets))
	candidates = append(candidates, j.Secret)
	candidates = append(candidates, j.PreviousSecrets...)

	var claims Claims
	matched := false
	for _, secret := range candidates {
		if secret == "" {
			continue
		}
		c, err := decodeToken(secret, j.Issuer, tokenString)
		if err == nil {
			claims = c
			matched = true
			break
		}
	}
	if !matched {
		return Claims{}, fmt.Errorf("%w: no matching signing key", ErrUnauthorized)
	}

	if claims.UserID == "" {
		return Claims{}, fmt.Errorf("%w: token has empty subject", ErrUnauthorized)
	}

	now := time.Now()
	if now.After(claims.ExpiresAt) {
		return Claims{}, fmt.Errorf("%w: token expired", ErrUnauthorized)
	}
	// Reject tokens that claim to have been issued in the future. Allow
	// a small skew for clock drift between issuer and verifier.
	if !claims.IssuedAt.IsZero() && claims.IssuedAt.After(now.Add(2*time.Minute)) {
		return Claims{}, fmt.Errorf("%w: token issued in the future", ErrUnauthorized)
	}

	return claims, nil
}

// resolveOwner loads the principal for a valid token. With a user store
// wired (SetUserStore / AuthManager.Init) the subject is re-resolved via
// FindByID on EVERY request, and identity AND roles come from the fresh
// row — a deleted/erased owner, or one whose roles were downgraded, fails
// closed immediately instead of at token TTL, mirroring resolveTokenOwner
// (API tokens) and resolveSessionUser (sessions). A lookup error is
// treated as owner-missing (fail closed), the same posture both siblings
// hold. A JWTAuth with no store — bare NewJWTAuth, service-to-server
// deployments with no user table — keeps the claims-derived principal.
func (j *JWTAuth) resolveOwner(ctx context.Context, c Claims) (User, bool) {
	if j.users == nil {
		return claimsToUser(c), true
	}
	u, err := j.users.FindByID(ctx, c.UserID)
	if err != nil || u == nil {
		return nil, false
	}
	// The subject existing is not enough: a password change, a reset or
	// an account claim since the token was issued ends it.
	if !j.credentialCurrent(ctx, c, u) {
		return nil, false
	}
	return u, true
}

// claimsToUser converts Claims into a User.
func claimsToUser(c Claims) User {
	return &BasicUser{
		ID:    c.UserID,
		Email: c.Email,
		Roles: c.Roles,
	}
}

// userToContext stores the user derived from claims into the context.
func userToContext(ctx context.Context, user User) context.Context {
	return handler.SetUser(ctx, user)
}
