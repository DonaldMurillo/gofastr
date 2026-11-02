package access_test

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// A refresh signal is a best-effort frame: a stalled bus drops it, and the
// peer that missed it kept honouring a revoked permission until the next
// change to that role or a restart. The fanout worker now reloads every
// role on a timer, so a peer that missed the signal converges anyway.
// Replica A here has no fanout at all: its revoke tells nobody, which is
// every dropped frame at once.
func TestGrantStoreReconcileHealsMissedRevoke(t *testing.T) {
	db := newGrantDB(t)
	ctx := context.Background()

	storeA := access.NewGrantStore(db, access.NewRolePolicy())
	if err := storeA.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema A: %v", err)
	}
	if err := storeA.Grant(ctx, "editor", "posts:write"); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	policyB := access.NewRolePolicy()
	storeB := access.NewGrantStore(db, policyB, access.WithReconcileInterval(20*time.Millisecond))
	if err := storeB.LoadInto(ctx, nil); err != nil {
		t.Fatalf("LoadInto B: %v", err)
	}
	stopB, err := storeB.SetFanout(fanout.NewInProcess())
	if err != nil {
		t.Fatalf("SetFanout B: %v", err)
	}
	defer stopB()

	cB := ctxFor(policyB, "editor")
	if !access.Can(cB, "posts:write") {
		t.Fatal("B did not load the grant at boot")
	}
	if err := storeA.Revoke(ctx, "editor", "posts:write"); err != nil {
		t.Fatalf("Revoke on A: %v", err)
	}
	if !pollUntil(2*time.Second, func() bool { return !access.Can(cB, "posts:write") }) {
		t.Fatal("B still grants posts:write after a revoke it was never told about")
	}
}

// A zero interval turns the reconcile off: nothing reloads B but a signal.
func TestGrantStoreReconcileZeroDisables(t *testing.T) {
	db := newGrantDB(t)
	ctx := context.Background()

	storeA := access.NewGrantStore(db, access.NewRolePolicy())
	if err := storeA.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema A: %v", err)
	}
	if err := storeA.Grant(ctx, "editor", "posts:write"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	policyB := access.NewRolePolicy()
	storeB := access.NewGrantStore(db, policyB, access.WithReconcileInterval(0))
	if err := storeB.LoadInto(ctx, nil); err != nil {
		t.Fatalf("LoadInto B: %v", err)
	}
	stopB, err := storeB.SetFanout(fanout.NewInProcess())
	if err != nil {
		t.Fatalf("SetFanout B: %v", err)
	}
	defer stopB()
	if err := storeA.Revoke(ctx, "editor", "posts:write"); err != nil {
		t.Fatalf("Revoke on A: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if !access.Can(ctxFor(policyB, "editor"), "posts:write") {
		t.Fatal("a disabled reconcile reloaded B anyway")
	}
}
