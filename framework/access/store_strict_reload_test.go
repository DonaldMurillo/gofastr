package access_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// A strict replica refused a whole role reload when the role's DB rows held
// a capability its own registry does not know: the normal state of a rolling
// deploy, where the newer replica registers a capability and grants it before
// the older one restarts. The reload failed on every retry, so the role froze
// on the strict replica, and a revoke issued afterwards never reached it: the
// replica kept honoring a permission every other replica had dropped.
//
// A reload is a refresh, not an admin action: a capability this replica
// cannot check is dropped from its view of the role (it never matched here
// anyway), and the rest of the role converges.
func TestGrantStoreStrictReplicaAppliesRevokeAcrossUnknownCapability(t *testing.T) {
	for name, open := range map[string]func(*testing.T) *sql.DB{
		"sqlite":   newGrantDB,
		"postgres": pgtest.DB,
	} {
		t.Run(name, func(t *testing.T) {
			db := open(t)
			ctx := context.Background()

			// A: the newer build, which knows reports:export.
			policyA := access.NewRolePolicy().StrictCapabilities()
			policyA.Register("posts:read", "posts:write", "reports:export")
			storeA := access.NewGrantStore(db, policyA)
			if err := storeA.EnsureSchema(ctx); err != nil {
				t.Fatalf("EnsureSchema: %v", err)
			}
			if err := storeA.LoadInto(ctx, policyA); err != nil {
				t.Fatalf("LoadInto A: %v", err)
			}
			// B: the older, strict build.
			policyB := access.NewRolePolicy().StrictCapabilities()
			policyB.Register("posts:read", "posts:write")
			storeB := access.NewGrantStore(db, policyB)
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

			cB := ctxFor(policyB, "editor")
			if err := storeA.Grant(ctx, "editor", "posts:read", "posts:write"); err != nil {
				t.Fatalf("Grant: %v", err)
			}
			if !pollUntil(2*time.Second, func() bool { return access.Can(cB, "posts:write") }) {
				t.Fatal("B: grant did not propagate")
			}
			if err := storeA.Grant(ctx, "editor", "reports:export"); err != nil {
				t.Fatalf("Grant reports:export: %v", err)
			}
			if err := storeA.Revoke(ctx, "editor", "posts:write"); err != nil {
				t.Fatalf("Revoke: %v", err)
			}
			if !pollUntil(2*time.Second, func() bool { return !access.Can(cB, "posts:write") }) {
				t.Fatal("B: still grants posts:write after A revoked it: the strict reload refused the role over reports:export")
			}
			if !access.Can(cB, "posts:read") {
				t.Fatal("B: lost posts:read, which nobody revoked")
			}
			if access.Can(cB, "reports:export") {
				t.Fatal("B: holds reports:export, a capability its strict registry refuses")
			}
		})
	}
}
