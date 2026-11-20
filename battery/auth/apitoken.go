package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/embed"
)

// TokenPrefix is the default brand every plaintext API token starts with.
const TokenPrefix = "gfsk_"

// OwnerKindUser and OwnerKindService are the APIToken.OwnerKind values.
const (
	OwnerKindUser    = "user"
	OwnerKindService = "service"
)

const (
	maxTokenScopes = 32
	tokenRandBytes = 20 // → 40 hex chars; plaintext is "gfsk_" + 40 = 45 chars
	tokenPrefixLen = 12 // display prefix length (TokenPrefix + 7 hex chars)

	tokenDisplayRandLen = 7  // random chars a display prefix keeps after the brand
	maxTokenBrandLen    = 16 // longest brand tokenPrefixPattern admits, "_" included
)

// APIToken is one scoped API token; only a sha256 hash of the plaintext is stored.
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

// APITokenStore stores API tokens by the sha256 hash of their plaintext.
type APITokenStore interface {
	Create(ctx context.Context, t APIToken, sha256Hash string) error
	// FindByHash returns (nil, nil) for unknown hashes.
	FindByHash(ctx context.Context, sha256Hash string) (*APIToken, error)
	List(ctx context.Context, ownerKind, ownerID string) ([]APIToken, error)
	// Revoke stamps RevokedAt on the owner's token, idempotently, or returns ErrTokenNotFound.
	Revoke(ctx context.Context, id, ownerKind, ownerID string) error
	TouchLastUsed(ctx context.Context, id string, at time.Time) error
}

// ServiceAccount is a non-human identity that signs in only with API tokens.
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

	// Prefix brands the plaintext (default TokenPrefix); it must match ^[a-z][a-z0-9]{0,14}_$.
	Prefix string
}

var tokenPrefixPattern = regexp.MustCompile(`^[a-z][a-z0-9]{0,14}_$`)

var scopePattern = regexp.MustCompile(`^[a-z0-9_*-]+:[a-z0-9_*-]+$`)

// ErrInvalidTokenSpec marks an IssueToken refusal caused by the caller's spec.
var ErrInvalidTokenSpec = errors.New("auth: invalid token spec")

const maxTokenNameBytes = 256

type tokenSpecFieldError struct {
	field  string
	reason i18n.Phrase
	params map[string]any
}

func (e *tokenSpecFieldError) Error() string                          { return e.reason.Format(e.params) }
func (e *tokenSpecFieldError) Unwrap() error                          { return ErrInvalidTokenSpec }
func (e *tokenSpecFieldError) Phrased() (i18n.Phrase, map[string]any) { return e.reason, e.params }

func invalidTokenField(field string, reason i18n.Phrase, params map[string]any) error {
	return &tokenSpecFieldError{field: field, reason: reason, params: params}
}

func validateTokenSpec(spec TokenSpec) error {
	if !textsafe.Storable(spec.Name) || len(spec.Name) > maxTokenNameBytes {
		return invalidTokenField("name", reasonInvalidTokenName, map[string]any{"max": maxTokenNameBytes})
	}
	if !textsafe.Storable(spec.OwnerID) {
		return invalidTokenField("owner_id", reasonInvalidTokenOwnerID, nil)
	}
	if strings.TrimSpace(spec.Name) == "" {
		return invalidTokenSpec(reasonTokenNameRequired, nil)
	}
	if spec.OwnerKind != OwnerKindUser && spec.OwnerKind != OwnerKindService {
		return invalidTokenSpec(reasonInvalidOwnerKind, map[string]any{"kind": strconv.Quote(spec.OwnerKind)})
	}
	if strings.TrimSpace(spec.OwnerID) == "" {
		return invalidTokenSpec(reasonOwnerIDRequired, nil)
	}
	if len(spec.Scopes) > maxTokenScopes {
		return invalidTokenSpec(reasonTooManyScopes, map[string]any{"count": len(spec.Scopes), "max": maxTokenScopes})
	}
	for _, sc := range spec.Scopes {
		if !scopePattern.MatchString(sc) {
			return invalidTokenSpec(reasonInvalidScope, map[string]any{"scope": strconv.Quote(sc)})
		}
	}
	if spec.TTL < 0 {
		return invalidTokenSpec(reasonNegativeTokenTTL, map[string]any{"ttl": spec.TTL.String()})
	}
	if spec.Prefix != "" && !tokenPrefixPattern.MatchString(spec.Prefix) {
		return invalidTokenSpec(reasonInvalidTokenPrefix, map[string]any{"prefix": strconv.Quote(spec.Prefix), "example": strconv.Quote(TokenPrefix)})
	}
	return nil
}

func invalidTokenSpec(reason i18n.Phrase, params map[string]any) error {
	return &i18n.Error{Reason: reason, Params: params, Err: ErrInvalidTokenSpec}
}

// IssueToken validates spec, stores the hashed record and returns the plaintext once.
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
	id, err := generateAPITokenID()
	if err != nil {
		return "", APIToken{}, err
	}
	rec := APIToken{
		ID:        id,
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

// NewServiceAccount returns a ServiceAccount with a fresh ID, ready for ServiceAccountStore.Create.
func NewServiceAccount(name string, roles []string) ServiceAccount {
	id, err := generateAPITokenID()
	if err != nil {
		panic(fmt.Sprintf("auth: NewServiceAccount: %v", err))
	}
	return ServiceAccount{
		ID:        id,
		Name:      name,
		Roles:     roles,
		CreatedAt: time.Now().UTC(),
	}
}

type serviceAccountUser struct {
	sa *ServiceAccount
}

func (u *serviceAccountUser) GetID() string      { return u.sa.ID }
func (u *serviceAccountUser) GetEmail() string   { return "" }
func (u *serviceAccountUser) GetRoles() []string { return u.sa.Roles }

func generateAPITokenPlaintext(prefix string) (string, error) {
	b := make([]byte, tokenRandBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: generate api token entropy: %w", err)
	}
	return prefix + hex.EncodeToString(b), nil
}

func generateAPITokenID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: generate api token id entropy: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func sha256hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func tokenDisplayPrefix(plaintext string) string {
	n := tokenPrefixLen
	if i := strings.IndexByte(plaintext, '_'); i >= 0 && i < maxTokenBrandLen {
		n = i + 1 + tokenDisplayRandLen
	}
	if len(plaintext) > n {
		return plaintext[:n]
	}
	return plaintext
}

type tokenScopesKey struct{}
type tokenIDKey struct{}

// WithTokenScopes marks ctx as scoped to scopes, for the auth gates and the framework's held-scope checks.
func WithTokenScopes(ctx context.Context, scopes []string) context.Context {
	ctx = access.WithHeldScopes(ctx, scopes)
	return context.WithValue(ctx, tokenScopesKey{}, scopes)
}

// WithTokenID records the authenticating token's ID in ctx.
func WithTokenID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, tokenIDKey{}, id)
}

// TokenID returns the authenticating token's ID, or false for a non-token request.
func TokenID(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(tokenIDKey{}).(string)
	return id, ok
}

// TokenScopes returns the scopes of an API-token or embed-grant request, or false for a session or JWT.
func TokenScopes(ctx context.Context) ([]string, bool) {
	if s, ok := ctx.Value(tokenScopesKey{}).([]string); ok {
		return s, true
	}
	if g, ok := embed.GrantFromContext(ctx); ok {
		return g.Scopes, true
	}
	return nil, false
}

// HasScope reports whether the request may use scope; an unscoped request may use any.
func HasScope(ctx context.Context, scope string) bool {
	held, ok := TokenScopes(ctx)
	if !ok {
		return true // session/JWT: unscoped by design
	}
	return scopeMatches(held, scope)
}

// ScopeMatch reports whether held grants want under the resource:verb wildcard grammar of access.ScopeMatch.
func ScopeMatch(held []string, want string) bool {
	return scopeMatches(held, want)
}

func scopeMatches(held []string, want string) bool {
	granted := make([]access.Permission, len(held))
	for i, h := range held {
		granted[i] = access.Permission(h)
	}
	return access.ScopeMatch(granted, access.Permission(want))
}
