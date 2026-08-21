package embed

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// BurnStore records which handshake nonces have been spent.
//
// The whole contract is [BurnStore.Burn], and its shape is chosen so that
// "already used" is decided by a unique constraint rather than by a read
// followed by a write. A read-then-write would race: two concurrent exchanges of
// the same nonce would both read "unused" and both mint a grant, which is
// precisely the shared-identity failure single-use exists to prevent.
type BurnStore interface {
	// Burn atomically claims nonceID for grant.
	//
	// First caller wins: it stores grant and returns (grant, false, nil).
	// A later caller arriving while the stored grant is still valid gets that
	// SAME grant back with replay=true. The exchange is idempotent, so a
	// prefetched iframe, a double-mounted loader or a page refresh does not
	// break the embed.
	// A caller arriving after the stored grant has expired gets
	// ("", true, nil): the nonce is spent and the idempotency window has
	// closed.
	Burn(ctx context.Context, nonceID, grant string, expires time.Time) (issued string, replay bool, err error)

	// Prune deletes rows past the retention deadline they were burned with.
	//
	// That deadline is the LATER of the grant's expiry and the nonce's: the
	// grant's, because replay has to keep returning it while it is valid; the
	// nonce's, because a row deleted while its nonce still verifies un-burns
	// it, and the next exchange mints a second, independent grant.
	Prune(ctx context.Context, now time.Time) error
}

// MemoryBurnStore is an in-process BurnStore.
//
// Correct on one replica AND across one process lifetime. Two replicas each
// keep their own map, so the same nonce can be spent once per replica, and a
// restart forgets every burn, so a nonce still inside its (short) TTL becomes
// spendable again. [NewSQLBurnStore] is the answer to both. Kept for tests and
// single-process apps, and named so the limitation is visible at the call site.
type MemoryBurnStore struct {
	mu   sync.Mutex
	rows map[string]memoryBurn

	// Opportunistic self-pruning. The embed host schedules no background
	// sweeper, so without this the rows map grew by one entry per nonce
	// exchange for the life of the process. Burn sweeps expired rows once the
	// live set crosses pruneThreshold AND at least pruneInterval has elapsed
	// since the last sweep, so a workload of all-live nonces does not scan the
	// map on every write. lastPrune is guarded by mu.
	pruneThreshold int
	pruneInterval  time.Duration
	lastPrune      time.Time
}

type memoryBurn struct {
	grant   string
	expires time.Time
}

// Defaults for opportunistic pruning. The threshold is a high-water mark: the
// map may grow past it between sweeps, but a sustained exchange rate cannot
// grow it without bound because every crossing re-runs the sweep.
const (
	defaultBurnPruneThreshold = 256
	defaultBurnPruneInterval  = time.Minute
)

// MemoryBurnStoreOption configures a MemoryBurnStore at construction.
type MemoryBurnStoreOption func(*MemoryBurnStore)

// WithBurnPrunePolicy overrides the opportunistic-prune high-water mark and
// minimum interval between sweeps. A threshold <= 0 keeps the default; an
// interval <= 0 disables the interval gate (sweep on every qualifying write).
func WithBurnPrunePolicy(threshold int, interval time.Duration) MemoryBurnStoreOption {
	return func(s *MemoryBurnStore) {
		if threshold > 0 {
			s.pruneThreshold = threshold
		}
		if interval > 0 {
			s.pruneInterval = interval
		} else {
			s.pruneInterval = 0 // no gate
		}
	}
}

// NewMemoryBurnStore returns an in-process burn store that opportunistically
// prunes expired rows on writes past a high-water mark.
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
		// Compare against the CALLER's clock reference via the row's own
		// expiry: an expired row is spent-and-closed, not reusable.
		if time.Now().Before(prior.expires) {
			return prior.grant, true, nil
		}
		return "", true, nil
	}
	// Record the burn either way: the row IS the tombstone, and refusing to
	// write it would leave the nonce unburnt and spendable by the next caller,
	// which is worse than what this guard prevents.
	s.rows[nonceID] = memoryBurn{grant: grant, expires: expires}
	// Opportunistic self-pruning: the embed host runs no background sweeper, so
	// this is what keeps the rows map bounded over the process lifetime. The
	// sweep fires only past the high-water mark and at most once per interval,
	// so a workload of all-live nonces does not scan on every write.
	s.maybePruneLocked()
	// But hand back a usable grant only if the deadline has not already passed.
	//
	// Verification happens before the burn and is not atomic with it, so a
	// request that verified a nonce and then stalled, a slow resolver, a
	// paused goroutine or a long GC, could arrive after Prune had removed the
	// winning row. Without this it would insert a fresh one and mint a SECOND
	// grant from a nonce already spent; with it, the tombstone is restored and
	// the caller is told the nonce is spent.
	if !time.Now().Before(expires) {
		return "", true, nil
	}
	return grant, false, nil
}

// maybePruneLocked runs a sweep when the high-water mark is crossed and the
// interval gate has elapsed. Caller holds s.mu.
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

// pruneLocked deletes every row whose retention deadline has passed. Caller
// holds s.mu. Shared by Prune (external) and maybePruneLocked (opportunistic).
func (s *MemoryBurnStore) pruneLocked(now time.Time) {
	for id, row := range s.rows {
		if !now.Before(row.expires) {
			delete(s.rows, id)
		}
	}
}

// Prune implements BurnStore. It remains the explicit, clock-injected sweep an
// external scheduler (or Host.Prune) can drive; Burn ALSO self-prunes
// opportunistically so the store stays bounded even without one.
func (s *MemoryBurnStore) Prune(_ context.Context, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	return nil
}

// PruneGrace is how far PAST a row's retention deadline Prune waits before
// deleting it.
//
// Burn refuses a claim whose deadline has passed, using the calling replica's
// clock. Prune deletes using its own. Two replicas whose clocks differ can
// therefore disagree about whether a row is still live, and the dangerous
// direction is a fast pruner deleting a row a slow verifier is about to need,
// which un-burns the nonce and lets it mint a second grant.
//
// A margin costs one extra row per spent nonce for its duration and removes the
// disagreement, which is the better trade: the rows are tiny and short-lived,
// and the alternative is a distributed clock assumption nothing enforces.
const PruneGrace = 5 * time.Minute

// DefaultBurnTable is the table [NewSQLBurnStore] creates unless overridden.
const DefaultBurnTable = "gofastr_embed_nonces"

// SQLBurnStore is a BurnStore backed by the app's database. Correct across
// replicas: the nonce id is the primary key, so the second INSERT of a nonce
// loses to the constraint no matter which replica issues it.
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

// NewSQLBurnStore creates the burn table if it does not exist and returns a
// store bound to it. The table is created here rather than through the entity
// migrator because it is framework plumbing, not app data, the same choice the
// auth battery's token tables make.
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
	if migrate.DetectDialect(database) == migrate.DialectPostgres {
		tsType = "TIMESTAMP"
	}
	stmt := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s (`+
			`nonce_id TEXT PRIMARY KEY, grant_token TEXT NOT NULL, expires_at %s NOT NULL)`,
		query.QuoteIdent(s.table), tsType,
	)
	if _, err := s.db.ExecContext(context.Background(), stmt); err != nil {
		return nil, fmt.Errorf("embed: create burn table: %w", err)
	}
	return s, nil
}

// Burn implements BurnStore.
//
// The INSERT is the claim. ON CONFLICT DO NOTHING makes a losing insert a
// zero-row success rather than an error, so the replay path is a plain follow-up
// SELECT instead of driver-specific constraint-error sniffing.
//
// expires is the retention deadline, not the grant's expiry. See BurnStore.
func (s *SQLBurnStore) Burn(ctx context.Context, nonceID, grant string, expires time.Time) (string, bool, error) {
	ins := fmt.Sprintf(
		`INSERT INTO %s (nonce_id, grant_token, expires_at) VALUES ($1, $2, $3) ON CONFLICT (nonce_id) DO NOTHING`,
		query.QuoteIdent(s.table))
	res, err := s.db.ExecContext(ctx, ins, nonceID, grant, expires.UTC())
	if err != nil {
		return "", false, fmt.Errorf("embed: burn nonce: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 1 {
		// The row is written either way: it IS the tombstone, and refusing to
		// write it would leave the nonce unburnt and spendable by the next
		// caller. But a claim whose retention deadline has already passed hands
		// back nothing usable.
		//
		// Verification happens before the burn and is not atomic with it, so a
		// request that verified a nonce and then stalled could otherwise arrive
		// after Prune removed the winning row, insert a fresh one, and mint a
		// SECOND grant from a nonce already spent. The residual cross-replica
		// clock skew is covered from the other side, by PruneGrace.
		if !time.Now().Before(expires) {
			return "", true, nil
		}
		return grant, false, nil
	}
	// Either the insert lost the race or the driver cannot report rows
	// affected. Both resolve the same way: whatever is stored is the answer.
	//
	// Note what this deliberately does NOT do: compare the stored grant to the
	// one we just minted and call a match "we won". Grants are a deterministic
	// function of their claims, so two exchanges of the same nonce inside the
	// same second mint byte-identical tokens. That comparison reported every
	// such replay as a first use. The replay flag is informational (the caller
	// answers both cases identically), so a driver that cannot report rows
	// affected loses only the label, never the single-use guarantee.
	sel := fmt.Sprintf(`SELECT grant_token, expires_at FROM %s WHERE nonce_id = $1`, query.QuoteIdent(s.table))
	var stored string
	var storedExp time.Time
	switch err := s.db.QueryRowContext(ctx, sel, nonceID).Scan(&stored, &storedExp); {
	case errors.Is(err, sql.ErrNoRows):
		// The row vanished between the insert and the read: only Prune
		// deletes, and it only deletes expired rows, so the nonce's window is
		// over. Treat it as spent rather than retrying the insert, which is
		// the only answer that cannot loop.
		return "", true, nil
	case err != nil:
		return "", false, fmt.Errorf("embed: read burned nonce: %w", err)
	}
	if time.Now().Before(storedExp) {
		return stored, true, nil
	}
	return "", true, nil
}

// Prune implements BurnStore.
func (s *SQLBurnStore) Prune(ctx context.Context, now time.Time) error {
	stmt := fmt.Sprintf(`DELETE FROM %s WHERE expires_at < $1`, query.QuoteIdent(s.table))
	if _, err := s.db.ExecContext(ctx, stmt, now.Add(-PruneGrace).UTC()); err != nil {
		return fmt.Errorf("embed: prune burned nonces: %w", err)
	}
	return nil
}
