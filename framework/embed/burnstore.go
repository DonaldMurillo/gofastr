package embed

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// BurnStore records which handshake nonces have been spent.
type BurnStore interface {
	// Burn atomically claims nonceID for grant until expires: the first caller gets
	// (grant, false), later ones the live stored grant or "" with replay=true.
	Burn(ctx context.Context, nonceID, grant string, expires time.Time) (issued string, replay bool, err error)

	// Prune deletes rows past the retention deadline they were burned with.
	Prune(ctx context.Context, now time.Time) error
}

// MemoryBurnStore is an in-process BurnStore, correct for one process only.
type MemoryBurnStore struct {
	mu   sync.Mutex
	rows map[string]memoryBurn

	pruneThreshold int
	pruneInterval  time.Duration
	lastPrune      time.Time
}

type memoryBurn struct {
	grant   string
	expires time.Time
}

const (
	defaultBurnPruneThreshold = 256
	defaultBurnPruneInterval  = time.Minute
)

// MemoryBurnStoreOption configures a MemoryBurnStore at construction.
type MemoryBurnStoreOption func(*MemoryBurnStore)

// WithBurnPrunePolicy sets the row count past which Burn sweeps (<= 0 keeps 256)
// and the minimum interval between sweeps (<= 0 removes it).
func WithBurnPrunePolicy(threshold int, interval time.Duration) MemoryBurnStoreOption {
	return func(s *MemoryBurnStore) {
		if threshold > 0 {
			s.pruneThreshold = threshold
		}
		s.pruneInterval = max(interval,
			0)
	}
}

// NewMemoryBurnStore returns an in-process burn store that prunes as it writes.
func NewMemoryBurnStore(opts ...MemoryBurnStoreOption) *MemoryBurnStore {
	s := &MemoryBurnStore{
		rows:           make(map[string]memoryBurn),
		pruneThreshold: defaultBurnPruneThreshold,
		pruneInterval:  defaultBurnPruneInterval,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Burn implements BurnStore.
func (s *MemoryBurnStore) Burn(_ context.Context, nonceID, grant string, expires time.Time) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if prior, ok := s.rows[nonceID]; ok {
		if time.Now().Before(prior.expires) {
			return prior.grant, true, nil
		}
		return "", true, nil
	}
	s.rows[nonceID] = memoryBurn{grant: grant, expires: expires}
	s.maybePruneLocked()
	if !time.Now().Before(expires) {
		return "", true, nil
	}
	return grant, false, nil
}

func (s *MemoryBurnStore) maybePruneLocked() {
	if len(s.rows) < s.pruneThreshold {
		return
	}
	if s.pruneInterval > 0 && !s.lastPrune.IsZero() && time.Since(s.lastPrune) < s.pruneInterval {
		return
	}
	s.pruneLocked(time.Now())
	s.lastPrune = time.Now()
}

func (s *MemoryBurnStore) pruneLocked(now time.Time) {
	for id, row := range s.rows {
		if !now.Before(row.expires) {
			delete(s.rows, id)
		}
	}
}

// Prune implements BurnStore.
func (s *MemoryBurnStore) Prune(_ context.Context, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	return nil
}

// PruneGrace is how long past its deadline SQLBurnStore.Prune keeps a row.
const PruneGrace = 5 * time.Minute

// DefaultBurnTable is the table [NewSQLBurnStore] creates unless overridden.
const DefaultBurnTable = "gofastr_embed_nonces"

// SQLBurnStore is a BurnStore backed by the app's database, correct across replicas.
type SQLBurnStore struct {
	db    db.Executor
	table string
}

// SQLBurnStoreOption configures [NewSQLBurnStore].
type SQLBurnStoreOption func(*sqlBurnConfig)

type sqlBurnConfig struct{ table string }

// WithBurnTable overrides the table name.
func WithBurnTable(name string) SQLBurnStoreOption {
	return func(c *sqlBurnConfig) { c.table = name }
}

// NewSQLBurnStore creates or upgrades the burn table and returns a store bound to it.
func NewSQLBurnStore(database *sql.DB, opts ...SQLBurnStoreOption) (*SQLBurnStore, error) {
	if database == nil {
		return nil, errors.New("embed: NewSQLBurnStore requires a database")
	}
	c := sqlBurnConfig{table: DefaultBurnTable}
	for _, o := range opts {
		o(&c)
	}
	if _, err := query.SafeIdent(c.table); err != nil {
		return nil, fmt.Errorf("embed: burn table %q: %w", c.table, err)
	}
	s := &SQLBurnStore{db: database, table: c.table}
	tsType := "DATETIME"
	pg := migrate.DetectDialect(database) == migrate.DialectPostgres
	if pg {
		tsType = "TIMESTAMP"
		if err := query.RefuseCaseKeptTables(context.Background(), database, s.table); err != nil {
			return nil, fmt.Errorf("embed: %w", err)
		}
	}
	stmt := fmt.Sprintf(
		//gofastr:allow(GOFASTR1415) the eraser below blanks the grant by subject; export leaves these rows out on purpose, since a restored row is a live grant
		`CREATE TABLE IF NOT EXISTS %s (`+
			`nonce_id TEXT PRIMARY KEY NOT NULL, grant_token TEXT NOT NULL, expires_at %s NOT NULL, `+
			`subject TEXT NOT NULL DEFAULT '')`,
		query.QuoteFolded(s.table), tsType,
	)
	if _, err := s.db.ExecContext(context.Background(), stmt); err != nil {
		return nil, fmt.Errorf("embed: create burn table: %w", err)
	}
	if err := s.addSubjectColumn(pg); err != nil {
		return nil, fmt.Errorf("embed: add burn subject column: %w", err)
	}
	datexport.RegisterEraser(datexport.DataEraser{
		Name: datexport.ScopedName("embed_nonces", s.table), Source: "embed", Table: s.table,
		Column: "subject", Mode: datexport.EraseAnonymize,
		ScrubColumns: []string{"grant_token"}, Tombstone: erasedGrant,
	})
	return s, nil
}

const erasedGrant = "[erased]"

func (s *SQLBurnStore) addSubjectColumn(pg bool) error {
	clause := "ADD COLUMN"
	if pg {
		clause = "ADD COLUMN IF NOT EXISTS"
	}
	stmt := fmt.Sprintf(`ALTER TABLE %s %s subject TEXT NOT NULL DEFAULT ''`, query.QuoteFolded(s.table), clause)
	_, err := s.db.ExecContext(context.Background(), stmt)
	if err != nil && !pg && strings.Contains(err.Error(), "duplicate column") {
		return nil
	}
	return err
}

func grantSubject(grant string) string {
	payload, _, ok := strings.Cut(strings.TrimPrefix(grant, GrantPrefix), ".")
	if !ok {
		return ""
	}
	raw, err := b64.DecodeString(payload)
	if err != nil {
		return ""
	}
	var c grantClaims
	if err := json.Unmarshal(raw, &c); err != nil {
		return ""
	}
	return c.Subject
}

// Burn implements BurnStore.
func (s *SQLBurnStore) Burn(ctx context.Context, nonceID, grant string, expires time.Time) (string, bool, error) {
	ins := fmt.Sprintf(
		`INSERT INTO %s (nonce_id, grant_token, expires_at, subject) VALUES ($1, $2, $3, $4) ON CONFLICT (nonce_id) DO NOTHING`,
		query.QuoteFolded(s.table))
	res, err := s.db.ExecContext(ctx, ins, nonceID, grant, expires.UTC(), grantSubject(grant))
	if err != nil {
		return "", false, fmt.Errorf("embed: burn nonce: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 1 {
		if !time.Now().Before(expires) {
			return "", true, nil
		}
		return grant, false, nil
	}
	sel := fmt.Sprintf(`SELECT grant_token, expires_at FROM %s WHERE nonce_id = $1`, query.QuoteFolded(s.table))
	var stored string
	var storedExp time.Time
	switch err := s.db.QueryRowContext(ctx, sel, nonceID).Scan(&stored, &storedExp); {
	case errors.Is(err, sql.ErrNoRows):
		return "", true, nil
	case err != nil:
		return "", false, fmt.Errorf("embed: read burned nonce: %w", err)
	}
	if stored != erasedGrant && time.Now().Before(storedExp) {
		return stored, true, nil
	}
	return "", true, nil
}

// Prune implements BurnStore.
func (s *SQLBurnStore) Prune(ctx context.Context, now time.Time) error {
	stmt := fmt.Sprintf(`DELETE FROM %s WHERE expires_at < $1`, query.QuoteFolded(s.table))
	if _, err := s.db.ExecContext(ctx, stmt, now.Add(-PruneGrace).UTC()); err != nil {
		return fmt.Errorf("embed: prune burned nonces: %w", err)
	}
	return nil
}
