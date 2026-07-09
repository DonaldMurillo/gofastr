package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// TokenPrefix marks every plaintext API token. The "gfsk_" prefix makes
// leaked tokens greppable by secret scanners (GitHub, truffleHog, …) the
// way "ghp_", "xoxb-", and "AKIA…" are.
const TokenPrefix = "gfsk_"

// OwnerKindUser and OwnerKindService are the two valid APIToken.OwnerKind
// values. A token belongs to either a human user (resolved via UserStore)
// or a non-human service account (resolved via ServiceAccountStore).
const (
	OwnerKindUser    = "user"
	OwnerKindService = "service"
)

const (
	maxTokenScopes = 32
	tokenRandBytes = 20 // → 40 hex chars; plaintext is "gfsk_" + 40 = 45 chars
	tokenPrefixLen = 12 // display prefix length (TokenPrefix + 7 hex chars)
)

// APIToken is one scoped, hashed API token (a PAT or a service-account
// credential). Only a sha256 hash of the plaintext is persisted; the
// Prefix field is the first tokenPrefixLen chars of the plaintext so
// listings can identify a token without revealing it.
type APIToken struct {
	ID         string
	Name       string     // human label, required
	OwnerKind  string     // "user" | "service"
	OwnerID    string     // user id or service-account id
	Prefix     string     // display prefix, first 12 chars of plaintext
	Scopes     []string   // e.g. "posts:read"; empty = NO scopes (RequireScope always 403s)
	ExpiresAt  *time.Time // nil = no expiry
	LastUsedAt *time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
}

// APITokenStore persists API tokens. Implementations store ONLY the
// sha256 hash of the plaintext — never the plaintext itself.
type APITokenStore interface {
	Create(ctx context.Context, t APIToken, sha256Hash string) error
	// FindByHash returns (nil, nil) for unknown hashes.
	FindByHash(ctx context.Context, sha256Hash string) (*APIToken, error)
	List(ctx context.Context, ownerKind, ownerID string) ([]APIToken, error)
	// Revoke stamps RevokedAt; idempotent; scoped by owner so one owner
	// cannot revoke another's token. Returns ErrTokenNotFound when no row
	// matches (id, ownerKind, ownerID).
	Revoke(ctx context.Context, id, ownerKind, ownerID string) error
	TouchLastUsed(ctx context.Context, id string, at time.Time) error
}

// ServiceAccount is a non-human identity that authenticates ONLY via API
// tokens (there is deliberately no interactive login path). Its Roles feed
// access.Can / RequireRole through the User interface, exactly like a human
// user's roles.
type ServiceAccount struct {
	ID        string
	Name      string // unique, required
	Roles     []string
	Disabled  bool
	CreatedAt time.Time
}

// ServiceAccountStore persists service accounts.
type ServiceAccountStore interface {
	Create(ctx context.Context, sa ServiceAccount) error
	Get(ctx context.Context, id string) (*ServiceAccount, error) // (nil,nil) unknown
	List(ctx context.Context) ([]ServiceAccount, error)
	SetDisabled(ctx context.Context, id string, disabled bool) error
}

// TokenSpec describes a token to mint.
type TokenSpec struct {
	Name      string
	OwnerKind string // "user" | "service"
	OwnerID   string
	Scopes    []string
	TTL       time.Duration // 0 = no expiry
}

// scopePattern is the closed vocabulary a scope string must match:
// "<resource>:<verb>", each half [a-z0-9_*-]+. Both halves admit "*" so
// the wildcard scopes HasScope/scopeMatches document are actually
// mintable: "posts:*" (any verb on posts), "*:read" (read across every
// resource), "*:*" (grant-all). Compiled once at init.
var scopePattern = regexp.MustCompile(`^[a-z0-9_*-]+:[a-z0-9_*-]+$`)

// validateTokenSpec enforces the IssueToken preconditions. It never sees
// the plaintext token (generated later), so no secret can leak through an
// error string here.
func validateTokenSpec(spec TokenSpec) error {
	if strings.TrimSpace(spec.Name) == "" {
		return errors.New("auth: token name is required")
	}
	if spec.OwnerKind != OwnerKindUser && spec.OwnerKind != OwnerKindService {
		return fmt.Errorf("auth: invalid owner_kind %q (want %q or %q)", spec.OwnerKind, OwnerKindUser, OwnerKindService)
	}
	if strings.TrimSpace(spec.OwnerID) == "" {
		return errors.New("auth: owner_id is required")
	}
	if len(spec.Scopes) > maxTokenScopes {
		return fmt.Errorf("auth: too many scopes (%d > %d)", len(spec.Scopes), maxTokenScopes)
	}
	for _, sc := range spec.Scopes {
		if !scopePattern.MatchString(sc) {
			return fmt.Errorf("auth: invalid scope %q (want resource:verb)", sc)
		}
	}
	return nil
}

// IssueToken mints the plaintext, persists the hashed record, and returns
// (plaintext, record, error). The plaintext is returned exactly ONCE and
// is never persisted, logged, or placed in any error string.
//
// Validation: Name/OwnerKind/OwnerID required; OwnerKind ∈ {user,service};
// scope strings must match ^[a-z0-9_-]+:[a-z0-9_*-]+$; max 32 scopes.
func IssueToken(ctx context.Context, store APITokenStore, spec TokenSpec) (string, APIToken, error) {
	if store == nil {
		return "", APIToken{}, errors.New("auth: IssueToken: store is nil")
	}
	if err := validateTokenSpec(spec); err != nil {
		return "", APIToken{}, err
	}
	plaintext, err := generateAPITokenPlaintext()
	if err != nil {
		return "", APIToken{}, err
	}
	now := time.Now().UTC()
	rec := APIToken{
		ID:        generateAPITokenID(),
		Name:      spec.Name,
		OwnerKind: spec.OwnerKind,
		OwnerID:   spec.OwnerID,
		Prefix:    tokenDisplayPrefix(plaintext),
		Scopes:    spec.Scopes,
		CreatedAt: now,
	}
	if spec.TTL > 0 {
		exp := now.Add(spec.TTL)
		rec.ExpiresAt = &exp
	}
	if err := store.Create(ctx, rec, sha256hex(plaintext)); err != nil {
		return "", APIToken{}, fmt.Errorf("auth: IssueToken: %w", err)
	}
	return plaintext, rec, nil
}

// NewServiceAccount builds a ServiceAccount with a fresh ID and CreatedAt,
// ready for ServiceAccountStore.Create. Service-account management is
// programmatic-only in v1 (no HTTP surface) — hosts call this then Create.
func NewServiceAccount(name string, roles []string) ServiceAccount {
	return ServiceAccount{
		ID:        generateAPITokenID(),
		Name:      name,
		Roles:     roles,
		CreatedAt: time.Now().UTC(),
	}
}

// serviceAccountUser adapts a ServiceAccount to the User interface so a
// token-authenticated service account populates request context the same
// way a session user does — GetRoles returns the account roles (feeding
// RequireRole / access.Can unchanged); GetEmail returns "" (no mailbox).
type serviceAccountUser struct {
	sa *ServiceAccount
}

func (u *serviceAccountUser) GetID() string      { return u.sa.ID }
func (u *serviceAccountUser) GetEmail() string   { return "" }
func (u *serviceAccountUser) GetRoles() []string { return u.sa.Roles }

// ─── Token material ────────────────────────────────────────────────────────

// generateAPITokenPlaintext returns TokenPrefix + 40 lowercase hex chars
// (20 cryptographically-random bytes). Entropy failure is fatal — the auth
// system cannot remain sound without crypto/rand.
func generateAPITokenPlaintext() (string, error) {
	b := make([]byte, tokenRandBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: generate api token entropy: %w", err)
	}
	return TokenPrefix + hex.EncodeToString(b), nil
}

// generateAPITokenID returns a 32-char hex record id from crypto/rand.
func generateAPITokenID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback keeps callers usable; crypto/rand failure is extraordinary.
		return fmt.Sprintf("tok-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// sha256hex returns the lowercase hex sha256 of s. This is the ONLY form
// of the token persisted or compared — hash-then-lookup gives uniform
// timing and means no plaintext is ever string-compared.
func sha256hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// tokenDisplayPrefix returns the first tokenPrefixLen chars of a plaintext
// token (safe to surface in listings/audit). It never panics on short
// input.
func tokenDisplayPrefix(plaintext string) string {
	if len(plaintext) > tokenPrefixLen {
		return plaintext[:tokenPrefixLen]
	}
	return plaintext
}

// ─── Scope context helpers ─────────────────────────────────────────────────

type tokenScopesKey struct{}

// WithTokenScopes stashes a token's scopes in ctx. TokenMiddleware calls
// this on success; request handlers read it via TokenScopes / HasScope.
func WithTokenScopes(ctx context.Context, scopes []string) context.Context {
	return context.WithValue(ctx, tokenScopesKey{}, scopes)
}

// TokenScopes returns (scopes, true) only for token-authenticated requests;
// (nil, false) for sessions/JWT (which carry full user capability and are
// unrestricted by the scope model).
func TokenScopes(ctx context.Context) ([]string, bool) {
	s, ok := ctx.Value(tokenScopesKey{}).([]string)
	return s, ok
}

// HasScope reports whether the request may exercise the given scope. It is
// true when the request is NOT token-authenticated (sessions/JWT carry full
// user capability), or when the token's scopes contain the target or a
// granting wildcard:
//   - exact match "posts:read" grants "posts:read"
//   - "posts:*" grants any "posts:…" verb
//   - "*:*" grants everything
//
// An empty-scopes token HasScope false for everything.
func HasScope(ctx context.Context, scope string) bool {
	held, ok := TokenScopes(ctx)
	if !ok {
		return true // session/JWT — unscoped by design
	}
	return scopeMatches(held, scope)
}

// scopeMatches is the pure wildcard matcher used by HasScope and tested in
// isolation. It does not consult ctx.
func scopeMatches(held []string, want string) bool {
	wantRes, wantVerb, ok := splitScope(want)
	if !ok {
		return false
	}
	for _, s := range held {
		if s == want {
			return true
		}
		res, verb, ok := splitScope(s)
		if !ok {
			continue
		}
		if (res == "*" || res == wantRes) && (verb == "*" || verb == wantVerb) {
			return true
		}
	}
	return false
}

// splitScope parses "resource:verb". ok is false when there is no colon or
// either half is empty.
func splitScope(s string) (resource, verb string, ok bool) {
	idx := strings.IndexByte(s, ':')
	if idx <= 0 || idx == len(s)-1 {
		return "", "", false
	}
	return s[:idx], s[idx+1:], true
}
