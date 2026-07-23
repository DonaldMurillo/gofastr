package middleware

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// SQLIdempotencyStore persists idempotency claims to a SQL database.
// It works with sqlite and postgres; dialect is pinned via
// WithSQLIdempotencyDialect or auto-detected via SELECT version() at
// construction.
//
// The table is created on first use with the supplied (or default)
// name. Records older than TTL are removed lazily on Begin — at most
// once per minute per store instance, not on every request.
//
// Schema (all dialects):
//
//	idempotency_keys(
//	    key            TEXT PRIMARY KEY,
//	    fingerprint    TEXT NOT NULL,
//	    status         INTEGER,           -- nullable while in-flight
//	    headers        TEXT,              -- JSON, http.Header shape
//	    body           BLOB,
//	    expires_at     TIMESTAMP/TIMESTAMPTZ NOT NULL,
//	    created_at     TIMESTAMP/TIMESTAMPTZ NOT NULL
//	)
type SQLIdempotencyStore struct {
	db            *sql.DB
	table         string
	ttl           time.Duration
	inFlightTTL   time.Duration
	dialect       string // "sqlite" or "postgres"
	dialectPinned bool
	lastReapUnix  atomic.Int64
}

// SQLIdempotencyOption configures the SQL store.
type SQLIdempotencyOption func(*SQLIdempotencyStore)

// WithSQLIdempotencyTable overrides the default "idempotency_keys" table name.
func WithSQLIdempotencyTable(name string) SQLIdempotencyOption {
	return func(s *SQLIdempotencyStore) { s.table = name }
}

// WithSQLIdempotencyTTL overrides the default 24h cached-response TTL.
func WithSQLIdempotencyTTL(d time.Duration) SQLIdempotencyOption {
	return func(s *SQLIdempotencyStore) { s.ttl = d }
}

// WithSQLIdempotencyInFlightTTL overrides the default 30s in-flight
// claim TTL. Set this above the worst-case handler latency: a slower
// handler whose claim expires mid-execution lets retries see "no row"
// and execute again.
func WithSQLIdempotencyInFlightTTL(d time.Duration) SQLIdempotencyOption {
	return func(s *SQLIdempotencyStore) { s.inFlightTTL = d }
}

// WithSQLIdempotencyDialect pins the SQL dialect ("postgres" or
// "sqlite") instead of running the auto-detection probe.
func WithSQLIdempotencyDialect(dialect string) SQLIdempotencyOption {
	return func(s *SQLIdempotencyStore) {
		s.dialect = dialect
		s.dialectPinned = true
	}
}

// NewSQLIdempotencyStore constructs a SQL-backed IdempotencyStore and
// ensures the backing table exists.
func NewSQLIdempotencyStore(db *sql.DB, opts ...SQLIdempotencyOption) (*SQLIdempotencyStore, error) {
	if db == nil {
		return nil, errors.New("idempotency: nil DB")
	}
	s := &SQLIdempotencyStore{
		db:          db,
		table:       "idempotency_keys",
		ttl:         24 * time.Hour,
		inFlightTTL: 30 * time.Second,
		dialect:     "sqlite",
	}
	for _, opt := range opts {
		opt(s)
	}
	if !safeIdent(s.table) {
		return nil, fmt.Errorf("idempotency: unsafe table name %q", s.table)
	}
	if s.dialect != "postgres" && s.dialect != "sqlite" {
		return nil, fmt.Errorf("idempotency: unsupported dialect %q (want postgres or sqlite)", s.dialect)
	}
	if !s.dialectPinned {
		var v string
		if err := db.QueryRow("SELECT version()").Scan(&v); err == nil {
			if strings.Contains(strings.ToLower(v), "postgresql") {
				s.dialect = "postgres"
			}
		}
	}
	if err := s.ensureTable(); err != nil {
		return nil, fmt.Errorf("ensure table: %w", err)
	}
	return s, nil
}

func (s *SQLIdempotencyStore) ensureTable() error {
	tsType := "DATETIME"
	if s.dialect == "postgres" {
		tsType = "TIMESTAMPTZ"
	}
	stmt := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		key         TEXT PRIMARY KEY,
		fingerprint TEXT NOT NULL,
		status      INTEGER,
		headers     TEXT,
		body        BLOB,
		expires_at  %s NOT NULL,
		created_at  %s NOT NULL
	)`, s.table, tsType, tsType)
	if _, err := s.db.Exec(stmt); err != nil {
		return err
	}
	if _, err := s.db.Exec(fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS %s_expires_idx ON %s (expires_at)",
		s.table, s.table,
	)); err != nil {
		return fmt.Errorf("idempotency: create expiry index: %w", err)
	}
	return nil
}

// Begin implements IdempotencyStore. The implementation is robust to
// concurrent inserts: an `INSERT … ON CONFLICT DO NOTHING` either
// claims the row (RowsAffected=1) or loses the race (RowsAffected=0),
// in which case we re-read and report the winner's state instead of
// surfacing a PK-violation error that would otherwise look like a
// generic store failure and bypass idempotency.
func (s *SQLIdempotencyStore) Begin(ctx context.Context, key, fingerprint string) (*IdempotentResponse, bool, error) {
	now := time.Now()
	// Rate-limited reap. Skipping the per-call DELETE keeps Begin cheap
	// at high load; expired rows still get removed at least once per
	// minute per store instance.
	if last := s.lastReapUnix.Load(); now.Unix()-last > 60 {
		if s.lastReapUnix.CompareAndSwap(last, now.Unix()) {
			// best-effort: authoritative reads filter expired rows; this
			// bounded reap only controls storage growth.
			_, _ = s.db.ExecContext(ctx,
				fmt.Sprintf("DELETE FROM %s WHERE expires_at <= %s", s.table, s.placeholder(1)),
				now,
			)
		}
	}

	// 1) Try to claim the row atomically. If another writer beat us to
	//    it, RowsAffected==0 and we fall through to a read.
	res, err := s.db.ExecContext(ctx, s.upsertClaimStmt(),
		key, fingerprint, now.Add(s.inFlightTTL), now,
	)
	if err != nil {
		return nil, false, err
	}
	rows, _ := res.RowsAffected()
	if rows >= 1 {
		// Fresh claim — we own the row.
		return nil, false, nil
	}

	// 2) Lost the race or there's an existing valid row. Read it.
	// Filter expired rows out of the read so a TTL'd entry never
	// replays — the reap is rate-limited and isn't authoritative.
	row := s.db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT fingerprint, status, headers, body FROM %s WHERE key = %s AND expires_at > %s",
			s.table, s.placeholder(1), s.placeholder(2)),
		key, now,
	)
	var storedFP string
	var status sql.NullInt64
	var headers sql.NullString
	var body []byte
	err = row.Scan(&storedFP, &status, &headers, &body)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// Existing row was expired (or reaped between the failed insert
		// and this read). Delete any stale row then retry the claim
		// once — second insert wins now that the conflict is gone.
		if _, deleteErr := s.db.ExecContext(ctx,
			fmt.Sprintf("DELETE FROM %s WHERE key = %s", s.table, s.placeholder(1)),
			key,
		); deleteErr != nil {
			return nil, false, fmt.Errorf("idempotency: delete expired claim: %w", deleteErr)
		}
		res, err = s.db.ExecContext(ctx, s.upsertClaimStmt(),
			key, fingerprint, now.Add(s.inFlightTTL), now,
		)
		if err != nil {
			return nil, false, err
		}
		rows, _ = res.RowsAffected()
		if rows >= 1 {
			return nil, false, nil
		}
		// Still racing — give up and report in-flight conservatively.
		return nil, false, ErrInFlight
	case err != nil:
		return nil, false, err
	}
	if storedFP != fingerprint {
		return nil, false, ErrFingerprintMismatch
	}
	if !status.Valid {
		return nil, false, ErrInFlight
	}
	hdr := http.Header{}
	if headers.Valid && headers.String != "" {
		_ = json.Unmarshal([]byte(headers.String), &hdr)
	}
	return &IdempotentResponse{
		Status: int(status.Int64),
		Header: hdr,
		Body:   append([]byte(nil), body...),
	}, true, nil
}

// Finish implements IdempotencyStore.
func (s *SQLIdempotencyStore) Finish(ctx context.Context, key string, resp *IdempotentResponse) error {
	if resp == nil {
		_, err := s.db.ExecContext(ctx,
			fmt.Sprintf("DELETE FROM %s WHERE key = %s", s.table, s.placeholder(1)),
			key,
		)
		return err
	}
	hdrJSON, err := json.Marshal(resp.Header)
	if err != nil {
		return err
	}
	expires := time.Now().Add(s.ttl)
	_, err = s.db.ExecContext(ctx, fmt.Sprintf(
		"UPDATE %s SET status = %s, headers = %s, body = %s, expires_at = %s WHERE key = %s",
		s.table,
		s.placeholder(1), s.placeholder(2), s.placeholder(3), s.placeholder(4), s.placeholder(5),
	), resp.Status, string(hdrJSON), resp.Body, expires, key)
	return err
}

func (s *SQLIdempotencyStore) upsertClaimStmt() string {
	// INSERT ON CONFLICT DO NOTHING: lets concurrent claims race
	// without one of them surfacing a PK-violation error. Returns
	// RowsAffected=1 on win, 0 on loss; callers re-read to discover
	// the winner's state.
	if s.dialect == "postgres" {
		return fmt.Sprintf(
			"INSERT INTO %s (key, fingerprint, expires_at, created_at) VALUES ($1,$2,$3,$4) ON CONFLICT (key) DO NOTHING",
			s.table,
		)
	}
	return fmt.Sprintf(
		"INSERT INTO %s (key, fingerprint, expires_at, created_at) VALUES (?,?,?,?) ON CONFLICT(key) DO NOTHING",
		s.table,
	)
}

func (s *SQLIdempotencyStore) placeholder(n int) string {
	if s.dialect == "postgres" {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

// reservedSQLIdentsMW is the middleware package's copy — keeping the
// list local avoids cross-package dependency for a 12-entry guard.
var reservedSQLIdentsMW = map[string]struct{}{
	"select": {}, "insert": {}, "update": {}, "delete": {},
	"drop": {}, "create": {}, "table": {}, "from": {}, "where": {},
	"users": {}, "user": {}, "migrations": {}, "sessions": {}, "accounts": {},
}

// safeIdent rejects unsafe table names, including SQL reserved words
// and leading-digit identifiers that some dialect parsers treat oddly.
func safeIdent(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	first := rune(name[0])
	if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') || first == '_') {
		return false
	}
	for _, r := range name {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
		if !ok {
			return false
		}
	}
	if _, bad := reservedSQLIdentsMW[strings.ToLower(name)]; bad {
		return false
	}
	return true
}
