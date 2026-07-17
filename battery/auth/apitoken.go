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

	"github.com/DonaldMurillo/gofastr/framework/access"
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

	// Prefix overrides the plaintext marker for this token (default
	// TokenPrefix, "gfsk_"). Hosts brand their credentials — a leaked token's
	// prefix then identifies WHICH product leaked it, and per-product secret
	// scanners can grep for it. Must match tokenPrefixPattern (lowercase
	// alnum, 2–16 chars, trailing underscore). Wire the SAME prefix into
	// TokenMiddleware via WithTokenPrefix or authenticated requests will pass
	// through unrecognized.
	Prefix string
}

// tokenPrefixPattern is the closed vocabulary for a custom token prefix:
// starts with a letter, lowercase alnum, 1–15 chars, then "_".
var tokenPrefixPattern = regexp.MustCompile(`^[a-z][a-z0-9]{0,14}_$`)

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
	if spec.Prefix != "" && !tokenPrefixPattern.MatchString(spec.Prefix) {
		return fmt.Errorf("auth: invalid token prefix %q (want lowercase alnum starting with a letter, 2-16 chars, trailing underscore, e.g. %q)", spec.Prefix, TokenPrefix)
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
	prefix := spec.Prefix
	if prefix == "" {
		prefix = TokenPrefix
	}
	plaintext, err := generateAPITokenPlaintext(prefix)
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
func generateAPITokenPlaintext(prefix string) (string, error) {
	b := make([]byte, tokenRandBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: generate api token entropy: %w", err)
	}
	return prefix + hex.EncodeToString(b), nil
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
type tokenIDKey struct{}

// WithTokenScopes stashes a token's scopes in ctx. TokenMiddleware calls
// this on success; request handlers read it via TokenScopes / HasScope.
func WithTokenScopes(ctx context.Context, scopes []string) context.Context {
	return context.WithValue(ctx, tokenScopesKey{}, scopes)
}

// WithTokenID stashes the authenticating token's ID in ctx. TokenMiddleware
// calls this on success so hosts can attribute a request to the SPECIFIC
// token — per-token metering, quotas, and audit trails need the token
// identity, not just the owner (one owner can hold many tokens).
func WithTokenID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, tokenIDKey{}, id)
}

// TokenID returns (id, true) only for token-authenticated requests;
// ("", false) for sessions/JWT.
func TokenID(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(tokenIDKey{}).(string)
	return id, ok
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

// ScopeMatch reports whether any scope in held grants want, using the
// resource:verb wildcard grammar owned by access.ScopeMatch (exact match,
// or a "*" on either half). Exported so other capability gates (e.g.
// framework/pluginhost) check grant sets against the SAME matcher rather
// than reimplementing a weaker one. This thin adapter exists for the
// []string-typed token-scope call site; new callers holding
// []access.Permission should call access.ScopeMatch directly.
func ScopeMatch(held []string, want string) bool {
	return scopeMatches(held, want)
}

// scopeMatches delegates to access.ScopeMatch so the resource:verb wildcard
// algebra has exactly one home in framework/access. It is kept as a private
// []string-typed adapter so HasScope and the existing scope tests stay
// unchanged; the conversion to []access.Permission is bounded by the
// 32-scope token cap (maxTokenScopes).
func scopeMatches(held []string, want string) bool {
	granted := make([]access.Permission, len(held))
	for i, h := range held {
		granted[i] = access.Permission(h)
	}
	return access.ScopeMatch(granted, access.Permission(want))
}
