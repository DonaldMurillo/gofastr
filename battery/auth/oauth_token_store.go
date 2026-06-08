package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
)

// ─── Token store ────────────────────────────────────────────────────────────

// OAuthTokenRecord is one provider token tied to a local user. The store
// persists it so that calls made on the user's behalf (e.g. reading a
// Google calendar) can recover after the short-lived access token expires
// by exchanging the refresh token for a fresh one.
type OAuthTokenRecord struct {
	// UserID is the local user/account id the token belongs to.
	UserID string
	// Provider is the OAuth2 provider name ("google", "github", …).
	Provider string
	// AccessToken is the current provider access token.
	AccessToken string
	// RefreshToken is the long-lived token used to mint new access tokens.
	// May be empty when the provider did not issue one.
	RefreshToken string
	// Expiry is when AccessToken stops being valid. Zero means "unknown";
	// callers treat a zero expiry as already-expired so a refresh is forced.
	Expiry time.Time
}

// ErrOAuthTokenNotFound is returned by OAuthTokenStore.Get when no token
// is stored for the (userID, provider) pair.
var ErrOAuthTokenNotFound = errors.New("auth: oauth token not found")

// OAuthTokenStore persists provider tokens per (user, provider). It mirrors
// the durable-store shape auth already uses for magic-link/reset/verify
// tokens. The store is opt-in: OAuth login works without one, but no
// refresh is possible until a store is configured.
type OAuthTokenStore interface {
	// Save inserts or replaces the token row for (UserID, Provider).
	Save(ctx context.Context, rec OAuthTokenRecord) error
	// Get returns the stored token for the pair, or ErrOAuthTokenNotFound.
	Get(ctx context.Context, userID, provider string) (OAuthTokenRecord, error)
	// Delete removes the stored token for the pair. Deleting a missing row
	// is not an error.
	Delete(ctx context.Context, userID, provider string) error
}

// ─── SQL implementation ─────────────────────────────────────────────────────

// SQLOAuthTokenStoreConfig tunes the SQL-backed store. EncryptionKey is
// required (NewSQLOAuthTokenStore fails closed without it).
type SQLOAuthTokenStoreConfig struct {
	// Table is the table name; defaults to "oauth_tokens".
	Table string
	// EncryptionKey seals the access/refresh tokens at rest with AES-GCM.
	// REQUIRED and non-empty — stored refresh tokens are password-equivalent,
	// so there is no default key. Any length is accepted (it is SHA-256-folded
	// to a 32-byte key). Source it from a secret manager, not source code.
	EncryptionKey []byte
}

// SQLOAuthTokenStore is a database-backed OAuthTokenStore. Tokens survive
// process restarts and are shared across replicas. Access and refresh
// tokens are sealed with AES-GCM before they touch the database so a raw
// table dump does not leak the live provider secrets.
//
// Schema (created on construction): (user_id, provider) composite primary
// key, sealed access/refresh token columns, and an expiry stored as a unix
// timestamp (portable across SQLite and Postgres).
type SQLOAuthTokenStore struct {
	db    *sql.DB
	table string
	gcm   cipher.AEAD
}

// NewSQLOAuthTokenStore creates the token table (IF NOT EXISTS) and returns
// the store. A non-empty EncryptionKey is REQUIRED — stored refresh tokens are
// password-equivalent, so the store fails closed rather than sealing them with
// a default key (which would be reversible obfuscation, not encryption).
func NewSQLOAuthTokenStore(db *sql.DB, cfg ...SQLOAuthTokenStoreConfig) (*SQLOAuthTokenStore, error) {
	var c SQLOAuthTokenStoreConfig
	if len(cfg) > 0 {
		c = cfg[0]
	}
	t := c.Table
	if t == "" {
		t = "oauth_tokens"
	}
	if _, err := query.SafeIdent(t); err != nil {
		return nil, fmt.Errorf("auth: oauth token table %q: %w", t, err)
	}

	// Fail closed: never seal password-equivalent refresh tokens with a key
	// the deployer didn't choose. An empty EncryptionKey is a config error.
	if len(c.EncryptionKey) == 0 {
		return nil, fmt.Errorf("auth: oauth token store requires a non-empty EncryptionKey")
	}
	sum := sha256.Sum256(c.EncryptionKey)
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, fmt.Errorf("auth: oauth token cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("auth: oauth token gcm: %w", err)
	}

	s := &SQLOAuthTokenStore{db: db, table: t, gcm: gcm}
	if err := s.ensureTable(context.Background()); err != nil {
		return nil, fmt.Errorf("auth: create oauth token table: %w", err)
	}
	return s, nil
}

func (s *SQLOAuthTokenStore) ensureTable(ctx context.Context) error {
	q := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s (
			user_id TEXT NOT NULL,
			provider TEXT NOT NULL,
			access_token TEXT NOT NULL,
			refresh_token TEXT NOT NULL,
			expires_at BIGINT NOT NULL,
			PRIMARY KEY (user_id, provider)
		)`,
		query.QuoteIdent(s.table),
	)
	_, err := s.db.ExecContext(ctx, q)
	return err
}

// seal encrypts a plaintext token with AES-GCM and base64-encodes it. The
// nonce is prepended to the ciphertext. An empty plaintext seals to the
// empty string so callers can distinguish "no refresh token" cheaply.
func (s *SQLOAuthTokenStore) seal(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, s.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := s.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.RawStdEncoding.EncodeToString(ct), nil
}

// open reverses seal.
func (s *SQLOAuthTokenStore) open(sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	raw, err := base64.RawStdEncoding.DecodeString(sealed)
	if err != nil {
		return "", err
	}
	ns := s.gcm.NonceSize()
	if len(raw) < ns {
		return "", errors.New("auth: oauth token ciphertext too short")
	}
	pt, err := s.gcm.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// Save upserts the token row for (UserID, Provider).
func (s *SQLOAuthTokenStore) Save(ctx context.Context, rec OAuthTokenRecord) error {
	access, err := s.seal(rec.AccessToken)
	if err != nil {
		return err
	}
	refresh, err := s.seal(rec.RefreshToken)
	if err != nil {
		return err
	}
	q := fmt.Sprintf(
		`INSERT INTO %s (user_id, provider, access_token, refresh_token, expires_at)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (user_id, provider)
		 DO UPDATE SET access_token = EXCLUDED.access_token,
		               refresh_token = EXCLUDED.refresh_token,
		               expires_at = EXCLUDED.expires_at`,
		query.QuoteIdent(s.table),
	)
	_, err = s.db.ExecContext(ctx, q, rec.UserID, rec.Provider, access, refresh, rec.Expiry.Unix())
	return err
}

// Get returns the stored token, or ErrOAuthTokenNotFound.
func (s *SQLOAuthTokenStore) Get(ctx context.Context, userID, provider string) (OAuthTokenRecord, error) {
	q := fmt.Sprintf(
		`SELECT access_token, refresh_token, expires_at FROM %s WHERE user_id = $1 AND provider = $2`,
		query.QuoteIdent(s.table),
	)
	var access, refresh string
	var exp int64
	err := s.db.QueryRowContext(ctx, q, userID, provider).Scan(&access, &refresh, &exp)
	if err == sql.ErrNoRows {
		return OAuthTokenRecord{}, ErrOAuthTokenNotFound
	}
	if err != nil {
		return OAuthTokenRecord{}, err
	}
	at, err := s.open(access)
	if err != nil {
		return OAuthTokenRecord{}, err
	}
	rt, err := s.open(refresh)
	if err != nil {
		return OAuthTokenRecord{}, err
	}
	return OAuthTokenRecord{
		UserID:       userID,
		Provider:     provider,
		AccessToken:  at,
		RefreshToken: rt,
		Expiry:       time.Unix(exp, 0),
	}, nil
}

// Delete removes the stored token; deleting a missing row is not an error.
func (s *SQLOAuthTokenStore) Delete(ctx context.Context, userID, provider string) error {
	q := fmt.Sprintf(`DELETE FROM %s WHERE user_id = $1 AND provider = $2`, query.QuoteIdent(s.table))
	_, err := s.db.ExecContext(ctx, q, userID, provider)
	return err
}

// ─── Refresh path ───────────────────────────────────────────────────────────

// OAuthTokenRefresher is implemented by providers that can exchange a
// refresh token for a fresh access token at the provider's token endpoint.
// The built-in GoogleProvider and GitHubProvider implement it. The refresh
// path is concrete per provider — there is deliberately no generic provider
// registry here.
type OAuthTokenRefresher interface {
	// RefreshToken exchanges refreshToken for a fresh access token. The
	// returned token's RefreshToken may be empty when the provider does
	// not re-issue one (Google's typical behavior); callers retain the
	// previously stored refresh token in that case.
	RefreshToken(ctx context.Context, refreshToken string) (*OAuth2Token, error)
}

// refreshSkew is how close to expiry ValidOAuthToken proactively refreshes.
const refreshSkew = 60 * time.Second

// RefreshOAuthToken loads the stored token for userID, exchanges its refresh
// token for a fresh access token via the provider, and writes the result
// back to the store. It returns the updated record.
//
// The provider must implement OAuthTokenRefresher (the built-in Google and
// GitHub providers do). If the stored token has no refresh token, refresh
// is impossible and an error is returned — the user must re-authenticate.
//
// SECURITY: userID MUST be the authenticated principal's id (e.g. from the
// resolved session), never a value taken from request input. Passing a
// client-supplied id is an IDOR — it reads/refreshes another user's tokens.
func RefreshOAuthToken(ctx context.Context, store OAuthTokenStore, provider OAuth2Provider, userID string) (OAuthTokenRecord, error) {
	if store == nil {
		return OAuthTokenRecord{}, errors.New("auth: oauth token store not configured")
	}
	if provider == nil {
		return OAuthTokenRecord{}, errors.New("auth: nil oauth provider")
	}
	refresher, ok := provider.(OAuthTokenRefresher)
	if !ok {
		return OAuthTokenRecord{}, fmt.Errorf("auth: provider %q does not support token refresh", provider.Name())
	}

	rec, err := store.Get(ctx, userID, provider.Name())
	if err != nil {
		return OAuthTokenRecord{}, err
	}
	if rec.RefreshToken == "" {
		return OAuthTokenRecord{}, fmt.Errorf("auth: no refresh token stored for user %q provider %q", userID, provider.Name())
	}

	tok, err := refresher.RefreshToken(ctx, rec.RefreshToken)
	if err != nil {
		return OAuthTokenRecord{}, fmt.Errorf("auth: refresh failed: %w", err)
	}

	rec.AccessToken = tok.AccessToken
	rec.Expiry = tok.Expiry
	// Providers commonly omit the refresh token on a refresh grant; keep
	// the stored one so the next refresh still works.
	if tok.RefreshToken != "" {
		rec.RefreshToken = tok.RefreshToken
	}

	if err := store.Save(ctx, rec); err != nil {
		return OAuthTokenRecord{}, err
	}
	return rec, nil
}

// ValidOAuthToken returns a currently-valid access token for the user,
// refreshing transparently when the stored token is expired or within
// refreshSkew of expiry. It is the recommended entry point for code making
// calls on the user's behalf.
//
// SECURITY: as with RefreshOAuthToken, userID MUST be the authenticated
// principal's id — never request-supplied — or it is an IDOR.
func ValidOAuthToken(ctx context.Context, store OAuthTokenStore, provider OAuth2Provider, userID string) (string, error) {
	if store == nil {
		return "", errors.New("auth: oauth token store not configured")
	}
	if provider == nil {
		return "", errors.New("auth: nil oauth provider")
	}
	rec, err := store.Get(ctx, userID, provider.Name())
	if err != nil {
		return "", err
	}
	if rec.AccessToken != "" && time.Until(rec.Expiry) > refreshSkew {
		return rec.AccessToken, nil
	}
	refreshed, err := RefreshOAuthToken(ctx, store, provider, userID)
	if err != nil {
		return "", err
	}
	return refreshed.AccessToken, nil
}
