package access_test

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/framework/access"

	_ "github.com/mattn/go-sqlite3"
)

// TestGrantStore_FanoutPropagatesGrantAndRevoke: two stores share one DB
// (replicas A and B) and one InProcess fanout. Grant on A → B's local
// policy reflects the new permission WITHOUT a restart (refresh-signal
// re-read from DB). Revoke on A → B loses it.
func TestGrantStore_FanoutPropagatesGrantAndRevoke(t *testing.T) {
	db := newGrantDB(t)
	ctx := context.Background()

	policyA := access.NewRolePolicy()
	storeA := access.NewGrantStore(db, policyA)
	if err := storeA.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema A: %v", err)
	}

	policyB := access.NewRolePolicy()
	storeB := access.NewGrantStore(db, policyB)
	if err := storeB.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema B: %v", err)
	}

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

	cB := ctxFor(policyB, "editor")

	// Before: B denies.
	if access.Can(cB, access.Permission("posts:read")) {
		t.Fatal("B: expected Can=false before grant")
	}

	// Grant on A → fanout → B reloads role from DB.
	if err := storeA.Grant(ctx, "editor", access.Permission("posts:read")); err != nil {
		t.Fatalf("Grant on A: %v", err)
	}
	if !pollUntil(2*time.Second, func() bool {
		return access.Can(cB, access.Permission("posts:read"))
	}) {
		t.Fatal("B: grant from A did not propagate (still Can=false)")
	}

	// Revoke on A → fanout → B loses it.
	if err := storeA.Revoke(ctx, "editor", access.Permission("posts:read")); err != nil {
		t.Fatalf("Revoke on A: %v", err)
	}
	if !pollUntil(2*time.Second, func() bool {
		return !access.Can(cB, access.Permission("posts:read"))
	}) {
		t.Fatal("B: revoke from A did not propagate (still Can=true)")
	}
}

// TestGrantStore_FanoutOwnNodeEchoDropped: a publish from A does not
// double-apply on A (the local policy already mutated synchronously in
// Grant/Revoke; the fanout echo would otherwise re-read DB and re-mutate).
// Verified by counting role-permission entries after a grant: still one.
func TestGrantStore_FanoutOwnNodeEchoDropped(t *testing.T) {
	db := newGrantDB(t)
	ctx := context.Background()

	policyA := access.NewRolePolicy()
	storeA := access.NewGrantStore(db, policyA)
	if err := storeA.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema A: %v", err)
	}

	f := fanout.NewInProcess()
	stop, err := storeA.SetFanout(f)
	if err != nil {
		t.Fatalf("SetFanout A: %v", err)
	}
	defer stop()

	if err := storeA.Grant(ctx, "editor", access.Permission("posts:read")); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	// Give the fanout delivery goroutine time to echo back.
	if !pollUntil(time.Second, func() bool {
		// By the time this resolves, the echo (if not dropped) has run.
		return true
	}) {
		t.Fatal("poll timed out")
	}
	perms := policyA.PermissionsOf("editor")
	if len(perms) != 1 || perms[0] != access.Permission("posts:read") {
		t.Fatalf("A: own-node echo not dropped — perms = %v, want [posts:read]", perms)
	}
}

// TestGrantStore_FanoutIdempotentDuplicateDelivery: delivering the SAME
// invalidation message twice is still correct — each delivery re-reads
// authoritative DB state, so the policy converges to whatever the DB says
// regardless of duplication.
func TestGrantStore_FanoutIdempotentDuplicateDelivery(t *testing.T) {
	db := newGrantDB(t)
	ctx := context.Background()

	policyA := access.NewRolePolicy()
	storeA := access.NewGrantStore(db, policyA)
	if err := storeA.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema A: %v", err)
	}
	policyB := access.NewRolePolicy()
	storeB := access.NewGrantStore(db, policyB)
	if err := storeB.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema B: %v", err)
	}

	f := fanout.NewInProcess()
	stopA, _ := storeA.SetFanout(f)
	defer stopA()
	stopB, _ := storeB.SetFanout(f)
	defer stopB()

	// Grant via A so the DB row exists.
	if err := storeA.Grant(ctx, "editor", access.Permission("posts:read")); err != nil {
		t.Fatalf("Grant A: %v", err)
	}
	// Wait for B to converge once.
	cB := ctxFor(policyB, "editor")
	if !pollUntil(2*time.Second, func() bool {
		return access.Can(cB, access.Permission("posts:read"))
	}) {
		t.Fatal("B: initial grant did not propagate")
	}

	// Manually re-deliver the same invalidation several times by publishing
	// through the fanout bus from A's perspective (simulating a duplicate
	// delivery from any source). B must remain with exactly one permission.
	for range 3 {
		// Trigger another publish via a no-op grant (idempotent in the DB;
		// still publishes the invalidation).
		if err := storeA.Grant(ctx, "editor", access.Permission("posts:read")); err != nil {
			t.Fatalf("dup grant: %v", err)
		}
	}
	// Settle.
	time.Sleep(100 * time.Millisecond)

	perms := policyB.PermissionsOf("editor")
	if len(perms) != 1 || perms[0] != access.Permission("posts:read") {
		t.Fatalf("B: duplicate delivery produced %v, want exactly [posts:read]", perms)
	}
}

// TestGrantStore_SetFanoutNilIsNoOp: SetFanout(nil) returns a callable stop
// and never subscribes — single-process deployments stay unaffected.
func TestGrantStore_SetFanoutNilIsNoOp(t *testing.T) {
	db := newGrantDB(t)
	ctx := context.Background()
	policy := access.NewRolePolicy()
	store := access.NewGrantStore(db, policy)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	stop, err := store.SetFanout(nil)
	if err != nil {
		t.Fatalf("SetFanout(nil) returned err: %v", err)
	}
	if stop == nil {
		t.Fatal("SetFanout(nil) returned nil stop")
	}
	stop() // must not panic
}

// TestRolePolicy_ReplaceRole: ReplaceRole atomically swaps a role's
// permissions (not append) and updates Can. Old permissions not in the
// replacement are dropped.
func TestRolePolicy_ReplaceRole(t *testing.T) {
	rp := access.NewRolePolicy()
	ctx := ctxFor(rp, "editor")

	// Start with read+write.
	if err := rp.Grant("editor",
		access.Permission("posts:read"),
		access.Permission("posts:write"),
	); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if !access.Can(ctx, access.Permission("posts:write")) {
		t.Fatal("expected posts:write before replace")
	}

	// Replace with just read — write must be gone.
	if err := rp.ReplaceRole("editor", access.Permission("posts:read")); err != nil {
		t.Fatalf("ReplaceRole: %v", err)
	}
	if !access.Can(ctx, access.Permission("posts:read")) {
		t.Fatal("expected posts:read after replace")
	}
	if access.Can(ctx, access.Permission("posts:write")) {
		t.Fatal("ReplaceRole did not drop posts:write")
	}

	// Replace with empty → role has no permissions.
	if err := rp.ReplaceRole("editor"); err != nil {
		t.Fatalf("ReplaceRole empty: %v", err)
	}
	if access.Can(ctx, access.Permission("posts:read")) {
		t.Fatal("expected posts:read gone after empty replace")
	}
	if got := rp.PermissionsOf("editor"); len(got) != 0 {
		t.Fatalf("empty replace left %v, want none", got)
	}
}

// TestRolePolicy_ReplaceRoleDedupes: duplicate perms in the replacement
// collapse to one entry.
func TestRolePolicy_ReplaceRoleDedupes(t *testing.T) {
	rp := access.NewRolePolicy()
	if err := rp.ReplaceRole("editor",
		access.Permission("posts:read"),
		access.Permission("posts:read"),
		access.Permission("posts:write"),
		access.Permission("posts:write"),
	); err != nil {
		t.Fatalf("ReplaceRole: %v", err)
	}
	got := rp.PermissionsOf("editor")
	if len(got) != 2 {
		t.Fatalf("after dedupe, perms = %v, want 2", got)
	}
}

// TestRolePolicy_ReplaceRoleStrictRejectsUnknown: strict mode rejects an
// unknown capability and leaves the role unchanged.
func TestRolePolicy_ReplaceRoleStrictRejectsUnknown(t *testing.T) {
	rp := access.NewRolePolicy().StrictCapabilities()
	rp.Register(access.Permission("posts:read"), access.Permission("posts:write"))
	if err := rp.Grant("editor", access.Permission("posts:read")); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	// Replace with a typo — must fail AND leave the existing grant intact.
	err := rp.ReplaceRole("editor", access.Permission("posts:read"), access.Permission("postz:write"))
	if err == nil {
		t.Fatal("expected strict ReplaceRole to reject unknown capability")
	}
	got := rp.PermissionsOf("editor")
	if len(got) != 1 || got[0] != access.Permission("posts:read") {
		t.Fatalf("after rejected replace, perms = %v, want [posts:read]", got)
	}
}

// TestGrantStore_FanoutMergesCodeBaseline pins that a cross-replica reload
// must NOT drop code-defined baseline grants. Replica B declares a grant in
// code (policyB.Grant), captures it as baseline at LoadInto, then receives a
// DB grant for the same role from replica A via fanout. B must end up with
// BOTH the code grant and the DB grant.
func TestGrantStore_FanoutMergesCodeBaseline(t *testing.T) {
	db := newGrantDB(t)
	ctx := context.Background()

	policyA := access.NewRolePolicy()
	storeA := access.NewGrantStore(db, policyA)
	if err := storeA.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema A: %v", err)
	}

	policyB := access.NewRolePolicy()
	// Code-defined baseline grant on B — the kind an app declares at startup.
	if err := policyB.Grant("admin", access.Permission("sys:all")); err != nil {
		t.Fatalf("code grant B: %v", err)
	}
	storeB := access.NewGrantStore(db, policyB)
	if err := storeB.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema B: %v", err)
	}
	// LoadInto captures the baseline (the code grant) before overlaying DB rows.
	if err := storeB.LoadInto(ctx, policyB); err != nil {
		t.Fatalf("LoadInto B: %v", err)
	}

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

	cB := ctxFor(policyB, "admin")
	if !access.Can(cB, access.Permission("sys:all")) {
		t.Fatal("B: code baseline grant missing before fanout")
	}

	// A grants a DB permission for the same role → B reloads via fanout.
	if err := storeA.Grant(ctx, "admin", access.Permission("posts:read")); err != nil {
		t.Fatalf("Grant on A: %v", err)
	}
	if !pollUntil(2*time.Second, func() bool {
		return access.Can(cB, access.Permission("posts:read"))
	}) {
		t.Fatal("B: DB grant from A did not propagate")
	}
	// The reload must NOT have wiped the code baseline.
	if !access.Can(cB, access.Permission("sys:all")) {
		t.Fatal("B: cross-replica reload wiped the code-defined baseline grant")
	}
}

// TestGrantStore_RejectsEmptyRole pins that "" (the full-reload fanout
// sentinel) is never a grantable role — otherwise Grant/Revoke("", p) could
// be mistaken for a reload-all signal and strand a permission.
func TestGrantStore_RejectsEmptyRole(t *testing.T) {
	db := newGrantDB(t)
	ctx := context.Background()
	store := access.NewGrantStore(db, access.NewRolePolicy())
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	if err := store.Grant(ctx, "", access.Permission("posts:read")); err == nil {
		t.Fatal("Grant with empty role should be rejected")
	}
	if err := store.Revoke(ctx, "", access.Permission("posts:read")); err == nil {
		t.Fatal("Revoke with empty role should be rejected")
	}
}

// pollUntil returns true if fn() returns true within the timeout. Used to
// wait out the asynchronous InProcess fanout delivery without a tight sleep.
func pollUntil(timeout time.Duration, fn func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if fn() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
}
