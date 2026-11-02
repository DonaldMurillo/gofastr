package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
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
	// Expiry is when AccessToken stops being valid. Zero means the provider
	// stated no expiry (no expires_in, as with GitHub's classic OAuth-app
	// tokens, which never expire): ValidOAuthToken returns such a token as
	// it is, and a caller whose provider call is refused with it calls
	// RefreshOAuthToken (or re-authenticates when no refresh token exists).
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

// OAuthTokenLeaser is implemented by a token store shared across replicas
// that can lease the right to spend a stored refresh token. Without it,
// two replicas refreshing one grant at once both spend the same refresh
// token: a rotating provider refuses the second, and one that detects
// reuse (RFC 9700 §4.14.2; Auth0, Okta) revokes the whole grant, so the
// user has to reconnect. SQLOAuthTokenStore implements it. A store that
// does not is serialized per process only.
type OAuthTokenLeaser interface {
	// LeaseRefresh claims the right to spend the stored refresh token
	// `from` for (userID, provider) until `until`. It reports false, and
	// claims nothing, when another holder's lease is live or the row no
	// longer holds `from`.
	LeaseRefresh(ctx context.Context, userID, provider, from string, until time.Time) (bool, error)
	// ReleaseRefresh drops the lease on `from` early, after an exchange
	// that failed. SaveIfRefreshUnchanged and Save clear it on success.
	ReleaseRefresh(ctx context.Context, userID, provider, from string) error
}

// ErrOAuthRefreshInProgress reports that another replica holds the lease
// on this grant's refresh and did not finish within the caller's wait.
// The refresh token was not spent: retry shortly.
var ErrOAuthRefreshInProgress = errors.New("auth: oauth token refresh in progress on another replica")

// oauthRefreshLease bounds a refresh lease: longer than a token exchange
// (10s client timeout) so a live exchange never loses its lease, short
// enough that a replica that died mid-exchange frees the grant soon.
const oauthRefreshLease = 30 * time.Second

// oauthRefreshWait bounds how long a caller that found another replica's
// lease waits for that refresh to land before failing closed.
const oauthRefreshWait = 10 * time.Second

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
	db      *sql.DB
	table   string
	sealer  *aeadSealer
	dialect storeDialect
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
	registerOAuthTokens(s.table)
	return s, nil
}

// registerOAuthTokens declares the token table to App.ExportData and
// App.EraseUserData under its configured name. An erased user's provider
// grants are deleted: a surviving refresh token still mints access to
// the user's provider account. The export carries the tokens sealed, so
// a restore needs the same EncryptionKey.
func registerOAuthTokens(table string) {
	datexport.Register(datexport.DataExporter{
		Name: datexport.ScopedName("oauth_tokens", table), Source: "auth", Table: table, PrimaryKey: "user_id,provider",
		Columns: []string{"user_id", "provider", "access_token", "refresh_token", "refresh_sha", "expires_at"},
	})
	datexport.RegisterEraser(datexport.DataEraser{
		Name: datexport.ScopedName("oauth_tokens", table), Source: "auth", Table: table,
		Column: "user_id", Mode: datexport.EraseDelete,
	})
}

func (s *SQLOAuthTokenStore) ensureTable(ctx context.Context) error {
	if err := refuseCaseKeptTable(ctx, s.db, s.dialect.of(s.db), s.table); err != nil {
		return err
	}
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
		query.QuoteFolded(s.table),
	)
	if _, err := s.db.ExecContext(ctx, q); err != nil {
		return err
	}
	if err := s.migrateRefreshSHA(ctx); err != nil {
		return err
	}
	return s.addColumn(ctx, "refresh_lease_ms BIGINT NOT NULL DEFAULT 0")
}

// addColumn adds a column to a table created by an older build; a column
// already there is the goal, not an error.
func (s *SQLOAuthTokenStore) addColumn(ctx context.Context, def string) error {
	alter := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s`, query.QuoteFolded(s.table), def)
	if s.dialect.of(s.db) == migrate.DialectPostgres {
		alter = fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s`, query.QuoteFolded(s.table), def)
	}
	if _, err := s.db.ExecContext(ctx, alter); err != nil {
		if s.dialect.of(s.db) != migrate.DialectPostgres && strings.Contains(err.Error(), "duplicate column") {
			return nil
		}
		return err
	}
	return nil
}

// LeaseRefresh implements OAuthTokenLeaser: one conditional UPDATE, so of
// any number of replicas racing for one grant exactly one claims it.
func (s *SQLOAuthTokenStore) LeaseRefresh(ctx context.Context, userID, provider, from string, until time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, fmt.Sprintf(
		`UPDATE %s SET refresh_lease_ms = $4 WHERE user_id = $1 AND provider = $2 AND refresh_sha = $3 AND refresh_lease_ms < $5`,
		query.QuoteFolded(s.table)), userID, provider, sha256hex(from), until.UnixMilli(), time.Now().UnixMilli())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// ReleaseRefresh implements OAuthTokenLeaser.
func (s *SQLOAuthTokenStore) ReleaseRefresh(ctx context.Context, userID, provider, from string) error {
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(
		`UPDATE %s SET refresh_lease_ms = 0 WHERE user_id = $1 AND provider = $2 AND refresh_sha = $3`,
		query.QuoteFolded(s.table)), userID, provider, sha256hex(from))
	return err
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
	alter := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN refresh_sha TEXT NOT NULL DEFAULT ''`, query.QuoteFolded(s.table))
	if s.dialect.of(s.db) == migrate.DialectPostgres {
		alter = fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS refresh_sha TEXT NOT NULL DEFAULT ''`, query.QuoteFolded(s.table))
	}
	if _, err := s.db.ExecContext(ctx, alter); err != nil {
		// SQLite has no IF NOT EXISTS for columns; a duplicate-column
		// error is the column already being there, the migration's goal.
		if s.dialect.of(s.db) != migrate.DialectPostgres && strings.Contains(err.Error(), "duplicate column") {
			// fall through to the backfill
		} else {
			return err
		}
	}
	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT user_id, provider, refresh_token FROM %s WHERE refresh_sha = ''`, query.QuoteFolded(s.table)))
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
			fmt.Sprintf(`UPDATE %s SET refresh_sha = $1 WHERE user_id = $2 AND provider = $3`, query.QuoteFolded(s.table)),
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
		               expires_at = EXCLUDED.expires_at,
		               refresh_lease_ms = 0`,
		query.QuoteFolded(s.table),
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
		`UPDATE %s SET access_token = $3, refresh_token = $4, refresh_sha = $5, expires_at = $6, refresh_lease_ms = 0
		 WHERE user_id = $1 AND provider = $2 AND refresh_sha = $7`,
		query.QuoteFolded(s.table),
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
		query.QuoteFolded(s.table),
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
	q := fmt.Sprintf(`DELETE FROM %s WHERE user_id = $1 AND provider = $2`, query.QuoteFolded(s.table))
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
// The row is read and written under provider.Name(). The login callback
// stores a row under the provider's REGISTRY key, the {provider} of
// /auth/oauth/{provider}; when that differs from Name() (an OIDCProvider
// registered as "keycloak" reports Name() "oidc") this function finds no
// row. (*OAuth2Plugin).RefreshOAuthToken takes the registry key and is the
// spelling that always matches the callback.
//
// The provider must implement OAuthTokenRefresher (the built-in Google and
// GitHub providers do). If the stored token has no refresh token, refresh
// is impossible and an error is returned, the user must re-authenticate.
//
// Concurrent refreshes of one (user, provider) in this process run one at
// a time, and one that finds the row already moved on by the refresh it
// waited behind returns that record without spending the refresh token
// again: under a provider with single-use refresh tokens a second POST of
// the same token fails, or revokes the grant. When the exchange fails and
// the row has meanwhile moved on (another replica refreshed first), the
// newer record is returned instead of the error.
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
	if err := checkTokenArgs(store, provider); err != nil {
		return OAuthTokenRecord{}, err
	}
	return refreshOAuthToken(ctx, store, provider, provider.Name(), userID)
}

// ValidOAuthToken returns a currently-valid access token for the user,
// refreshing transparently when the stored token is expired or within
// refreshSkew of expiry. A token stored with a zero Expiry (the provider
// stated none, GitHub's classic tokens) is returned as it is. It is the
// recommended entry point for code making calls on the user's behalf.
//
// Like RefreshOAuthToken it reads the row under provider.Name(); see there
// for when to use (*OAuth2Plugin).ValidOAuthToken instead.
//
// SECURITY: as with RefreshOAuthToken, userID MUST be the authenticated
// principal's id, never request-supplied, or it is an IDOR.
func ValidOAuthToken(ctx context.Context, store OAuthTokenStore, provider OAuth2Provider, userID string) (string, error) {
	if err := checkTokenArgs(store, provider); err != nil {
		return "", err
	}
	return validOAuthToken(ctx, store, provider, provider.Name(), userID)
}

// ValidOAuthToken is the package ValidOAuthToken for the provider
// registered under providerKey (the {provider} of /auth/oauth/{provider}),
// on the plugin's OAuth2Config.TokenStore. It reads the row under the key
// the login callback stored it under, whatever the provider's Name().
//
// SECURITY: userID MUST be the authenticated principal's id.
func (p *OAuth2Plugin) ValidOAuthToken(ctx context.Context, providerKey, userID string) (string, error) {
	provider, err := p.tokenProvider(providerKey)
	if err != nil {
		return "", err
	}
	return validOAuthToken(ctx, p.tokenStore, provider, providerKey, userID)
}

// RefreshOAuthToken is the package RefreshOAuthToken for the provider
// registered under providerKey, on the plugin's OAuth2Config.TokenStore.
//
// SECURITY: userID MUST be the authenticated principal's id.
func (p *OAuth2Plugin) RefreshOAuthToken(ctx context.Context, providerKey, userID string) (OAuthTokenRecord, error) {
	provider, err := p.tokenProvider(providerKey)
	if err != nil {
		return OAuthTokenRecord{}, err
	}
	return refreshOAuthToken(ctx, p.tokenStore, provider, providerKey, userID)
}

// tokenProvider resolves providerKey for the plugin's token methods.
func (p *OAuth2Plugin) tokenProvider(providerKey string) (OAuth2Provider, error) {
	provider, ok := p.providers[providerKey]
	if !ok {
		return nil, fmt.Errorf("auth: no oauth provider registered under %q", providerKey)
	}
	if err := checkTokenArgs(p.tokenStore, provider); err != nil {
		return nil, err
	}
	return provider, nil
}

func checkTokenArgs(store OAuthTokenStore, provider OAuth2Provider) error {
	if store == nil {
		return errors.New("auth: oauth token store not configured")
	}
	if provider == nil {
		return errors.New("auth: nil oauth provider")
	}
	return nil
}

// tokenUsable reports whether rec's access token can be used as it is.
func tokenUsable(rec OAuthTokenRecord) bool {
	return rec.AccessToken != "" && (rec.Expiry.IsZero() || time.Until(rec.Expiry) > refreshSkew)
}

func validOAuthToken(ctx context.Context, store OAuthTokenStore, provider OAuth2Provider, key, userID string) (string, error) {
	rec, err := store.Get(ctx, userID, key)
	if err != nil {
		return "", err
	}
	if tokenUsable(rec) {
		return rec.AccessToken, nil
	}
	refreshed, err := refreshFrom(ctx, store, provider, key, userID, rec)
	if err != nil {
		return "", err
	}
	return refreshed.AccessToken, nil
}

func refreshOAuthToken(ctx context.Context, store OAuthTokenStore, provider OAuth2Provider, key, userID string) (OAuthTokenRecord, error) {
	if _, ok := provider.(OAuthTokenRefresher); !ok {
		return OAuthTokenRecord{}, fmt.Errorf("auth: provider %q does not support token refresh", key)
	}
	seen, err := store.Get(ctx, userID, key)
	if err != nil {
		return OAuthTokenRecord{}, err
	}
	return refreshFrom(ctx, store, provider, key, userID, seen)
}

// tokenMovedOn reports whether cur is a different grant than seen: another
// refresh or a login stored it after seen was read.
func tokenMovedOn(seen, cur OAuthTokenRecord) bool {
	return !sameSecret(cur.RefreshToken, seen.RefreshToken) || !sameSecret(cur.AccessToken, seen.AccessToken) ||
		cur.Expiry.After(seen.Expiry)
}

// sameSecret compares two stored credentials in constant time.
func sameSecret(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// refreshFrom refreshes the row the caller read as seen, one refresh per
// (user, provider) at a time in this process.
func refreshFrom(ctx context.Context, store OAuthTokenStore, provider OAuth2Provider, key, userID string, seen OAuthTokenRecord) (OAuthTokenRecord, error) {
	refresher, ok := provider.(OAuthTokenRefresher)
	if !ok {
		return OAuthTokenRecord{}, fmt.Errorf("auth: provider %q does not support token refresh", key)
	}
	release, err := oauthRefreshGates.acquire(ctx, oauthRefreshKey{userID: userID, provider: key})
	if err != nil {
		return OAuthTokenRecord{}, err
	}
	defer release()

	rec, err := store.Get(ctx, userID, key)
	if err != nil {
		return OAuthTokenRecord{}, err
	}
	if tokenMovedOn(seen, rec) {
		// The refresh this call waited behind (or a login) already
		// replaced the grant: spending the refresh token again would
		// fail under rotation, or revoke the grant.
		return rec, nil
	}
	if rec.RefreshToken == "" {
		return OAuthTokenRecord{}, fmt.Errorf("auth: no refresh token stored for user %q provider %q", userID, key)
	}

	refreshedFrom := rec.RefreshToken
	// Across replicas the in-process gate above holds nothing: lease the
	// grant before spending its refresh token. Fail closed throughout: a
	// lease that cannot be taken or read is no licence to spend.
	leaser, leased := store.(OAuthTokenLeaser)
	if leased {
		got, err := leaser.LeaseRefresh(ctx, userID, key, refreshedFrom, time.Now().Add(oauthRefreshLease))
		if err != nil {
			return OAuthTokenRecord{}, fmt.Errorf("auth: lease oauth token refresh: %w", err)
		}
		if !got {
			return awaitOtherRefresh(ctx, store, userID, key, rec)
		}
	}
	tok, err := refresher.RefreshToken(ctx, rec.RefreshToken)
	if err != nil {
		if leased {
			undo, cancel := undoContext(ctx)
			if rerr := leaser.ReleaseRefresh(undo, userID, key, refreshedFrom); rerr != nil {
				slog.Warn("auth: releasing an oauth refresh lease failed; it lapses on its own",
					"user_hash", hashedIdentifier(userID), "err", rerr)
			}
			cancel()
		}
		// Another replica may have spent this refresh token first; its
		// newer record is then the answer, not the failure.
		if cur, gerr := store.Get(ctx, userID, key); gerr == nil && tokenMovedOn(rec, cur) {
			return cur, nil
		}
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
		current, err := store.Get(ctx, userID, key)
		if err != nil {
			// The row vanished (unlink) rather than moved on; hand back
			// our refreshed record rather than failing the caller.
			return rec, nil
		}
		return current, nil
	}
	return rec, nil
}

// awaitOtherRefresh waits, bounded by oauthRefreshWait and ctx, for the
// replica holding the lease to store its refreshed grant, and returns it.
// It never spends the refresh token itself: when the wait ends with the
// grant unchanged it fails with ErrOAuthRefreshInProgress.
func awaitOtherRefresh(ctx context.Context, store OAuthTokenStore, userID, key string, seen OAuthTokenRecord) (OAuthTokenRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, oauthRefreshWait)
	defer cancel()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		//gofastr:allow(recovercallback) a bounded poll on the caller's own request goroutine, the same call refreshFrom makes outside the loop; no goroutine or read loop is spawned
		cur, err := store.Get(ctx, userID, key)
		if err == nil && tokenMovedOn(seen, cur) {
			return cur, nil
		}
		if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			return OAuthTokenRecord{}, err
		}
		select {
		case <-ctx.Done():
			return OAuthTokenRecord{}, ErrOAuthRefreshInProgress
		case <-tick.C:
		}
	}
}

// oauthRefreshKey names one stored grant for the refresh gate.
type oauthRefreshKey struct {
	userID   string
	provider string
}

// oauthRefreshGate admits one refresh per (user, provider) at a time in
// this process. Each key's slot lives only while someone holds or waits
// for it, so the map stays as small as the refreshes in flight, and a
// refresh for one user never waits on another's.
type oauthRefreshGate struct {
	mu    sync.Mutex
	slots map[oauthRefreshKey]*oauthRefreshSlot
}

type oauthRefreshSlot struct {
	ch   chan struct{}
	refs int
}

var oauthRefreshGates = &oauthRefreshGate{slots: map[oauthRefreshKey]*oauthRefreshSlot{}}

// acquire waits, bounded by ctx, for k's slot and returns its release.
func (g *oauthRefreshGate) acquire(ctx context.Context, k oauthRefreshKey) (func(), error) {
	g.mu.Lock()
	s := g.slots[k]
	if s == nil {
		s = &oauthRefreshSlot{ch: make(chan struct{}, 1)}
		g.slots[k] = s
	}
	s.refs++
	g.mu.Unlock()
	select {
	case s.ch <- struct{}{}:
		return func() {
			<-s.ch
			g.drop(k, s)
		}, nil
	case <-ctx.Done():
		g.drop(k, s)
		return nil, ctx.Err()
	}
}

func (g *oauthRefreshGate) drop(k oauthRefreshKey, s *oauthRefreshSlot) {
	g.mu.Lock()
	defer g.mu.Unlock()
	s.refs--
	if s.refs == 0 {
		delete(g.slots, k)
	}
}
