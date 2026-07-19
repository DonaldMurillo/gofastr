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
// Grant/Revoke call is "atomic enough" — a reader may see the state
// before or after the change, never a torn map.
//
// The store holds a reference to the live *RolePolicy (store-holds-policy
// shape). Bind the policy at construction with NewGrantStore(db, policy),
// then call LoadInto once at boot to hydrate the policy from persisted
// rows. Subsequent Grant/Revoke calls mutate both layers.
//
// All role and permission VALUES are passed as $n bound parameters — never
// interpolated into SQL. The table name is validated via query.SafeIdent
// at construction time and quoted via query.QuoteIdent in every statement.
//
// Both SQLite (mattn/go-sqlite3) and PostgreSQL (lib/pq) accept $N
// placeholders and ON CONFLICT DO NOTHING, so the same SQL works on both.
type GrantStore struct {
	db     *sql.DB
	table  string
	policy *RolePolicy

	// baseline holds the CODE-defined grants captured at LoadInto time,
	// before DB rows are overlaid. Every reload rebuilds a role as
	// baseline ∪ DB, so a cross-replica refresh (or a local reconcile)
	// never wipes grants an app declared in code with policy.Grant. Read
	// under fanoutMu (written once in LoadInto, read in reloadRole).
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
// The body is treated as a refresh SIGNAL only — the receiver re-reads
// authoritative DB state and never trusts this struct's data.
type accessInvalidateMsg struct {
	Role string `json:"role"`
}

// GrantStoreOption configures a GrantStore.
type GrantStoreOption func(*GrantStore)

// WithGrantTable overrides the default table name ("access_grants").
// The name is validated via query.SafeIdent — an unsafe identifier
// panics at construction time, not at query time.
func WithGrantTable(name string) GrantStoreOption {
	return func(gs *GrantStore) {
		// MustIdent panics on unsafe identifiers; construction-time fail-fast
		// is the right posture for a config-time value.
		gs.table = query.MustIdent(name)
	}
}

// NewGrantStore creates a GrantStore bound to the given policy. The policy
// reference is retained — Grant/Revoke mutate it directly so concurrent
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

// EnsureSchema creates the grants table if it does not already exist.
// Idempotent (CREATE TABLE IF NOT EXISTS). The column types (TEXT) are
// portable across SQLite and PostgreSQL. The (role, permission) pair has
// a UNIQUE constraint so INSERT ... ON CONFLICT DO NOTHING is a no-op for
// duplicates.
func (s *GrantStore) EnsureSchema(ctx context.Context) error {
	stmt := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s (role TEXT NOT NULL, permission TEXT NOT NULL, UNIQUE(role, permission))",
		query.QuoteIdent(s.table),
	)
	_, err := s.db.ExecContext(ctx, stmt)
	return err
}

// LoadInto reads all persisted grant rows and calls policy.Grant for each,
// hydrating the live *RolePolicy from the database. The policy is also
// retained as the store's active policy (overwriting any previously bound
// one) so subsequent Grant/Revoke calls mutate it. Call once at boot,
// after constructing the policy and after EnsureSchema.
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
	// Capture the code-defined baseline BEFORE overlaying DB grants, so a
	// later reload merges baseline ∪ DB instead of replacing a role with only
	// its DB rows (which would drop grants an app declared with policy.Grant).
	s.fanoutMu.Lock()
	s.baseline = s.policy.Snapshot()
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
		if err := s.policy.Grant(role, Permission(perm)); err != nil {
			return fmt.Errorf("access: load grant %q→%q: %w", role, perm, err)
		}
	}
	return rows.Err()
}

// Grant validates and expands permissions, persists the resulting
// (role, permission) rows to the database (INSERT ... ON CONFLICT DO NOTHING),
// and then updates the live policy. Idempotent: granting an already-held
// permission is a no-op in both layers. In strict capability mode, validation
// happens before any database write.
//
// Role and permission are bound as $n parameters — never interpolated.
func (s *GrantStore) Grant(ctx context.Context, role string, perms ...Permission) error {
	if s.policy == nil {
		return fmt.Errorf("access: GrantStore has no policy — call LoadInto first")
	}
	// An empty role name is the fanout "reload everything" sentinel — never a
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
	// (stable across reloads — matches LoadInto), not the raw wildcard input.
	prepared, err := s.policy.prepareGrants(perms)
	if err != nil {
		return err
	}
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
	// The DB write succeeded — apply the grant DIRECTLY to the live policy. A
	// local grant/revoke is an authoritative admin action on THIS replica and
	// mutates memory directly (an admin may revoke a grant that was seeded in
	// code and never persisted to the DB). The baseline ∪ DB reconcile is used
	// ONLY on the remote fanout path (reloadRole), where its job is to stop a
	// peer's refresh from wiping this replica's code-defined grants — never to
	// second-guess a local mutation.
	s.policy.grantPrepared(role, prepared)
	// Signal other replicas to re-read this role's grants from the DB
	// (non-blocking; a stalled bus never wedges the grant).
	s.publish(role)
	return nil
}

// Revoke deletes (role, permission) rows from the database and then calls
// policy.Revoke on the live policy. Idempotent: revoking a permission the
// role doesn't hold is a no-op in both layers.
//
// Role and permission are bound as $n parameters — never interpolated.
func (s *GrantStore) Revoke(ctx context.Context, role string, perms ...Permission) error {
	if s.policy == nil {
		return fmt.Errorf("access: GrantStore has no policy — call LoadInto first")
	}
	if role == "" {
		return fmt.Errorf("access: Revoke requires a non-empty role name")
	}
	if len(perms) == 0 {
		return nil
	}
	for _, p := range perms {
		q := fmt.Sprintf(
			"DELETE FROM %s WHERE role = $1 AND permission = $2",
			query.QuoteIdent(s.table),
		)
		if _, err := s.db.ExecContext(ctx, q, role, string(p)); err != nil {
			return fmt.Errorf("access: persist revoke %q→%q: %w", role, p, err)
		}
	}
	// DB write succeeded — remove from the live policy DIRECTLY. A local
	// revoke is authoritative and removes the permission from memory even if
	// it was seeded in code (never in the DB), so an admin revoke takes effect
	// immediately. NOTE: a code-SEEDED grant lives only in this replica's
	// memory, so its revocation does not propagate to peers via the DB
	// refresh-signal — seed store-managed grants via GrantStore.Grant (which
	// persists them) if they must be revocable across replicas.
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
// for the worker to reload — it does NOT reload inline, so a slow DB can't
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
		return // own publish — drop the echo
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
			// Something failed to reload — schedule a retry so the dropped
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
			slog.Warn("access: fanout role reload failed — will retry",
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

// reloadRole rebuilds role's effective permissions as baseline ∪ DB and
// atomically replaces the live policy's view via [RolePolicy.ReplaceRole].
// The code-defined baseline (captured at LoadInto) is always merged back, so
// a refresh never drops grants declared in code. An empty role triggers a
// convergent full reload. On DB error the policy is left unchanged and the
// error is returned (fail-safe: a missed reload is retried by the worker).
func (s *GrantStore) reloadRole(ctx context.Context, role string) error {
	if s.policy == nil || s.db == nil {
		return nil
	}
	if role == "" {
		return s.reloadAll(ctx)
	}
	dbPerms, err := s.dbPermsForRole(ctx, role)
	if err != nil {
		return err
	}
	return s.policy.ReplaceRole(role, s.mergeBaseline(role, dbPerms)...)
}

// reloadAll rebuilds every role that has a baseline or DB grant as
// baseline ∪ DB. Convergent (removes deleted DB grants), unlike additive
// LoadInto. Never published in practice (Grant/Revoke reject empty roles) —
// kept as a defensive full-reconcile path.
func (s *GrantStore) reloadAll(ctx context.Context) error {
	byRole, err := s.allDBPerms(ctx)
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
		if err := s.policy.ReplaceRole(r, s.mergeBaseline(r, byRole[r])...); err != nil {
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

// mergeBaseline returns baseline[role] ∪ dbPerms, de-duplicated, baseline
// first. The result is what ReplaceRole installs for the role.
func (s *GrantStore) mergeBaseline(role string, dbPerms []Permission) []Permission {
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
	return out
}
