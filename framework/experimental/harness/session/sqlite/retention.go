package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// DropMetadata removes event rows entirely (not just their content)
// whose ts is older than the given age. Returns the count of deleted
// rows.
//
// Default policy per § Persistence: drop metadata-only rows after
// 180 days. Pinned sessions (tracked separately) survive.
func (s *Store) DropMetadata(ctx context.Context, age time.Duration, exemptSessions []string) (int64, error) {
	cutoff := time.Now().Add(-age).UTC().Format(time.RFC3339Nano)
	query := `DELETE FROM events WHERE ts < ?`
	args := []any{cutoff}
	if len(exemptSessions) > 0 {
		placeholders := make([]byte, 0, len(exemptSessions)*2-1)
		for i := range exemptSessions {
			if i > 0 {
				placeholders = append(placeholders, ',')
			}
			placeholders = append(placeholders, '?')
			args = append(args, exemptSessions[i])
		}
		query += " AND session NOT IN (" + string(placeholders) + ")"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ---------- Monthly rolling log files ----------

// MonthlyRollover provides per-month log files
// (sessions-YYYYMM.db) for retention efficiency: VACUUM only touches
// the current month's file, not the whole history.
//
// The current month's file is the "live" Store; older months are
// closed and become read-only archives. Open queries can span
// archives by iterating Open() on each month file.
type MonthlyRollover struct {
	mu      sync.Mutex
	dir     string
	current *Store
	month   string // YYYYMM the current store was opened for
}

// NewMonthlyRollover returns a rollover manager rooted at dir. The
// current month's store is opened immediately.
func NewMonthlyRollover(dir string) (*MonthlyRollover, error) {
	m := &MonthlyRollover{dir: dir}
	if err := m.openCurrentLocked(); err != nil {
		return nil, err
	}
	return m, nil
}

// Current returns the live Store, rolling over if the calendar month
// has changed since the last call.
func (m *MonthlyRollover) Current() (*Store, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.shouldRoll() {
		if m.current != nil {
			_ = m.current.Close()
			m.current = nil
		}
		if err := m.openCurrentLocked(); err != nil {
			return nil, err
		}
	}
	return m.current, nil
}

// ListArchives returns the paths of every month file in the directory
// (current + archives), oldest first.
func (m *MonthlyRollover) ListArchives() ([]string, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if len(name) >= len("sessions-YYYYMM.db") && name[:9] == "sessions-" && filepath.Ext(name) == ".db" {
			out = append(out, filepath.Join(m.dir, name))
		}
	}
	// alphabetical sort happens to be chronological with YYYYMM.
	slices.Sort(out)
	return out, nil
}

// Close releases the live Store.
func (m *MonthlyRollover) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current != nil {
		err := m.current.Close()
		m.current = nil
		return err
	}
	return nil
}

func (m *MonthlyRollover) shouldRoll() bool {
	return m.current == nil || m.month != time.Now().UTC().Format("200601")
}

func (m *MonthlyRollover) openCurrentLocked() error {
	month := time.Now().UTC().Format("200601")
	path := filepath.Join(m.dir, "sessions-"+month+".db")
	s, err := Open(path)
	if err != nil {
		return err
	}
	m.current = s
	m.month = month
	return nil
}

// ---------- Cost-ledger split into its own non-encrypted DB ----------

// CostLedger is a separate SQLite DB that records cost rows
// independently from the event log. Survives VACUUM on the main
// session DB and is queryable directly for the cost dashboard.
type CostLedger struct {
	db   *sql.DB
	path string
	mu   sync.Mutex
}

// OpenCostLedger opens the cost ledger DB.
func OpenCostLedger(path string) (*CostLedger, error) {
	// Owner-only, matching the sibling event store: the ledger records
	// per-session spend (session ids, providers, models, token counts).
	// The WAL sidecars inherit the main db's mode on creation.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	// Owner-only, matching the sibling event store: the ledger records
	// per-session spend (session ids, providers, models, token counts).
	// The WAL sidecars inherit the main db's mode on creation, and
	// SeedOwnerOnly also tightens a pre-existing weaker file (the
	// event store's Open does the same through the shared helper).
	if err := fileperm.SeedOwnerOnly(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", path+"?_journal=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS cost_rows (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    session       TEXT NOT NULL,
    ts            TEXT NOT NULL,
    provider      TEXT NOT NULL,
    model         TEXT NOT NULL,
    input_tokens  INTEGER NOT NULL,
    output_tokens INTEGER NOT NULL,
    cache_tokens  INTEGER NOT NULL,
    usd           REAL NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_cost_session_ts ON cost_rows (session, ts);
CREATE INDEX IF NOT EXISTS idx_cost_provider   ON cost_rows (provider);
`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("cost ledger schema: %w", err)
	}
	return &CostLedger{db: db, path: path}, nil
}

// Record inserts one cost row.
func (c *CostLedger) Record(ctx context.Context, session, provider, model string, inputT, outputT, cacheT int, usd float64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.db.ExecContext(ctx, `
        INSERT INTO cost_rows(session, ts, provider, model, input_tokens, output_tokens, cache_tokens, usd)
        VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		session, time.Now().UTC().Format(time.RFC3339Nano),
		provider, model, inputT, outputT, cacheT, usd,
	)
	return err
}

// CostByProvider returns USD totals grouped by provider since `since`.
func (c *CostLedger) CostByProvider(ctx context.Context, since time.Time) (map[string]float64, error) {
	return c.groupedUSD(ctx, since, `
        SELECT provider, SUM(usd) FROM cost_rows
         WHERE ts >= ? GROUP BY provider`)
}

// CostBySession returns USD totals grouped by session since `since`.
func (c *CostLedger) CostBySession(ctx context.Context, since time.Time) (map[string]float64, error) {
	return c.groupedUSD(ctx, since, `
        SELECT session, SUM(usd) FROM cost_rows
         WHERE ts >= ? GROUP BY session`)
}

// groupedUSD runs a (key, SUM(usd)) GROUP BY query over cost_rows since
// the given time and collects the per-key totals. It replaces the
// duplicated query-and-scan bodies of CostByProvider and CostBySession,
// which differed only in the grouped column.
func (c *CostLedger) groupedUSD(ctx context.Context, since time.Time, query string) (map[string]float64, error) {
	rows, err := c.db.QueryContext(ctx, query, since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]float64)
	for rows.Next() {
		var key string
		var usd float64
		if err := rows.Scan(&key, &usd); err != nil {
			return nil, err
		}
		out[key] = usd
	}
	return out, rows.Err()
}

// Close releases the DB.
func (c *CostLedger) Close() error { return c.db.Close() }

// ErrLedgerClosed is returned when methods are called after Close.
var ErrLedgerClosed = errors.New("sqlite: cost ledger closed")

// DefaultCostLedgerPath returns the canonical XDG state location.
func DefaultCostLedgerPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "gofastr", "harness", "cost-ledger.db")
}

// Compile-time sanity that fmt.Sprintf isn't accidentally unused.
var _ = fmt.Sprintf
