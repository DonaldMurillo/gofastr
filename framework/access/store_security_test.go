package access

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// Property: two replicas (GrantStores) sharing one database must converge to
// a single verdict for a (role, perm) pair after a concurrent Grant on one
// and Revoke on the other — the fanout refresh path re-reads authoritative
// DB state, so once both replicas have reloaded, Can() must agree.
//
// The suspected defect: Grant and Revoke each write two untransacted
// statements (Grant: INSERT row, then DELETE tombstone; Revoke: DELETE row,
// then INSERT tombstone) and Revoke narrows only the LOCAL baseline. The
// interleave  G1(insert row) → R1(delete row) → R2(insert tombstone) →
// G2(delete tombstone)  leaves the DB looking like nothing happened (no
// row, no tombstone) while replica A's baseline still holds the code seed
// and replica B's was narrowed by its revoke: A reloads to granted, B to
// denied, permanently.
//
// The interleave is forced deterministically (no sleeps): a driver wrapper
// parks Grant's tombstone-clearing DELETE until Revoke has fully returned.

// ---- gated sqlite driver ----------------------------------------------------

// gateState parks the FIRST armed DELETE on the tombstone table until the
// test releases it, producing the G1 → R1 → R2 → G2 order.
type gateState struct {
	mu      sync.Mutex
	armed   bool
	parked  bool
	entered chan struct{}
	release chan struct{}
	seen    []string
}

func newGateState() *gateState {
	return &gateState{entered: make(chan struct{}), release: make(chan struct{})}
}

func (g *gateState) onExec(q string) {
	g.mu.Lock()
	g.seen = append(g.seen, q)
	armed := g.armed
	g.mu.Unlock()
	if !armed {
		return
	}
	if strings.Contains(q, "DELETE FROM") && strings.Contains(q, "_revoked") {
		g.mu.Lock()
		first := !g.parked
		g.parked = true
		g.mu.Unlock()
		if first {
			close(g.entered)
		}
		<-g.release
	}
}

func (g *gateState) arm()    { g.mu.Lock(); g.armed = true; g.mu.Unlock() }
func (g *gateState) disarm() { g.mu.Lock(); g.armed = false; g.mu.Unlock() }
func (g *gateState) queries() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.seen...)
}

// gateConn intercepts ExecContext so the test can park one statement.
type gateConn struct{ driver.Conn }

func (c *gateConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if h := gateForConns; h != nil {
		h.onExec(query)
	}
	if ex, ok := c.Conn.(driver.ExecerContext); ok {
		return ex.ExecContext(ctx, query, args)
	}
	st, err := c.Conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	vals := make([]driver.Value, len(args))
	for i, nv := range args {
		vals[i] = nv.Value
	}
	return st.Exec(vals)
}

type gatedDriver struct{ inner driver.Driver }

func (d gatedDriver) Open(dsn string) (driver.Conn, error) {
	c, err := d.inner.Open(dsn)
	if err != nil {
		return nil, err
	}
	return &gateConn{Conn: c}, nil
}

var (
	gateForConns    *gateState
	registerGatedDB sync.Once
)

// openGatedDB opens a file-backed sqlite DB through the gated driver. A
// file (not :memory:) so several pooled connections share one database,
// and more than one connection so the parked statement cannot starve the
// other replica's writes.
func openGatedDB(t *testing.T) (*sql.DB, *gateState) {
	t.Helper()
	gate := newGateState()
	gateForConns = gate
	registerGatedDB.Do(func() {
		sql.Register("access-gated-sqlite", gatedDriver{inner: &stdlib.SQLiteDriver{}})
	})
	dsn := "file:" + filepath.Join(t.TempDir(), "grants.db") + "?_pragma=busy_timeout(5000)"
	db, err := sql.Open("access-gated-sqlite", dsn)
	if err != nil {
		t.Fatalf("open gated db: %v", err)
	}
	db.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = db.Close() })
	return db, gate
}

// TestGrantRevokeCrossReplicaConverge drives the recon's split-brain
// trigger with an explicit statement interleave and asserts both replicas
// agree after each has reloaded from the shared DB.
func TestGrantRevokeCrossReplicaConverge(t *testing.T) {
	db, gate := openGatedDB(t)
	ctx := context.Background()

	// Two replicas, both code-seeding editor→posts:read (real-boot shape).
	storeA, policyA := seedStore(t, db, "editor", Permission("posts:read"))
	storeB, policyB := seedStore(t, db, "editor", Permission("posts:read"))

	// In-process fanout, exactly the production wiring the recon trigger
	// describes: grant/revoke propagate by refresh signal.
	f := fanout.NewInProcess()
	stopA, err := storeA.SetFanout(f)
	if err != nil {
		t.Fatalf("SetFanout A: %v", err)
	}
	defer stopA()
	stopB, err := storeB.SetFanout(f)
	if err != nil {
		t.Fatalf("SetFanout B: %v", err)
	}
	defer stopB()

	// Deterministic interleave:
	//   A: Grant  → G1 INSERT grant row        (completes)
	//              G2 DELETE revoke tombstone  (PARKED)
	//   B: Revoke → R1 DELETE grant row        (completes)
	//              R2 INSERT revoke tombstone  (completes)
	//   release G2                                     (completes)
	// Final DB: no grant row, no tombstone.
	gate.arm()
	grantErr := make(chan error, 1)
	go func() { grantErr <- storeA.Grant(ctx, "editor", Permission("posts:read")) }()

	select {
	case <-gate.entered:
		// Grant has finished G1 and is parked before G2.
	case <-time.After(10 * time.Second):
		gate.disarm()
		t.Fatalf("gate never observed Grant's tombstone-clearing DELETE; queries seen: %v", gate.queries())
	}

	if err := storeB.Revoke(ctx, "editor", Permission("posts:read")); err != nil {
		t.Fatalf("Revoke on B: %v", err)
	}
	close(gate.release)
	if err := <-grantErr; err != nil {
		t.Fatalf("Grant on A: %v", err)
	}
	gate.disarm()

	// Force each replica to reconcile from the shared DB, the same
	// (baseline ∪ DB) − tombstones path the fanout worker drives.
	if err := storeA.reloadAll(ctx); err != nil {
		t.Fatalf("reloadAll A: %v", err)
	}
	if err := storeB.reloadAll(ctx); err != nil {
		t.Fatalf("reloadAll B: %v", err)
	}

	ctxA := WithRoles(WithPolicy(ctx, policyA), []string{"editor"})
	ctxB := WithRoles(WithPolicy(ctx, policyB), []string{"editor"})
	canA := policyA.Can(ctxA, Permission("posts:read"))
	canB := policyB.Can(ctxB, Permission("posts:read"))
	if canA != canB {
		var rows, tombs int
		_ = db.QueryRow(`SELECT COUNT(*) FROM "access_grants"`).Scan(&rows)
		_ = db.QueryRow(`SELECT COUNT(*) FROM "access_grants_revoked"`).Scan(&tombs)
		t.Fatalf("SECURITY: [access-fanout] cross-replica split-brain: after a successful Grant on A and "+
			"Revoke on B of the same (role, perm), the replicas' verdicts diverge — A Can(posts:read)=%v, "+
			"B Can(posts:read)=%v (grant rows=%d, tombstones=%d). The DB looks like nothing happened, but "+
			"Revoke narrowed only B's baseline while A's still holds the code seed, so every reload keeps "+
			"A fail-open. Root cause: Grant/Revoke write their row/tombstone statements untransacted, so the "+
			"G1→R1→R2→G2 interleave erases both; fix by wrapping each Grant/Revoke's two statements in one "+
			"transaction (or making reload treat an absent row AND absent tombstone deterministically from "+
			"shared state, not per-replica baselines).", canA, canB, rows, tombs)
	}
}

// ---------------------------------------------------------------------------
// Property: handleRemote treats a fanout payload as a REFRESH SIGNAL only —
// malformed payloads are dropped without panicking and without marking any
// role dirty, while a well-formed FOREIGN signal marks exactly its role.
// The delivery goroutine is shared with every other topic; a panic or a
// spurious full reload here wedges or floods the refresh path.
// ---------------------------------------------------------------------------

func TestHandleRemoteIgnoresMalformedSignals(t *testing.T) {
	db := openAccessDB(t)
	store, _ := seedStore(t, db, "editor", Permission("posts:read"))
	stop, err := store.SetFanout(fanout.NewInProcess())
	if err != nil {
		t.Fatalf("SetFanout: %v", err)
	}
	defer stop()

	malformed := [][]byte{
		nil,
		[]byte(""),
		[]byte("not json at all"),
		[]byte(`{"node":"broken`), // truncated envelope
	}
	for i, raw := range malformed {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("SECURITY: [access] handleRemote panicked on malformed payload #%d: %v", i, r)
				}
			}()
			store.handleRemote(raw)
		}()
	}

	// A well-formed envelope whose BODY is not the invalidate struct is
	// dropped too (json.Unmarshal error), not treated as role "".
	store.handleRemote(fanout.Wrap("foreign-node", []byte("garbage-body")))
	store.dirtyMu.Lock()
	dirty := len(store.dirty)
	store.dirtyMu.Unlock()
	if dirty != 0 {
		t.Errorf("malformed signals marked %d roles dirty, want 0", dirty)
	}

	// Control: a well-formed foreign signal for a role marks that role and
	// the worker drains it.
	store.handleRemote(fanout.Wrap("foreign-node", []byte(`{"role":"editor"}`)))
	if !pollAccessUntil(2*time.Second, func() bool { return storeRoleDirtyCleared(store) }) {
		t.Fatal("worker never drained the well-formed signal")
	}
}

// storeRoleDirtyCleared reports whether the dirty set is empty.
func storeRoleDirtyCleared(s *GrantStore) bool {
	s.dirtyMu.Lock()
	defer s.dirtyMu.Unlock()
	return len(s.dirty) == 0
}

// pollAccessUntil polls fn until true or the deadline passes.
func pollAccessUntil(timeout time.Duration, fn func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// ---------------------------------------------------------------------------
// Property: an empty-role signal ("") converges the WHOLE policy from
// authoritative DB state — a drifted in-memory grant the DB never saw is
// removed by the full reload, the same convergent path a failed reload
// retries through. The signal must never be able to GRANT anything itself.
// ---------------------------------------------------------------------------

func TestEmptyRoleSignalConvergesDrift(t *testing.T) {
	db := openAccessDB(t)
	store, policy := seedStore(t, db, "editor", Permission("posts:read"))

	stop, err := store.SetFanout(fanout.NewInProcess())
	if err != nil {
		t.Fatalf("SetFanout: %v", err)
	}
	defer stop()

	// Drift: an in-memory-only grant the DB and baseline never saw.
	if err := policy.Grant("editor", Permission("drift:only")); err != nil {
		t.Fatalf("drift grant: %v", err)
	}
	if len(policy.PermissionsOf("editor")) != 2 {
		t.Fatalf("setup: editor holds %v, want 2", policy.PermissionsOf("editor"))
	}

	// Foreign replica says "reload everything".
	store.handleRemote(fanout.Wrap("foreign-node", []byte(`{"role":""}`)))

	if !pollAccessUntil(3*time.Second, func() bool {
		return len(policy.PermissionsOf("editor")) == 1
	}) {
		t.Errorf("SECURITY: [access] empty-role signal did not converge drifted grants; editor still holds %v (stale in-memory grant survived an authoritative reload)", policy.PermissionsOf("editor"))
	}
}

// ---------------------------------------------------------------------------
// Property: teardown is quiet — stop is idempotent, and publish/handleRemote
// after stop (a Grant racing shutdown) are safe no-ops that neither panic
// nor resurrect the worker. The framework registers stop as an OnStop
// drainer, so a late Grant during shutdown takes exactly this path.
// ---------------------------------------------------------------------------

func TestSetFanoutStopThenLateActivityQuiet(t *testing.T) {
	db := openAccessDB(t)
	store, policy := seedStore(t, db, "editor", Permission("posts:read"))

	stop, err := store.SetFanout(fanout.NewInProcess())
	if err != nil {
		t.Fatalf("SetFanout: %v", err)
	}
	stop()
	stop() // idempotent

	before := policy.PermissionsOf("editor")
	quiet := func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SECURITY: [access] post-stop activity panicked: %v", r)
			}
		}()
		store.publish("editor")                                      // send == nil path
		store.handleRemote(fanout.Wrap("f", []byte(`{"role":"x"}`))) // dirty == nil path
		store.handleRemote(fanout.Wrap("f", []byte(`{"role":""}`)))  // full-reload signal post-stop
	}
	quiet()
	quiet()

	got := policy.PermissionsOf("editor")
	if len(got) != len(before) {
		t.Errorf("policy mutated after stop: %v -> %v", before, got)
	}
	// Post-stop signals may still land in the (now workerless) dirty set;
	// that is benign bookkeeping. What must hold — asserted above — is that
	// no reload ran (policy unchanged) and nothing panicked.
}

// ---------------------------------------------------------------------------
// Property: invalidation coalescing loses no ROLES — the wake channel is
// buffered(1), so N distinct roles invalidated while the worker is busy
// must all be reloaded (the dirty set, not the wake count, carries them).
// A lost role here is a replica running on stale grants indefinitely.
// ---------------------------------------------------------------------------

func TestRefreshCoalescingLosesNoRoles(t *testing.T) {
	db := openAccessDB(t)
	ctx := context.Background()

	storeA, _ := seedStore(t, db, "r0", Permission("seed:0"))
	storeB, policyB := seedStore(t, db, "r0", Permission("seed:0"))

	f := fanout.NewInProcess()
	stopA, err := storeA.SetFanout(f)
	if err != nil {
		t.Fatalf("SetFanout A: %v", err)
	}
	defer stopA()
	stopB, err := storeB.SetFanout(f)
	if err != nil {
		t.Fatalf("SetFanout B: %v", err)
	}
	defer stopB()

	roles := []string{"r1", "r2", "r3", "r4", "r5"}
	for _, r := range roles {
		if err := storeA.Grant(ctx, r, Permission("p")); err != nil {
			t.Fatalf("Grant %s: %v", r, err)
		}
	}

	ok := pollAccessUntil(3*time.Second, func() bool {
		for _, r := range roles {
			perms := policyB.PermissionsOf(r)
			if len(perms) == 0 {
				return false
			}
		}
		return true
	})
	if !ok {
		t.Errorf("SECURITY: [access] coalesced invalidations lost roles; replica B holds: %v", policyB.Snapshot())
	}
}
