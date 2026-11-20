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

type aeadSealer struct {
	gcm cipher.AEAD
}

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


// OAuthTokenRecord is one provider grant held for a local user.
type OAuthTokenRecord struct {
	// UserID is the local user/account id the token belongs to.
	UserID string
	// Provider is the OAuth2 provider name ("google", "github", …).
	Provider string
	// AccessToken is the current provider access token.
	AccessToken string
	// RefreshToken is the long-lived token used to mint new access tokens.
	RefreshToken string
	// Expiry is when AccessToken lapses; zero means the provider stated none.
	Expiry time.Time
}

// ErrOAuthTokenNotFound means no token is stored for the user and provider.
var ErrOAuthTokenNotFound = errors.New("auth: oauth token not found")

// OAuthTokenStore keeps provider tokens per user and provider.
type OAuthTokenStore interface {
	// Save inserts or replaces the token row for (UserID, Provider).
	Save(ctx context.Context, rec OAuthTokenRecord) error
	// SaveIfRefreshUnchanged writes rec only while the stored row still holds refresh token from, and reports whether it wrote.
	SaveIfRefreshUnchanged(ctx context.Context, rec OAuthTokenRecord, from string) (bool, error)
	// Get returns the stored token for the pair, or ErrOAuthTokenNotFound.
	Get(ctx context.Context, userID, provider string) (OAuthTokenRecord, error)
	// Delete removes the pair's token; a missing row is not an error.
	Delete(ctx context.Context, userID, provider string) error
}

// OAuthTokenLeaser is a store shared by replicas that leases the right to spend a refresh token.
type OAuthTokenLeaser interface {
	// LeaseRefresh claims refresh token from until until, reporting false when another lease is live or the row moved on.
	LeaseRefresh(ctx context.Context, userID, provider, from string, until time.Time) (bool, error)
	// ReleaseRefresh drops the lease on from after a failed exchange.
	ReleaseRefresh(ctx context.Context, userID, provider, from string) error
}

// ErrOAuthRefreshInProgress means another replica's refresh did not land within the wait; nothing was spent.
var ErrOAuthRefreshInProgress = errors.New("auth: oauth token refresh in progress on another replica")

const oauthRefreshLease = 30 * time.Second

const oauthRefreshWait = 10 * time.Second


// SQLOAuthTokenStoreConfig tunes SQLOAuthTokenStore; EncryptionKey is required.
type SQLOAuthTokenStoreConfig struct {
	// Table is the table name; defaults to "oauth_tokens".
	Table string
	// EncryptionKey seals the access/refresh tokens at rest with AES-GCM.
	EncryptionKey []byte
}

// SQLOAuthTokenStore is an OAuthTokenStore in a SQLite or PostgreSQL table, tokens sealed with AES-GCM.
type SQLOAuthTokenStore struct {
	db      *sql.DB
	table   string
	sealer  *aeadSealer
	dialect storeDialect
}

// NewSQLOAuthTokenStore creates the table and returns the store, or an error without an EncryptionKey.
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

// LeaseRefresh implements OAuthTokenLeaser with one conditional UPDATE.
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

func (s *SQLOAuthTokenStore) migrateRefreshSHA(ctx context.Context) error {
	alter := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN refresh_sha TEXT NOT NULL DEFAULT ''`, query.QuoteFolded(s.table))
	if s.dialect.of(s.db) == migrate.DialectPostgres {
		alter = fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS refresh_sha TEXT NOT NULL DEFAULT ''`, query.QuoteFolded(s.table))
	}
	if _, err := s.db.ExecContext(ctx, alter); err != nil {
		if s.dialect.of(s.db) != migrate.DialectPostgres && strings.Contains(err.Error(), "duplicate column") {
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

// SaveIfRefreshUnchanged implements the compare-and-swap on refresh_sha.
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


// OAuthTokenRefresher is a provider that exchanges a refresh token for a new access token.
type OAuthTokenRefresher interface {
	// RefreshToken exchanges refreshToken; an empty RefreshToken in the result means keep the stored one.
	RefreshToken(ctx context.Context, refreshToken string) (*OAuth2Token, error)
}

const refreshSkew = 60 * time.Second

// RefreshOAuthToken refreshes the stored grant for userID under provider.Name() and returns the stored record.
func RefreshOAuthToken(ctx context.Context, store OAuthTokenStore, provider OAuth2Provider, userID string) (OAuthTokenRecord, error) {
	if err := checkTokenArgs(store, provider); err != nil {
		return OAuthTokenRecord{}, err
	}
	return refreshOAuthToken(ctx, store, provider, provider.Name(), userID)
}

// ValidOAuthToken returns a usable access token for userID, refreshing one within a minute of expiry.
func ValidOAuthToken(ctx context.Context, store OAuthTokenStore, provider OAuth2Provider, userID string) (string, error) {
	if err := checkTokenArgs(store, provider); err != nil {
		return "", err
	}
	return validOAuthToken(ctx, store, provider, provider.Name(), userID)
}

// ValidOAuthToken is ValidOAuthToken for the provider registered under providerKey; userID must be the authenticated caller.
func (p *OAuth2Plugin) ValidOAuthToken(ctx context.Context, providerKey, userID string) (string, error) {
	provider, err := p.tokenProvider(providerKey)
	if err != nil {
		return "", err
	}
	return validOAuthToken(ctx, p.tokenStore, provider, providerKey, userID)
}

// RefreshOAuthToken is RefreshOAuthToken for the provider registered under providerKey; userID must be the authenticated caller.
func (p *OAuth2Plugin) RefreshOAuthToken(ctx context.Context, providerKey, userID string) (OAuthTokenRecord, error) {
	provider, err := p.tokenProvider(providerKey)
	if err != nil {
		return OAuthTokenRecord{}, err
	}
	return refreshOAuthToken(ctx, p.tokenStore, provider, providerKey, userID)
}

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

var errOAuthRefreshNoToken = errors.New("provider returned no access token")

func tokenMovedOn(seen, cur OAuthTokenRecord) bool {
	return !sameSecret(cur.RefreshToken, seen.RefreshToken) || !sameSecret(cur.AccessToken, seen.AccessToken) ||
		cur.Expiry.After(seen.Expiry)
}

func sameSecret(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

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
		return rec, nil
	}
	if rec.RefreshToken == "" {
		return OAuthTokenRecord{}, fmt.Errorf("auth: no refresh token stored for user %q provider %q", userID, key)
	}

	refreshedFrom := rec.RefreshToken
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
	if err == nil && (tok == nil || tok.AccessToken == "") {
		err = errOAuthRefreshNoToken
	}
	if err != nil {
		if leased {
			undo, cancel := undoContext(ctx)
			if rerr := leaser.ReleaseRefresh(undo, userID, key, refreshedFrom); rerr != nil {
				slog.Warn("auth: releasing an oauth refresh lease failed; it lapses on its own",
					"user_hash", hashedIdentifier(userID), "err", rerr)
			}
			cancel()
		}
		if cur, gerr := store.Get(ctx, userID, key); gerr == nil && tokenMovedOn(rec, cur) {
			return cur, nil
		}
		return OAuthTokenRecord{}, fmt.Errorf("auth: refresh failed: %w", err)
	}

	rec.AccessToken = tok.AccessToken
	rec.Expiry = tok.Expiry
	if tok.RefreshToken != "" {
		rec.RefreshToken = tok.RefreshToken
	}

	settle, cancel := undoContext(ctx)
	defer cancel()
	saved, err := store.SaveIfRefreshUnchanged(settle, rec, refreshedFrom)
	if err != nil {
		if leased {
			if rerr := leaser.ReleaseRefresh(settle, userID, key, refreshedFrom); rerr != nil {
				slog.Warn("auth: releasing an oauth refresh lease failed; it lapses on its own",
					"user_hash", hashedIdentifier(userID), "err", rerr)
			}
		}
		return OAuthTokenRecord{}, err
	}
	if !saved {
		current, err := store.Get(settle, userID, key)
		if err != nil {
			return rec, nil
		}
		return current, nil
	}
	return rec, nil
}

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

type oauthRefreshKey struct {
	userID   string
	provider string
}

type oauthRefreshGate struct {
	mu    sync.Mutex
	slots map[oauthRefreshKey]*oauthRefreshSlot
}

type oauthRefreshSlot struct {
	ch   chan struct{}
	refs int
}

var oauthRefreshGates = &oauthRefreshGate{slots: map[oauthRefreshKey]*oauthRefreshSlot{}}

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
