package access

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/core/query"
)

// ErrRevokedByWildcard is returned by [GrantStore.Grant] for a permission a
// wildcard revocation it does not lift still covers. Grant the wildcard to lift it.
var ErrRevokedByWildcard = errors.New("access: a wildcard revocation covers the permission")

// GrantStore persists role grants and revocation tombstones in the database
// and mirrors each change into a live *RolePolicy. Call LoadInto once at boot.
type GrantStore struct {
	db     *sql.DB
	table  string
	policy *RolePolicy

	transitionMu sync.Mutex

	baseline map[string][]Permission

	fanoutMu  sync.Mutex
	nodeID    string
	send      func([]byte) // PublishQueue enqueue; nil when no fanout
	stopQueue func()       // stops the PublishQueue drainer

	dirtyMu  sync.Mutex
	dirty    map[string]bool // "" reloads every role
	wake     chan struct{}   // buffered(1); worker wakeups
	stopWork chan struct{}   // closed by stop() to end the worker
	workerWG sync.WaitGroup

	reconcileEvery time.Duration
}

const reloadTimeout = 5 * time.Second

const accessFanoutTopic = "gofastr.access"

type accessInvalidateMsg struct {
	Role string `json:"role"`
}

// GrantStoreOption configures a GrantStore.
type GrantStoreOption func(*GrantStore)

// WithGrantTable overrides the grants table name (default "access_grants");
// an unsafe identifier panics.
func WithGrantTable(name string) GrantStoreOption {
	return func(gs *GrantStore) {
		gs.table = query.MustIdent(name)
	}
}

// DefaultReconcileInterval is the full-reload period of a GrantStore with a fanout.
const DefaultReconcileInterval = 5 * time.Minute

// WithReconcileInterval sets how often a fanout-wired store reloads every role;
// zero or negative turns it off.
func WithReconcileInterval(d time.Duration) GrantStoreOption {
	return func(gs *GrantStore) {
		//gofastr:allow(optzero) zero or negative is the documented "reconcile off"; refreshWorker arms its ticker only when reconcile > 0
		gs.reconcileEvery = d
	}
}

// NewGrantStore returns a store bound to policy, which may be nil until LoadInto.
func NewGrantStore(db *sql.DB, policy *RolePolicy, opts ...GrantStoreOption) *GrantStore {
	gs := &GrantStore{
		db:             db,
		table:          "access_grants",
		policy:         policy,
		reconcileEvery: DefaultReconcileInterval,
	}
	for _, opt := range opts {
		opt(gs)
	}
	return gs
}

// Policy returns the live policy the store mutates, or nil before one is bound.
func (s *GrantStore) Policy() *RolePolicy {
	return s.policy
}

// EnsureSchema creates the grants and tombstone tables if missing.
func (s *GrantStore) EnsureSchema(ctx context.Context) error {
	if query.IsPostgres(s.db) {
		if err := query.RefuseCaseKeptTables(ctx, s.db, s.table, s.revokedTable()); err != nil {
			return fmt.Errorf("access: %w", err)
		}
	}
	stmt := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s (role TEXT NOT NULL, permission TEXT NOT NULL, UNIQUE(role, permission))",
		query.QuoteFolded(s.table),
	)
	if _, err := s.db.ExecContext(ctx, stmt); err != nil {
		return err
	}
	revStmt := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s (role TEXT NOT NULL, permission TEXT NOT NULL, UNIQUE(role, permission))",
		query.QuoteFolded(s.revokedTable()),
	)
	if _, err := s.db.ExecContext(ctx, revStmt); err != nil {
		return err
	}
	s.registerExport()
	return nil
}

func (s *GrantStore) registerExport() {
	for _, t := range [...]struct{ base, table string }{
		{"access_grants", s.table},
		{"access_grants_revoked", s.revokedTable()},
	} {
		datexport.Register(datexport.DataExporter{
			Name: datexport.ScopedName(t.base, t.table), Source: "access", Table: t.table,
			PrimaryKey: "role,permission", Columns: []string{"role", "permission"},
		})
	}
}

// LoadInto binds policy (nil keeps the current one), captures its code grants
// as the baseline and overlays the persisted grants minus the tombstones.
func (s *GrantStore) LoadInto(ctx context.Context, policy *RolePolicy) error {
	if policy != nil {
		s.policy = policy
	}
	if s.policy == nil {
		return fmt.Errorf("access: GrantStore.LoadInto called with no policy (pass a *RolePolicy or construct with NewGrantStore(db, policy))")
	}
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()

	tombstones, err := s.allTombstones(ctx)
	if err != nil {
		return err
	}
	for role, ts := range tombstones {
		tombstones[role] = s.policy.prepareRevokes(ts)
	}
	snap := s.policy.Snapshot()
	s.fanoutMu.Lock()
	s.baseline = snap
	s.fanoutMu.Unlock()
	q := fmt.Sprintf("SELECT role, permission FROM %s", query.QuoteFolded(s.table))
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return fmt.Errorf("access: load grants: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var role, perm string
		if err := rows.Scan(&role, &perm); err != nil {
			return fmt.Errorf("access: scan grant row: %w", err)
		}
		if permIn(tombstones[role], Permission(perm)) {
			continue
		}
		if err := s.policy.Grant(role, Permission(perm)); err != nil {
			return fmt.Errorf("access: load grant %q→%q: %w", role, perm, err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("access: iterate grants: %w", err)
	}
	for role, ts := range tombstones {
		held := s.policy.PermissionsOf(role)
		toRevoke := make([]Permission, 0, len(ts))
		for _, p := range ts {
			if permIn(held, p) {
				toRevoke = append(toRevoke, p)
			}
		}
		if len(toRevoke) > 0 {
			s.policy.Revoke(role, toRevoke...)
		}
	}
	return nil
}

// Grant persists perms for role, lifts their tombstones and updates the live
// policy. It errors on an empty role, a strict-mode unknown capability, or
// [ErrRevokedByWildcard], writing nothing.
func (s *GrantStore) Grant(ctx context.Context, role string, perms ...Permission) error {
	if s.policy == nil {
		return fmt.Errorf("access: GrantStore has no policy: call LoadInto first")
	}
	if role == "" {
		return fmt.Errorf("access: Grant requires a non-empty role name")
	}
	if len(perms) == 0 {
		return nil
	}
	prepared, err := s.policy.prepareGrants(perms)
	if err != nil {
		return err
	}
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()
	lifted, err := s.refuseCoveredGrant(ctx, role, perms, prepared)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("access: persist grant %q: begin: %w", role, err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, permission := range prepared {
		q := fmt.Sprintf(
			"INSERT INTO %s (role, permission) VALUES ($1, $2) ON CONFLICT DO NOTHING",
			query.QuoteFolded(s.table),
		)
		if _, err := tx.ExecContext(ctx, q, role, string(permission)); err != nil {
			return fmt.Errorf("access: persist grant %q→%q: %w", role, permission, err)
		}
	}
	for _, permission := range lifted {
		q := fmt.Sprintf(
			"DELETE FROM %s WHERE role = $1 AND permission = $2",
			query.QuoteFolded(s.revokedTable()),
		)
		if _, err := tx.ExecContext(ctx, q, role, string(permission)); err != nil {
			return fmt.Errorf("access: clear revoke tombstone %q→%q: %w", role, permission, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("access: persist grant %q: commit: %w", role, err)
	}
	s.policy.grantPrepared(role, prepared)
	s.publish(role)
	return nil
}

// CheckGrant returns the error Grant would return for the same call, writing nothing.
func (s *GrantStore) CheckGrant(ctx context.Context, role string, perms ...Permission) error {
	if s.policy == nil {
		return fmt.Errorf("access: GrantStore has no policy: call LoadInto first")
	}
	if role == "" {
		return fmt.Errorf("access: Grant requires a non-empty role name")
	}
	if len(perms) == 0 {
		return nil
	}
	prepared, err := s.policy.prepareGrants(perms)
	if err != nil {
		return err
	}
	_, err = s.refuseCoveredGrant(ctx, role, perms, prepared)
	return err
}

func (s *GrantStore) refuseCoveredGrant(ctx context.Context, role string, perms, prepared []Permission) ([]Permission, error) {
	lifted := s.policy.prepareRevokes(perms)
	tombstones, err := s.tombstonesForRole(ctx, role)
	if err != nil {
		return nil, err
	}
	for _, stone := range tombstones {
		if permIn(lifted, stone) {
			continue
		}
		for _, covered := range s.policy.prepareRevokes([]Permission{stone}) {
			if permIn(prepared, covered) {
				return nil, fmt.Errorf("%w: role %q, %q revokes %q; grant %q, then revoke what the role should not hold",
					ErrRevokedByWildcard, role, stone, covered, stone)
			}
		}
	}
	return lifted, nil
}

// Revoke deletes perms (wildcards expanded) for role, tombstones each, and
// updates the live policy. It errors on an empty role.
func (s *GrantStore) Revoke(ctx context.Context, role string, perms ...Permission) error {
	if s.policy == nil {
		return fmt.Errorf("access: GrantStore has no policy: call LoadInto first")
	}
	if role == "" {
		return fmt.Errorf("access: Revoke requires a non-empty role name")
	}
	if len(perms) == 0 {
		return nil
	}
	prepared := s.policy.prepareRevokes(perms)
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("access: persist revoke %q: begin: %w", role, err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, p := range prepared {
		q := fmt.Sprintf(
			"DELETE FROM %s WHERE role = $1 AND permission = $2",
			query.QuoteFolded(s.table),
		)
		if _, err := tx.ExecContext(ctx, q, role, string(p)); err != nil {
			return fmt.Errorf("access: persist revoke %q→%q: %w", role, p, err)
		}
	}
	for _, p := range prepared {
		q := fmt.Sprintf(
			"INSERT INTO %s (role, permission) VALUES ($1, $2) ON CONFLICT DO NOTHING",
			query.QuoteFolded(s.revokedTable()),
		)
		if _, err := tx.ExecContext(ctx, q, role, string(p)); err != nil {
			return fmt.Errorf("access: persist revoke tombstone %q→%q: %w", role, p, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("access: persist revoke %q: commit: %w", role, err)
	}
	s.policy.Revoke(role, prepared...)
	s.publish(role)
	return nil
}

// SetFanout propagates grants and revokes to other replicas over f and reloads
// roles they change. A nil f is a no-op. Call stop at shutdown.
func (s *GrantStore) SetFanout(f fanout.Fanout) (stop func(), err error) {
	if f == nil {
		return func() {}, nil
	}
	s.fanoutMu.Lock()
	s.nodeID = fanout.NewNodeID()
	s.send, s.stopQueue = fanout.PublishQueue(f, accessFanoutTopic, 0)
	s.dirty = make(map[string]bool)
	s.wake = make(chan struct{}, 1)
	s.stopWork = make(chan struct{})
	wake := s.wake
	stopWork := s.stopWork
	stopQueue := s.stopQueue
	s.fanoutMu.Unlock()

	cancel, subErr := f.Subscribe(accessFanoutTopic, func(payload []byte) {
		s.handleRemote(payload)
	})
	if subErr != nil {
		s.fanoutMu.Lock()
		s.send = nil
		s.stopQueue = nil
		s.dirty = nil
		s.nodeID = ""
		s.fanoutMu.Unlock()
		stopQueue()
		return func() {}, fmt.Errorf("access: subscribe fanout: %w", subErr)
	}

	s.workerWG.Add(1)
	go s.refreshWorker(wake, stopWork, s.reconcileEvery)

	var once sync.Once
	stop = func() {
		cancel()                            // stop delivery first
		once.Do(func() { close(stopWork) }) // end the worker (safe on repeat)
		s.workerWG.Wait()                   // drain the in-flight reload
		stopQueue()                         // stop the publish queue
	}
	return stop, nil
}

func (s *GrantStore) publish(role string) {
	s.fanoutMu.Lock()
	send := s.send
	nodeID := s.nodeID
	s.fanoutMu.Unlock()
	if send == nil {
		return
	}
	payload, _ := json.Marshal(accessInvalidateMsg{Role: role})
	send(fanout.Wrap(nodeID, payload))
}

func (s *GrantStore) handleRemote(raw []byte) {
	s.fanoutMu.Lock()
	ownNode := s.nodeID
	s.fanoutMu.Unlock()
	_, body, ok := fanout.UnwrapForeign(ownNode, raw)
	if !ok {
		return
	}
	var msg accessInvalidateMsg
	if err := json.Unmarshal(body, &msg); err != nil {
		return
	}
	s.markDirty(msg.Role)
}

func (s *GrantStore) markDirty(role string) {
	s.dirtyMu.Lock()
	if s.dirty == nil {
		s.dirtyMu.Unlock()
		return
	}
	s.dirty[role] = true
	wake := s.wake
	s.dirtyMu.Unlock()
	select {
	case wake <- struct{}{}:
	default:
	}
}

func (s *GrantStore) refreshWorker(wake, stopWork <-chan struct{}, reconcile time.Duration) {
	defer s.workerWG.Done()
	var tick <-chan time.Time
	if reconcile > 0 {
		t := time.NewTicker(reconcile)
		defer t.Stop()
		tick = t.C
	}
	for {
		select {
		case <-stopWork:
			return
		case <-wake:
		case <-tick:
			s.markDirty("") // "" reloads every role
		}
		if failed := s.drainDirty(); failed {
			select {
			case <-stopWork:
				return
			case <-time.After(reloadTimeout):
				s.markDirty("") // "" reloads everything still owed
			}
		}
	}
}

func (s *GrantStore) drainDirty() (anyFailed bool) {
	s.dirtyMu.Lock()
	roles := slices.Sorted(maps.Keys(s.dirty))
	clear(s.dirty)
	s.dirtyMu.Unlock()
	for _, role := range roles {
		ctx, cancel := context.WithTimeout(context.Background(), reloadTimeout)
		err := s.reloadRole(ctx, role)
		cancel()
		if err != nil {
			slog.Warn("access: fanout role reload failed: will retry",
				slog.String("role", role), slog.Any("err", err))
			s.dirtyMu.Lock()
			if s.dirty != nil {
				s.dirty[role] = true
			}
			s.dirtyMu.Unlock()
			anyFailed = true
		}
	}
	return anyFailed
}

func (s *GrantStore) reloadRole(ctx context.Context, role string) error {
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()
	if s.policy == nil || s.db == nil {
		return nil
	}
	if role == "" {
		return s.reloadAllLocked(ctx)
	}
	dbPerms, err := s.dbPermsForRole(ctx, role)
	if err != nil {
		return err
	}
	tombstones, err := s.tombstonesForRole(ctx, role)
	if err != nil {
		return err
	}
	s.policy.replacePrepared(role, s.mergeBaseline(role, dbPerms, tombstones))
	return nil
}

func (s *GrantStore) reloadAll(ctx context.Context) error {
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()
	return s.reloadAllLocked(ctx)
}

func (s *GrantStore) reloadAllLocked(ctx context.Context) error {
	byRole, err := s.allDBPerms(ctx)
	if err != nil {
		return err
	}
	tombstones, err := s.allTombstones(ctx)
	if err != nil {
		return err
	}
	roles := make(map[string]bool, len(byRole))
	s.fanoutMu.Lock()
	for r := range s.baseline {
		roles[r] = true
	}
	s.fanoutMu.Unlock()
	for r := range byRole {
		roles[r] = true
	}
	for r := range tombstones {
		roles[r] = true
	}
	for r := range roles {
		s.policy.replacePrepared(r, s.mergeBaseline(r, byRole[r], tombstones[r]))
	}
	return nil
}

func (s *GrantStore) dbPermsForRole(ctx context.Context, role string) ([]Permission, error) {
	q := fmt.Sprintf("SELECT permission FROM %s WHERE role = $1", query.QuoteFolded(s.table))
	rows, err := s.db.QueryContext(ctx, q, role)
	if err != nil {
		return nil, fmt.Errorf("access: reload role %q: %w", role, err)
	}
	defer rows.Close()
	var perms []Permission
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("access: scan role %q: %w", role, err)
		}
		perms = append(perms, Permission(p))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("access: iterate role %q: %w", role, err)
	}
	return perms, nil
}

func (s *GrantStore) allDBPerms(ctx context.Context) (map[string][]Permission, error) {
	q := fmt.Sprintf("SELECT role, permission FROM %s", query.QuoteFolded(s.table))
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("access: reload all: %w", err)
	}
	defer rows.Close()
	out := make(map[string][]Permission)
	for rows.Next() {
		var role, perm string
		if err := rows.Scan(&role, &perm); err != nil {
			return nil, fmt.Errorf("access: scan grant row: %w", err)
		}
		out[role] = append(out[role], Permission(perm))
	}
	return out, rows.Err()
}

// mergeBaseline is expand(baseline ∪ dbPerms) − expand(tombstones).
func (s *GrantStore) mergeBaseline(role string, dbPerms, tombstones []Permission) []Permission {
	s.fanoutMu.Lock()
	base := s.baseline[role]
	s.fanoutMu.Unlock()
	seen := make(map[Permission]bool, len(base)+len(dbPerms))
	out := make([]Permission, 0, len(base)+len(dbPerms))
	for _, p := range base {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, p := range dbPerms {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return subtractPerms(s.prepareReload(role, out), s.policy.prepareRevokes(tombstones))
}

func (s *GrantStore) prepareReload(role string, perms []Permission) []Permission {
	expanded, err := s.policy.prepareGrants(perms)
	if err == nil {
		return expanded
	}
	seen := make(map[Permission]bool, len(perms))
	out := make([]Permission, 0, len(perms))
	for _, p := range perms {
		one, err := s.policy.prepareGrants([]Permission{p})
		if err != nil {
			slog.Warn("access: reload dropped a grant this replica refuses",
				slog.String("role", role), slog.Any("err", err))
			continue
		}
		for _, q := range one {
			if !seen[q] {
				seen[q] = true
				out = append(out, q)
			}
		}
	}
	return out
}

func (s *GrantStore) revokedTable() string {
	return s.table + "_revoked"
}

func (s *GrantStore) tombstonesForRole(ctx context.Context, role string) ([]Permission, error) {
	q := fmt.Sprintf("SELECT permission FROM %s WHERE role = $1", query.QuoteFolded(s.revokedTable()))
	rows, err := s.db.QueryContext(ctx, q, role)
	if err != nil {
		return nil, fmt.Errorf("access: reload tombstones %q: %w", role, err)
	}
	defer rows.Close()
	var perms []Permission
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("access: scan tombstone %q: %w", role, err)
		}
		perms = append(perms, Permission(p))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("access: iterate tombstones %q: %w", role, err)
	}
	return perms, nil
}

func (s *GrantStore) allTombstones(ctx context.Context) (map[string][]Permission, error) {
	q := fmt.Sprintf("SELECT role, permission FROM %s", query.QuoteFolded(s.revokedTable()))
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("access: load tombstones: %w", err)
	}
	defer rows.Close()
	out := make(map[string][]Permission)
	for rows.Next() {
		var role, perm string
		if err := rows.Scan(&role, &perm); err != nil {
			return nil, fmt.Errorf("access: scan tombstone row: %w", err)
		}
		out[role] = append(out[role], Permission(perm))
	}
	return out, rows.Err()
}

func subtractPerms(perms, remove []Permission) []Permission {
	if len(remove) == 0 {
		return perms
	}
	drop := make(map[Permission]bool, len(remove))
	for _, p := range remove {
		drop[p] = true
	}
	out := perms[:0]
	for _, p := range perms {
		if !drop[p] {
			out = append(out, p)
		}
	}
	return out
}

func permIn(perms []Permission, want Permission) bool {
	return slices.Contains(perms, want)
}
