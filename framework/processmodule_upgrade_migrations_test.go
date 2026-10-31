package framework

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// An artifact upgrade must not inherit the previous artifact's migration
// approval. InstallOrUpdate bumped artifact_sha256 and the generation and
// kept migrations_applied_at, so the new version's DDL never ran and the
// module reached Ready against the old schema (2026-10-27 bug bash M13).

// TestArtifactUpgradeClearsMigrationStamp: either InstallOrUpdate arm
// that writes a new artifact clears the stamp; a grants-only change, or
// none, keeps it. On SQLite and, when reachable, Postgres.
func TestArtifactUpgradeClearsMigrationStamp(t *testing.T) {
	t.Run("sqlite", func(t *testing.T) { checkUpgradeStamp(t, newTestStore) })
	t.Run("postgres", func(t *testing.T) {
		checkUpgradeStamp(t, func(t *testing.T) *SQLProcessModuleStore {
			store, err := NewSQLProcessModuleStore(pgtest.DB(t))
			if err != nil {
				t.Fatalf("new store: %v", err)
			}
			if err := store.EnsureSchema(context.Background()); err != nil {
				t.Fatalf("ensure schema: %v", err)
			}
			return store
		})
	})
}

func checkUpgradeStamp(t *testing.T, newStore func(*testing.T) *SQLProcessModuleStore) {
	ctx := context.Background()
	stamp := time.UnixMilli(1_700_000_000_000)
	read := []access.Permission{"articles:read"}
	write := []access.Permission{"articles:write"}
	for _, tc := range []struct {
		name   string
		sha    string
		grants []access.Permission
		kept   bool
	}{
		{"unchanged", hex256("aa"), read, true},
		{"grants only", hex256("aa"), write, true},
		{"artifact", hex256("bb"), read, false},
		{"artifact and grants", hex256("bb"), write, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newStore(t)
			if err := store.Install(ctx, DesiredState{Module: "demo", ArtifactSHA256: hex256("aa"), EffectiveGrants: read, MigrationsAppliedAt: &stamp}); err != nil {
				t.Fatalf("install: %v", err)
			}
			res, err := store.InstallOrUpdate(ctx, DesiredState{Module: "demo", ArtifactSHA256: tc.sha, EffectiveGrants: tc.grants})
			if err != nil {
				t.Fatalf("InstallOrUpdate: %v", err)
			}
			got, err := store.GetDesired(ctx, "demo")
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if kept := got.MigrationsAppliedAt != nil; kept != tc.kept {
				t.Errorf("stamp kept = %v, want %v (outcome %v)", kept, tc.kept, res.Outcome)
			}
			if kept := res.Row.MigrationsAppliedAt != nil; kept != tc.kept {
				t.Errorf("returned row stamp kept = %v, want %v", kept, tc.kept)
			}
		})
	}
}

// TestStampRefusesChangedArtifact: a coordinator approves the artifact
// its descriptor names. When the row has moved to another artifact (a
// replica booted a newer build while Apply ran) the stamp is refused.
func TestStampRefusesChangedArtifact(t *testing.T) {
	ctx := context.Background()
	store, desc := coordStoreWithModule(t, "demo", TrustTrusted)
	if _, err := store.InstallOrUpdate(ctx, DesiredState{Module: "demo", ArtifactSHA256: hex256("bb")}); err != nil {
		t.Fatalf("InstallOrUpdate: %v", err)
	}
	c, err := NewMigrationCoordinator(store, store.db)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	err = c.Apply(ctx, desc, nil)
	if err == nil || !strings.Contains(err.Error(), "artifact") {
		t.Fatalf("Apply for a replaced artifact = %v, want an artifact error", err)
	}
	got, err := store.GetDesired(ctx, "demo")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.MigrationsAppliedAt != nil {
		t.Errorf("the replaced artifact's Apply stamped the row: %v", got.MigrationsAppliedAt)
	}
}

// migratingChild is the echo child declaring a migration group.
func migratingChild(t *testing.T) ProcessModuleDescriptor {
	t.Helper()
	d := descriptorForChild(t, childModeEcho)
	d.MigrationGroup = "demo"
	surface, err := ComputeSurfaceSHA256(d)
	if err != nil {
		t.Fatalf("surface: %v", err)
	}
	d.SurfaceSHA256 = surface
	return d
}

// TestPendingMigrationsDrainReadyChild: when the stamp clears under a
// Ready module (another replica's upgrade), the supervisor stops the
// child. It only relabelled the slot InstalledDisabled and left the
// child serving.
func TestPendingMigrationsDrainReadyChild(t *testing.T) {
	if os.Getenv(childEnvName) != "" {
		return
	}
	ctx := context.Background()
	store := newTestStore(t)
	d := migratingChild(t)
	sup := newTestSupervisor(t, store, childModeEcho)
	if _, err := sup.Register(ctx, d, ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	stamp := time.Now()
	if err := store.SetMigrationsAppliedAt(ctx, d.Name, &stamp); err != nil {
		t.Fatalf("stamp: %v", err)
	}
	sup.StartLoops()
	if err := sup.Enable(ctx, d.Name); err != nil {
		t.Fatalf("enable: %v", err)
	}
	waitForState(t, sup, d.Name, StateReady, 8*time.Second)

	if err := store.SetMigrationsAppliedAt(ctx, d.Name, nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, err := store.BumpGeneration(ctx, d.Name); err != nil {
		t.Fatalf("bump: %v", err)
	}
	waitForState(t, sup, d.Name, StateInstalledDisabled, 8*time.Second)
	deadline := time.Now().Add(3 * time.Second)
	for redLeaseSlotPID(sup, d.Name) > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if pid := redLeaseSlotPID(sup, d.Name); pid > 0 {
		t.Fatalf("the child (pid %d) kept running with migrations pending", pid)
	}
}

// TestUpgradeDrainWaitsForMigrations: an upgrade drain that ends after
// the stamp cleared does not respawn the module.
func TestUpgradeDrainWaitsForMigrations(t *testing.T) {
	if os.Getenv(childEnvName) != "" {
		return
	}
	ctx := context.Background()
	store := newTestStore(t)
	d := migratingChild(t)
	sup := newTestSupervisor(t, store, childModeEcho)
	if _, err := sup.Register(ctx, d, ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := store.SetEnabled(ctx, d.Name, true); err != nil {
		t.Fatalf("enable row: %v", err)
	}
	desired, err := store.GetDesired(ctx, d.Name)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	sup.Slot(d.Name).finishDrain(StateDrainingUpgrade, desired.DesiredGeneration)
	if info, _ := sup.Info(d.Name); info.State != StateInstalledDisabled {
		t.Fatalf("upgrade drain with migrations pending left %s, want InstalledDisabled", info.State)
	}
}
