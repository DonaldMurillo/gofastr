package access

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/core/query"
)

// GrantStore persists role→permission grants to a database table so RBAC
// edits survive restarts. It wraps a live *RolePolicy: Grant/Revoke write
// the DB row AND mutate the in-memory policy in one call, keeping the two
// in sync. The policy's own RWMutex covers concurrent Can checks, so a
// Grant/Revoke call is "atomic enough", a reader may see the state
// before or after the change, never a torn map.
//
// The store holds a reference to the live *RolePolicy (store-holds-policy
// shape). Bind the policy at construction with NewGrantStore(db, policy),
// then call LoadInto once at boot to hydrate the policy from persisted
// rows. Subsequent Grant/Revoke calls mutate both layers.
//
// All role and permission VALUES are passed as $n bound parameters, never
// interpolated into SQL. The table name is validated via query.SafeIdent
// at construction time and quoted via query.QuoteIdent in every statement.
//
// Both SQLite (mattn/go-sqlite3) and PostgreSQL (lib/pq) accept $N
// placeholders and ON CONFLICT DO NOTHING, so the same SQL works on both.
type GrantStore struct {
	db     *sql.DB
	table  string
	policy *RolePolicy

	// transitionMu serialises every policy transition. Grant, Revoke,
	// reloadRole, reloadAll, LoadInto, across its full read→mutate span,
	// so a reload that read a stale DB snapshot can never run its
	// ReplaceRole after a newer local Grant/Revoke and silently undo it.
	// Deliberately NOT fanoutMu: fanoutMu guards transport state (send
	// queue, node id) and stays narrow. Lock ordering is always
	// transitionMu → fanoutMu; never the reverse.
	transitionMu sync.Mutex

	// baseline holds the CODE-defined grants captured at LoadInto time,
	// before DB rows are overlaid. Every reload rebuilds a role as
	// baseline ∪ DB, so a cross-replica refresh (or a local reconcile)
	// never wipes grants an app declared in code with policy.Grant. Local
	// revokes remove entries so a later reconcile cannot undo them. Access
	// under fanoutMu; replacements use fresh slices so reload snapshots remain
	// safe after releasing the lock.
	baseline map[string][]Permission

	// Cross-replica fanout plumbing, wired by SetFanout. All of it is nil
	// until then (single-process deployments pay nothing).
	//
	// Propagation is a REFRESH SIGNAL: a grant/revoke enqueues the role
	// name onto other replicas, which re-read the authoritative DB row.
	// The payload's data is never trusted.
	//
	// Two hard requirements from the fanout contract shape this design:
	//   - Publish must not block Grant/Revoke on a stalled backend, so we
	//     publish through fanout.PublishQueue (non-blocking enqueue).
	//   - The Subscribe callback must not block the delivery goroutine, so
	//     it only marks the role dirty + wakes a worker; the DB reload runs
	//     on the worker with a finite timeout. A slow reload can never wedge
	//     delivery and cause a distinct revoke to be dropped.
	fanoutMu  sync.Mutex
	nodeID    string
	send      func([]byte) // PublishQueue enqueue; nil when no fanout
	stopQueue func()       // stops the PublishQueue drainer

	dirtyMu  sync.Mutex
	dirty    map[string]bool // role names awaiting reload ("" = reload all)
	wake     chan struct{}   // buffered(1); worker wakeups
	stopWork chan struct{}   // closed by stop() to end the worker
	workerWG sync.WaitGroup
}

// reloadTimeout bounds each background role reload so a stalled DB can never
// wedge the refresh worker (and thereby drop later invalidations).
const reloadTimeout = 5 * time.Second

// accessFanoutTopic is the pub/sub lane grant/revoke invalidations ride on.
// A distinct topic keeps RBAC refresh signals from inter-leaving with the
// module-toggle and island lanes even when they share one fanout backend.
const accessFanoutTopic = "gofastr.access"

// accessInvalidateMsg is the fanout payload. Role names the role whose
// grants changed; an empty Role asks the receiver to reload every role.
// The body is treated as a refresh SIGNAL only, the receiver re-reads
// authoritative DB state and never trusts this struct's data.
type accessInvalidateMsg struct {
	Role string `json:"role"`
}

// GrantStoreOption configures a GrantStore.
type GrantStoreOption func(*GrantStore)

// WithGrantTable overrides the default table name ("access_grants").
// The name is validated via query.SafeIdent, an unsafe identifier
// panics at construction time, not at query time.
func WithGrantTable(name string) GrantStoreOption {
	return func(gs *GrantStore) {
		// MustIdent panics on unsafe identifiers; construction-time fail-fast
		// is the right posture for a config-time value.
		gs.table = query.MustIdent(name)
	}
}

// NewGrantStore creates a GrantStore bound to the given policy. The policy
// reference is retained. Grant/Revoke mutate it directly so concurrent
// Can checks see the change without a reload. Call LoadInto once at boot
// to hydrate the policy from persisted rows.
//
// A nil policy is allowed only if you intend to call LoadInto with a
// policy before any Grant/Revoke; Grant/Revoke on a store with a nil
// policy return an error.
func NewGrantStore(db *sql.DB, policy *RolePolicy, opts ...GrantStoreOption) *GrantStore {
	gs := &GrantStore{
		db:     db,
		table:  "access_grants",
		policy: policy,
	}
	for _, opt := range opts {
		opt(gs)
	}
	return gs
}

// Policy returns the live *RolePolicy the store mutates. May be nil if
// LoadInto has not yet been called and no policy was passed to
// NewGrantStore.
func (s *GrantStore) Policy() *RolePolicy {
	return s.policy
}

// EnsureSchema creates the grants table (and the revocation-tombstone table
// that backs cross-replica revocation) if they do not already exist.
// Idempotent (CREATE TABLE IF NOT EXISTS), so existing deployments need no
// migration. The column types (TEXT) are portable across SQLite and
// PostgreSQL. The (role, permission) pair has a UNIQUE constraint so
// INSERT ... ON CONFLICT DO NOTHING is a no-op for duplicates.
func (s *GrantStore) EnsureSchema(ctx context.Context) error {
	stmt := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s (role TEXT NOT NULL, permission TEXT NOT NULL, UNIQUE(role, permission))",
		query.QuoteIdent(s.table),
	)
	if _, err := s.db.ExecContext(ctx, stmt); err != nil {
		return err
	}
	// Revocation tombstones share the grants table's shape. A Revoke
	// inserts a row here; reloads and fresh boots subtract these from the
	// baseline ∪ DB union so a revoked grant stays revoked on every replica
	// and survives restarts. Derived from the configured grants table
	// (WithGrantTable) so a custom name gets a matching tombstone table.
	revStmt := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s (role TEXT NOT NULL, permission TEXT NOT NULL, UNIQUE(role, permission))",
		query.QuoteIdent(s.revokedTable()),
	)
	if _, err := s.db.ExecContext(ctx, revStmt); err != nil {
		return err
	}
	return nil
}

// LoadInto reads all persisted grant rows and calls policy.Grant for each,
// hydrating the live *RolePolicy from the database. The policy is also
// retained as the store's active policy (overwriting any previously bound
// one) so subsequent Grant/Revoke calls mutate it. Call once at boot,
// after constructing the policy and after EnsureSchema.
//
// Revocation tombstones are subtracted BOTH from the captured code baseline
// and from the installed DB grants, so a replica booting after a revoke does
// not resurrect the permission from its code seed.
//
// If the store was constructed with a policy and policy is nil, the
// store's existing policy is used.
func (s *GrantStore) LoadInto(ctx context.Context, policy *RolePolicy) error {
	if policy != nil {
		s.policy = policy
	}
	if s.policy == nil {
		return fmt.Errorf("access: GrantStore.LoadInto called with no policy (pass a *RolePolicy or construct with NewGrantStore(db, policy))")
	}
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()

	// Read tombstones under the transition lock so a concurrent Grant/Revoke
	// cannot change them between this read and the install (the stale-reload
	// race): a boot and a transition never interleave.
	tombstones, err := s.allTombstones(ctx)
	if err != nil {
		return err
	}
	// Capture the code-defined baseline BEFORE overlaying DB grants, then
	// subtract tombstones, a code-seeded grant revoked on another replica
	// must stay revoked on this one too.
	snap := s.policy.Snapshot()
	for role, ts := range tombstones {
		if base, ok := snap[role]; ok {
			snap[role] = subtractPerms(base, ts)
		}
	}
	s.fanoutMu.Lock()
	s.baseline = snap
	s.fanoutMu.Unlock()
	q := fmt.Sprintf("SELECT role, permission FROM %s", query.QuoteIdent(s.table))
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
		// A grant row with a live tombstone is an inconsistent write,
		// fail closed: the tombstone wins.
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
	// Apply tombstones to the LIVE policy too: the baseline above already
	// excludes them for future reloads, but a code-seeded grant is already
	// in the in-memory policy at boot, a replica booting after a revoke
	// must not resurrect it. Revoke any tombstoned permission the policy
	// currently holds.
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

// Grant validates and expands permissions, persists the resulting
// (role, permission) rows to the database (INSERT ... ON CONFLICT DO NOTHING),
// and then updates the live policy. Idempotent: granting an already-held
// permission is a no-op in both layers. In strict capability mode, validation
// happens before any database write.
//
// Grant also DELETES any matching revocation tombstone for the granted
// (role, perm) pairs, a re-grant is the ONE way to lift a prior revocation.
// A tombstoned permission stays revoked across replicas and restarts until
// Grant removes the tombstone, even if the code keeps declaring it.
//
// Role and permission are bound as $n parameters, never interpolated.
func (s *GrantStore) Grant(ctx context.Context, role string, perms ...Permission) error {
	if s.policy == nil {
		return fmt.Errorf("access: GrantStore has no policy: call LoadInto first")
	}
	// An empty role name is the fanout "reload everything" sentinel, never a
	// real grantable role. Reject it so a Grant/Revoke("", …) can't be
	// mistaken for a full-reload signal (and can't strand a permission that
	// the additive full-reload path could never remove).
	if role == "" {
		return fmt.Errorf("access: Grant requires a non-empty role name")
	}
	if len(perms) == 0 {
		return nil
	}
	// Validate + expand up front so a strict-mode rejection never persists a
	// row it would then refuse in memory. The EXPANDED set is what we persist
	// (stable across reloads, matches LoadInto), not the raw wildcard input.
	prepared, err := s.policy.prepareGrants(perms)
	if err != nil {
		return err
	}
	// Hold the transition lock across the DB writes and the policy mutation
	// so a concurrent reload reading a stale DB snapshot cannot land its
	// ReplaceRole after this grant and undo it.
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()
	// One INSERT per (role, perm) with ON CONFLICT DO NOTHING. A batch
	// VALUES clause would be marginally faster but complicates the
	// placeholder math; the grant matrix is small and admin-driven, so
	// clarity wins.
	for _, permission := range prepared {
		q := fmt.Sprintf(
			"INSERT INTO %s (role, permission) VALUES ($1, $2) ON CONFLICT DO NOTHING",
			query.QuoteIdent(s.table),
		)
		if _, err := s.db.ExecContext(ctx, q, role, string(permission)); err != nil {
			return fmt.Errorf("access: persist grant %q→%q: %w", role, permission, err)
		}
	}
	// A re-grant lifts a prior revocation: delete any tombstone for these
	// (role, perm) pairs so the next reload (on this replica or a peer) no
	// longer subtracts them.
	for _, permission := range prepared {
		q := fmt.Sprintf(
			"DELETE FROM %s WHERE role = $1 AND permission = $2",
			query.QuoteIdent(s.revokedTable()),
		)
		if _, err := s.db.ExecContext(ctx, q, role, string(permission)); err != nil {
			return fmt.Errorf("access: clear revoke tombstone %q→%q: %w", role, permission, err)
		}
	}
	// The DB write succeeded, apply the grant DIRECTLY to the live policy. A
	// local grant/revoke is an authoritative admin action on THIS replica and
	// mutates memory directly (an admin may revoke a grant that was seeded in
	// code and never persisted to the DB). The baseline ∪ DB reconcile is used
	// ONLY on the remote fanout path (reloadRole), where its job is to stop a
	// peer's refresh from wiping this replica's code-defined grants, never to
	// second-guess a local mutation.
	s.policy.grantPrepared(role, prepared)
	// Signal other replicas to re-read this role's grants from the DB
	// (non-blocking; a stalled bus never wedges the grant).
	s.publish(role)
	return nil
}

// Revoke deletes (role, permission) rows from the database, records a
// revocation tombstone for each, and then calls policy.Revoke on the live
// policy. Idempotent: revoking a permission the role doesn't hold is a
// no-op in both layers.
//
// Role and permission are bound as $n parameters, never interpolated.
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
	// Hold the transition lock across the DB writes and the policy mutation
	// so a concurrent reload cannot land its ReplaceRole after this revoke
	// and undo it.
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()
	for _, p := range perms {
		q := fmt.Sprintf(
			"DELETE FROM %s WHERE role = $1 AND permission = $2",
			query.QuoteIdent(s.table),
		)
		if _, err := s.db.ExecContext(ctx, q, role, string(p)); err != nil {
			return fmt.Errorf("access: persist revoke %q→%q: %w", role, p, err)
		}
	}
	// Record a tombstone for each revoked permission. The tombstone is shared
	// DB state, so every replica's reload (baseline ∪ DB) − tombstones and
	// every fresh boot's LoadInto subtracts it, a code-SEEDED grant revoked
	// here stays revoked on peers and on replicas that boot later. ON CONFLICT
	// DO NOTHING mirrors Grant's duplicate posture.
	for _, p := range perms {
		q := fmt.Sprintf(
			"INSERT INTO %s (role, permission) VALUES ($1, $2) ON CONFLICT DO NOTHING",
			query.QuoteIdent(s.revokedTable()),
		)
		if _, err := s.db.ExecContext(ctx, q, role, string(p)); err != nil {
			return fmt.Errorf("access: persist revoke tombstone %q→%q: %w", role, p, err)
		}
	}
	// A local revoke must also narrow the captured code baseline. Otherwise
	// the next peer-driven reconcile would merge the revoked grant back in.
	s.fanoutMu.Lock()
	base := s.baseline[role]
	filtered := make([]Permission, 0, len(base))
	for _, candidate := range base {
		revoked := false
		for _, permission := range perms {
			if candidate == permission {
				revoked = true
				break
			}
		}
		if !revoked {
			filtered = append(filtered, candidate)
		}
	}
	if len(filtered) == 0 {
		delete(s.baseline, role)
	} else {
		s.baseline[role] = filtered
	}
	s.fanoutMu.Unlock()
	// DB write succeeded, remove from the live policy DIRECTLY. A local
	// revoke is authoritative and removes the permission from memory even if
	// it was seeded in code (never in the DB), so an admin revoke takes
	// effect immediately. NOTE: with revocation tombstones, a code-seeded
	// grant's revocation DOES propagate to peers and DOES survive peer boots,
	// the tombstone row is shared DB state, subtracted by every reload and
	// every fresh LoadInto. Re-granting via GrantStore.Grant deletes the
	// tombstone and lifts the revocation (the one way to un-revoke); a
	// tombstoned permission stays revoked even if the code keeps declaring
	// it, until Grant() is called. DB intent outlives code declarations,
	// the same precedence the store already gives DB grants over the
	// baseline.
	s.policy.Revoke(role, perms...)
	// Signal other replicas to re-read (non-blocking).
	s.publish(role)
	return nil
}

// SetFanout attaches a cross-replica fanout so grant/revoke propagate to
// other replicas and remote grant/revoke re-read authoritative state into
// the local policy. Mirrors the wiring shape of [island.Manager.SetFanout]
// and [ModuleManager.subscribeFanout]: store the backend, mint a node id,
// subscribe to accessFanoutTopic, and return the unsubscribe func as stop.
// Call once at boot; the returned stop is registered by the framework as
// an OnStop drainer. A nil fanout makes SetFanout a no-op returning a no-op
// stop, so callers can unconditionally wire it regardless of topology.
func (s *GrantStore) SetFanout(f fanout.Fanout) (stop func(), err error) {
	if f == nil {
		return func() {}, nil
	}
	s.fanoutMu.Lock()
	s.nodeID = fanout.NewNodeID()
	// Non-blocking publish: a stalled backend must never wedge Grant/Revoke.
	s.send, s.stopQueue = fanout.PublishQueue(f, accessFanoutTopic, 0)
	// Dirty-set + worker: the Subscribe callback only marks a role dirty and
	// wakes the worker (it never blocks the delivery goroutine); the worker
	// performs the DB reload with a finite timeout, so a slow reload can't
	// wedge delivery and cause a distinct revoke to be dropped.
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
	go s.refreshWorker(wake, stopWork)

	var once sync.Once
	stop = func() {
		cancel()                            // stop delivery first
		once.Do(func() { close(stopWork) }) // end the worker (safe on repeat)
		s.workerWG.Wait()                   // drain the in-flight reload
		stopQueue()                         // stop the publish queue
	}
	return stop, nil
}

// publish enqueues an invalidation for role onto the non-blocking publish
// queue. No-op when no fanout is attached. The enqueue never blocks
// Grant/Revoke; a stalled backend drops frames (the queue's documented
// behavior), and a missed refresh heals on the next grant/revoke or restart.
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

// handleRemote processes an invalidation from another replica. The payload is
// a REFRESH SIGNAL only: its data is never trusted. It unwraps the envelope,
// drops its own echoes (nodeID == s.nodeID), and marks the named role dirty
// for the worker to reload, it does NOT reload inline, so a slow DB can't
// block the fanout delivery goroutine (which would overflow the bounded queue
// and drop later, distinct invalidations). Malformed payloads are ignored.
func (s *GrantStore) handleRemote(raw []byte) {
	s.fanoutMu.Lock()
	ownNode := s.nodeID
	s.fanoutMu.Unlock()
	fromNode, body, err := fanout.Unwrap(raw)
	if err != nil {
		return
	}
	if fromNode == ownNode {
		return // own publish, drop the echo
	}
	var msg accessInvalidateMsg
	if err := json.Unmarshal(body, &msg); err != nil {
		return
	}
	s.markDirty(msg.Role)
}

// markDirty records a role needing reload and wakes the worker without
// blocking (the wake channel is buffered(1); a pending wake already covers a
// fresh dirty entry).
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

// refreshWorker drains dirty roles off the fanout delivery path and reloads
// each with a finite timeout. A failed reload re-marks the role dirty and
// retries after a delay, so a distinct revoke can never disappear silently
// under DB pressure. Exits when stopWork is closed.
func (s *GrantStore) refreshWorker(wake, stopWork <-chan struct{}) {
	defer s.workerWG.Done()
	for {
		select {
		case <-stopWork:
			return
		case <-wake:
		}
		if failed := s.drainDirty(); failed {
			// Something failed to reload, schedule a retry so the dropped
			// refresh reconverges rather than waiting for the next unrelated
			// invalidation.
			select {
			case <-stopWork:
				return
			case <-time.After(reloadTimeout):
				s.markDirty("") // "" reloads everything still owed
			}
		}
	}
}

// drainDirty reloads every currently-dirty role. Returns true if any reload
// failed (and was re-marked dirty for retry).
func (s *GrantStore) drainDirty() (anyFailed bool) {
	for {
		s.dirtyMu.Lock()
		var role string
		found := false
		for r := range s.dirty {
			role, found = r, true
			break
		}
		if found {
			delete(s.dirty, role)
		}
		s.dirtyMu.Unlock()
		if !found {
			return anyFailed
		}
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
}

// reloadRole rebuilds role's effective permissions as
// (baseline ∪ DB) − tombstones and atomically replaces the live policy's
// view via [RolePolicy.ReplaceRole]. The code-defined baseline (captured at
// LoadInto) is always merged back, so a refresh never drops grants declared
// in code, but a revocation tombstone subtracts a permission from the
// union, so a revoke propagates to peers and stays revoked. An empty role
// triggers a convergent full reload. On DB error the policy is left
// unchanged and the error is returned (fail-safe: a missed reload is
// retried by the worker).
//
// The whole read→mutate span runs under transitionMu so a reload that read
// a stale DB snapshot cannot land after a newer local Grant/Revoke.
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
	return s.policy.ReplaceRole(role, s.mergeBaseline(role, dbPerms, tombstones)...)
}

// reloadAll rebuilds every role that has a baseline or DB grant as
// (baseline ∪ DB) − tombstones. Convergent (removes deleted DB grants and
// honours tombstones), unlike additive LoadInto. Never published in practice
// (Grant/Revoke reject empty roles), kept as a defensive full-reconcile
// path. reloadAll takes transitionMu and delegates to reloadAllLocked so
// reloadRole's empty-role branch can reuse it without re-locking.
func (s *GrantStore) reloadAll(ctx context.Context) error {
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()
	return s.reloadAllLocked(ctx)
}

// reloadAllLocked is reloadAll assuming transitionMu is already held.
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
	for r := range roles {
		if err := s.policy.ReplaceRole(r, s.mergeBaseline(r, byRole[r], tombstones[r])...); err != nil {
			return err
		}
	}
	return nil
}

// dbPermsForRole reads one role's persisted permissions.
func (s *GrantStore) dbPermsForRole(ctx context.Context, role string) ([]Permission, error) {
	q := fmt.Sprintf("SELECT permission FROM %s WHERE role = $1", query.QuoteIdent(s.table))
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

// allDBPerms reads every persisted grant grouped by role.
func (s *GrantStore) allDBPerms(ctx context.Context) (map[string][]Permission, error) {
	q := fmt.Sprintf("SELECT role, permission FROM %s", query.QuoteIdent(s.table))
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

// mergeBaseline returns (baseline[role] ∪ dbPerms) − tombstones, de-duplicated,
// baseline first. The result is what ReplaceRole installs for the role. If a
// permission is somehow both granted and tombstoned (inconsistent write), the
// tombstone wins, fail closed.
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
	return subtractPerms(out, tombstones)
}

// revokedTable is the revocation-tombstone table name, derived from the
// configured grants table. The grants table is validated at construction
// (query.MustIdent) and the "_revoked" suffix is all safe-identifier
// characters, so the result is safe to pass to query.QuoteIdent.
func (s *GrantStore) revokedTable() string {
	return s.table + "_revoked"
}

// tombstonesForRole reads one role's revocation tombstones. Mirrors
// dbPermsForRole against the <table>_revoked table.
func (s *GrantStore) tombstonesForRole(ctx context.Context, role string) ([]Permission, error) {
	q := fmt.Sprintf("SELECT permission FROM %s WHERE role = $1", query.QuoteIdent(s.revokedTable()))
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

// allTombstones reads every revocation tombstone grouped by role. Mirrors
// allDBPerms against the <table>_revoked table.
func (s *GrantStore) allTombstones(ctx context.Context) (map[string][]Permission, error) {
	q := fmt.Sprintf("SELECT role, permission FROM %s", query.QuoteIdent(s.revokedTable()))
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

// subtractPerms returns perms with every element also in remove dropped,
// preserving order. Used to apply revocation tombstones to a merged
// permission set (baseline ∪ DB). Reuses perms' backing array, so the
// caller must not keep aliasing the input slice after this returns.
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

// permIn reports whether want appears in perms (linear scan; grant and
// tombstone sets are small and admin-driven).
func permIn(perms []Permission, want Permission) bool {
	for _, p := range perms {
		if p == want {
			return true
		}
	}
	return false
}
