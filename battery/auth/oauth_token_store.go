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
	"log/slog"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// aeadSealer is the auth battery's shared at-rest sealing: AES-GCM under a
// SHA-256-folded key, base64(nonce || ciphertext) on the wire. It exists so
// every password-equivalent secret a SQL column carries (OAuth refresh
// tokens, TOTP seeds) is sealed by ONE implementation rather than a family
// of copy-pasted ciphers drifting apart.
type aeadSealer struct {
	gcm cipher.AEAD
}

// newAEADSealer folds key material to 32 bytes and builds the AEAD. A key
// is chosen by the operator (secret manager), never defaulted: sealing
// with a built-in key is reversible obfuscation, not encryption.
func newAEADSealer(key []byte) (*aeadSealer, error) {
	if len(key) == 0 {
		return nil, fmt.Errorf("auth: at-rest sealing requires a non-empty encryption key")
	}
	sum := sha256.Sum256(key)
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, fmt.Errorf("auth: at-rest cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("auth: at-rest gcm: %w", err)
	}
	return &aeadSealer{gcm: gcm}, nil
}

// seal encrypts plaintext with AES-GCM and base64-encodes it. The nonce is
// prepended to the ciphertext. An empty plaintext seals to the empty string
// so callers can distinguish "no value" cheaply.
func (s *aeadSealer) seal(plaintext string) (string, error) {
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

// open reverses seal. The second return is false when the value is not a
// value this sealer produced (not base64, too short, failed authentication),
// which is the legacy-plaintext signal for read-both migrations.
func (s *aeadSealer) open(sealed string) (string, bool) {
	if sealed == "" {
		return "", true
	}
	raw, err := base64.RawStdEncoding.DecodeString(sealed)
	if err != nil {
		return "", false
	}
	ns := s.gcm.NonceSize()
	if len(raw) < ns {
		return "", false
	}
	pt, err := s.gcm.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", false
	}
	return string(pt), true
}

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
	// It is the login-callback's unconditional upsert; the refresh
	// path must use SaveIfRefreshUnchanged instead.
	Save(ctx context.Context, rec OAuthTokenRecord) error
	// SaveIfRefreshUnchanged persists rec only while the stored row for
	// (rec.UserID, rec.Provider) still holds `from`, the refresh token
	// the caller refreshed FROM. It reports saved=false when the row is
	// missing or has moved on — a concurrent login callback persisted a
	// newer grant — and writes nothing in that case: the stored row is
	// newer by construction, and overwriting it with the pre-exchange
	// token bricks the linkage under provider rotation. This is the
	// compare-and-swap RefreshOAuthToken's final write goes through;
	// a plain Save there is check-then-act on the re-read.
	SaveIfRefreshUnchanged(ctx context.Context, rec OAuthTokenRecord, from string) (bool, error)
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
	// REQUIRED and non-empty, stored refresh tokens are password-equivalent,
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
	db     *sql.DB
	table  string
	sealer *aeadSealer
}

// NewSQLOAuthTokenStore creates the token table (IF NOT EXISTS) and returns
// the store. A non-empty EncryptionKey is REQUIRED, stored refresh tokens are
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
	sealer, err := newAEADSealer(c.EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("auth: oauth token store: %w", err)
	}

	s := &SQLOAuthTokenStore{db: db, table: t, sealer: sealer}

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
			refresh_sha TEXT NOT NULL DEFAULT '',
			expires_at BIGINT NOT NULL,
			PRIMARY KEY (user_id, provider)
		)`,
		query.QuoteIdent(s.table),
	)
	if _, err := s.db.ExecContext(ctx, q); err != nil {
		return err
	}
	return s.migrateRefreshSHA(ctx)
}

// migrateRefreshSHA upgrades tables created before the CAS column
// existed: ALTER in refresh_sha, then backfill it from each row's sealed
// refresh token. The sealed column itself cannot be compared in SQL
// (AES-GCM uses a fresh nonce per seal), so refresh_sha — a plain
// sha256 of the refresh token — is the deterministic handle
// SaveIfRefreshUnchanged's WHERE clause keys on. A row whose digest
// cannot be backfilled (sealed value no longer opens) keeps ” and
// fails CAS closed: its refresh writes are refused until a login
// callback re-persists the row through Save.
func (s *SQLOAuthTokenStore) migrateRefreshSHA(ctx context.Context) error {
	alter := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN refresh_sha TEXT NOT NULL DEFAULT ''`, query.QuoteIdent(s.table))
	if migrate.DetectDialect(s.db) == migrate.DialectPostgres {
		alter = fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS refresh_sha TEXT NOT NULL DEFAULT ''`, query.QuoteIdent(s.table))
	}
	if _, err := s.db.ExecContext(ctx, alter); err != nil {
		// SQLite has no IF NOT EXISTS for columns; a duplicate-column
		// error is the column already being there, the migration's goal.
		if migrate.DetectDialect(s.db) != migrate.DialectPostgres && strings.Contains(err.Error(), "duplicate column") {
			// fall through to the backfill
		} else {
			return err
		}
	}
	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT user_id, provider, refresh_token FROM %s WHERE refresh_sha = ''`, query.QuoteIdent(s.table)))
	if err != nil {
		return err
	}
	defer rows.Close()
	type key struct{ user, provider string }
	var stale []key
	for rows.Next() {
		var k key
		var sealed string
		if err := rows.Scan(&k.user, &k.provider, &sealed); err != nil {
			return err
		}
		refresh, ok := s.sealer.open(sealed)
		if !ok {
			stale = append(stale, k)
			continue
		}
		if _, err := s.db.ExecContext(ctx,
			fmt.Sprintf(`UPDATE %s SET refresh_sha = $1 WHERE user_id = $2 AND provider = $3`, query.QuoteIdent(s.table)),
			sha256hex(refresh), k.user, k.provider); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, k := range stale {
		slog.Warn("auth: oauth token row could not be backfilled for CAS (sealed refresh no longer opens); refresh writes fail closed until the next login-callback Save",
			"user_id", k.user, "provider", k.provider)
	}
	return nil
}

// Save upserts the token row for (UserID, Provider).
func (s *SQLOAuthTokenStore) Save(ctx context.Context, rec OAuthTokenRecord) error {
	access, err := s.sealer.seal(rec.AccessToken)
	if err != nil {
		return err
	}
	refresh, err := s.sealer.seal(rec.RefreshToken)
	if err != nil {
		return err
	}
	q := fmt.Sprintf(
		`INSERT INTO %s (user_id, provider, access_token, refresh_token, refresh_sha, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (user_id, provider)
		 DO UPDATE SET access_token = EXCLUDED.access_token,
		               refresh_token = EXCLUDED.refresh_token,
		               refresh_sha = EXCLUDED.refresh_sha,
		               expires_at = EXCLUDED.expires_at`,
		query.QuoteIdent(s.table),
	)
	_, err = s.db.ExecContext(ctx, q, rec.UserID, rec.Provider, access, refresh, sha256hex(rec.RefreshToken), rec.Expiry.Unix())
	return err
}

// SaveIfRefreshUnchanged is the refresh path's compare-and-swap: the
// row is rewritten only while its refresh_sha still matches sha256(from).
// The predicate lives in the UPDATE's WHERE clause, so a grant persisted
// by a concurrent login callback between the caller's read and this
// write changes refresh_sha and the write declines — unlike a
// read-compare-write sequence, which is check-then-act across two
// statements and loses the same race one level down.
func (s *SQLOAuthTokenStore) SaveIfRefreshUnchanged(ctx context.Context, rec OAuthTokenRecord, from string) (bool, error) {
	access, err := s.sealer.seal(rec.AccessToken)
	if err != nil {
		return false, err
	}
	refresh, err := s.sealer.seal(rec.RefreshToken)
	if err != nil {
		return false, err
	}
	q := fmt.Sprintf(
		`UPDATE %s SET access_token = $3, refresh_token = $4, refresh_sha = $5, expires_at = $6
		 WHERE user_id = $1 AND provider = $2 AND refresh_sha = $7`,
		query.QuoteIdent(s.table),
	)
	res, err := s.db.ExecContext(ctx, q,
		rec.UserID, rec.Provider, access, refresh, sha256hex(rec.RefreshToken), rec.Expiry.Unix(), sha256hex(from))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
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
	at, ok := s.sealer.open(access)
	if !ok {
		return OAuthTokenRecord{}, errors.New("auth: oauth token ciphertext failed to open (wrong EncryptionKey?)")
	}
	rt, ok := s.sealer.open(refresh)
	if !ok {
		return OAuthTokenRecord{}, errors.New("auth: oauth token ciphertext failed to open (wrong EncryptionKey?)")
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
// path is concrete per provider, there is deliberately no generic provider
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
// is impossible and an error is returned, the user must re-authenticate.
//
// The final write is a compare-and-swap on the refresh token refreshed
// FROM (SaveIfRefreshUnchanged): when a concurrent login callback
// persists a newer grant during the exchange, this call declines to
// write and returns that newer record instead.
//
// SECURITY: userID MUST be the authenticated principal's id (e.g. from the
// resolved session), never a value taken from request input. Passing a
// client-supplied id is an IDOR, it reads/refreshes another user's tokens.
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

	refreshedFrom := rec.RefreshToken
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

	// Compare-and-swap the write: it lands only while the row still
	// holds `refreshedFrom`. A login callback running concurrently — the
	// user reconnecting the provider in another tab — persists a freshly
	// rotated grant during the exchange, and an unconditional write puts
	// the token we started from back on top of it; under a provider that
	// rotates refresh tokens the overwritten one is already dead, so the
	// linkage bricks until the user re-authorizes, and nothing reports
	// it. CAS declines, and the caller is handed the newer grant that
	// won. The comparison is on the refresh token we refreshed FROM: if
	// the stored one is no longer that, someone else has moved the
	// record on and their version is newer than ours by construction.
	saved, err := store.SaveIfRefreshUnchanged(ctx, rec, refreshedFrom)
	if err != nil {
		return OAuthTokenRecord{}, err
	}
	if !saved {
		current, err := store.Get(ctx, userID, provider.Name())
		if err != nil {
			// The row vanished (unlink) rather than moved on; hand back
			// our refreshed record rather than failing the caller.
			return rec, nil
		}
		return current, nil
	}
	return rec, nil
}

// ValidOAuthToken returns a currently-valid access token for the user,
// refreshing transparently when the stored token is expired or within
// refreshSkew of expiry. It is the recommended entry point for code making
// calls on the user's behalf.
//
// SECURITY: as with RefreshOAuthToken, userID MUST be the authenticated
// principal's id, never request-supplied, or it is an IDOR.
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
