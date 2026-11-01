package framework

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// On Postgres a boot's InstallOrUpdate locks the row it reconciles, so a
// revoke that commits while the boot is between its read and its write
// is read, not overwritten. Without the lock the boot read the row
// before the revoke and wrote the descriptor's grants back after it,
// and the revoke was lost (#614). Another transaction holds the row
// lock, the boot blocks on it, and the revoke commits while it waits.
// The boot's descriptor adds a grant, so it writes the grant set.
func TestInstallWaitsForRevoke(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("postgres", pgtest.FreshDatabaseDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := NewSQLProcessModuleStore(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	readWrite := []access.Permission{"articles:read", "articles:create"}
	boot := DesiredState{Module: "m", Enabled: true, ArtifactSHA256: strings.Repeat("ab", 32), EffectiveGrants: readWrite}
	if _, err := store.InstallOrUpdate(ctx, boot); err != nil {
		t.Fatal(err)
	}

	holder, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback() //nolint:errcheck
	if _, err := holder.ExecContext(ctx, `SELECT module FROM gofastr_process_modules WHERE module = $1 FOR UPDATE`, "m"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	widened := boot
	widened.EffectiveGrants = append(slices.Clone(readWrite), "articles:update")
	go func() {
		_, err := store.InstallOrUpdate(ctx, widened)
		done <- err
	}()
	// The boot is parked on the row lock: at its read with the lock, at
	// its write without it.
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		var waiting int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the boot never waited on the row lock")
		}
	}
	// The revoke SetEffectiveGrants writes, inside the lock holder.
	if _, err := holder.ExecContext(ctx, `UPDATE gofastr_process_modules SET effective_grants = $1, revoked_grants = $2, desired_generation = desired_generation + 1 WHERE module = $3`,
		`["articles:read"]`, `["articles:create"]`, "m"); err != nil {
		t.Fatal(err)
	}
	if err := holder.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("boot: %v", err)
	}
	row, err := store.GetDesired(ctx, "m")
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(row.EffectiveGrants, "articles:create") {
		t.Errorf("SECURITY: the boot wrote back the revoked grant: effective = %v, revoked = %v", row.EffectiveGrants, row.RevokedGrants)
	}
}
