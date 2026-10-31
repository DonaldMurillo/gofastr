package framework

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/access"
)

// Another replica that boots a newer artifact moves the row's digest and
// bumps the generation. This replica's descriptor still names the old
// executable, so it must not respawn it under the new generation's
// grants: it drains the child and holds until its own deploy registers
// the matching artifact (2026-10-27 bug bash P32).

// TestOtherArtifactDrainsReadyChild: the upgrade drain does not respawn
// the old binary.
func TestOtherArtifactDrainsReadyChild(t *testing.T) {
	if os.Getenv(childEnvName) != "" {
		return
	}
	ctx := context.Background()
	store := newTestStore(t)
	d := descriptorForChild(t, childModeEcho)
	sup := newTestSupervisor(t, store, childModeEcho)
	if _, err := sup.Register(ctx, d, ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	sup.StartLoops()
	if err := sup.Enable(ctx, d.Name); err != nil {
		t.Fatalf("enable: %v", err)
	}
	waitForState(t, sup, d.Name, StateReady, 8*time.Second)

	if _, err := store.InstallOrUpdate(ctx, DesiredState{Module: d.Name, ArtifactSHA256: hex256("bb"), EffectiveGrants: []access.Permission{"articles:read"}}); err != nil {
		t.Fatalf("InstallOrUpdate: %v", err)
	}
	waitForState(t, sup, d.Name, StateInstalledDisabled, 8*time.Second)
	// Several polls: the hold must stay, not flap back to a spawn.
	time.Sleep(300 * time.Millisecond)
	if info, _ := sup.Info(d.Name); info.State != StateInstalledDisabled {
		t.Fatalf("state = %s after the row moved to another artifact, want InstalledDisabled", info.State)
	}
	if pid := redLeaseSlotPID(sup, d.Name); pid > 0 {
		t.Fatalf("the old binary (pid %d) runs under the other artifact's generation", pid)
	}
}

// TestOtherArtifactRule: an empty row digest names nothing, and hex case
// is not a different artifact.
func TestOtherArtifactRule(t *testing.T) {
	desc := ProcessModuleDescriptor{ArtifactSHA256: hex256("aa")}
	for _, tc := range []struct {
		row  string
		want bool
	}{
		{"", false},
		{hex256("aa"), false},
		{strings.ToUpper(hex256("aa")), false},
		{hex256("bb"), true},
	} {
		if got := otherArtifact(desc, DesiredState{ArtifactSHA256: tc.row}); got != tc.want {
			t.Errorf("otherArtifact(row %q) = %v, want %v", tc.row, got, tc.want)
		}
	}
}

// TestUpgradeDrainRefusesOtherArtifact: an upgrade drain that ends after
// the row moved to another artifact holds instead of respawning.
func TestUpgradeDrainRefusesOtherArtifact(t *testing.T) {
	if os.Getenv(childEnvName) != "" {
		return
	}
	ctx := context.Background()
	store := newTestStore(t)
	d := descriptorForChild(t, childModeEcho)
	sup := newTestSupervisor(t, store, childModeEcho)
	if _, err := sup.Register(ctx, d, ApprovedGrants{"articles:read"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := store.SetEnabled(ctx, d.Name, true); err != nil {
		t.Fatalf("enable row: %v", err)
	}
	if _, err := store.InstallOrUpdate(ctx, DesiredState{Module: d.Name, ArtifactSHA256: hex256("bb"), EffectiveGrants: []access.Permission{"articles:read"}}); err != nil {
		t.Fatalf("InstallOrUpdate: %v", err)
	}
	desired, err := store.GetDesired(ctx, d.Name)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	sup.Slot(d.Name).finishDrain(StateDrainingUpgrade, desired.DesiredGeneration)
	if info, _ := sup.Info(d.Name); info.State != StateInstalledDisabled {
		t.Fatalf("upgrade drain onto another artifact left %s, want InstalledDisabled", info.State)
	}
}
