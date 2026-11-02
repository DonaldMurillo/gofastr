package framework

import (
	"context"
	"database/sql"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// An operator turns a module off in gofastr_modules, and a process module's
// desired state (on/off, its approved artifact, its grants and the grants
// an operator took away) lives in gofastr_process_modules. ExportData left
// both tables out, so a restore booted every module the operator had turned
// off back on, and a process module's revoked grants came back with it.
func TestExportRestoresModuleState(t *testing.T) {
	ctx := context.Background()
	build := func(t *testing.T, db *sql.DB) *App {
		app := newBlogApp(t, db) // NewApp(WithDB) creates gofastr_modules
		ps, err := NewSQLProcessModuleStore(db)
		if err != nil {
			t.Fatalf("NewSQLProcessModuleStore: %v", err)
		}
		if err := ps.EnsureSchema(ctx); err != nil {
			t.Fatalf("EnsureSchema: %v", err)
		}
		return app
	}
	seed := func(t *testing.T, db *sql.DB) {
		ms, err := NewSQLModuleStore(db)
		if err != nil {
			t.Fatalf("NewSQLModuleStore: %v", err)
		}
		if err := ms.SetEnabled(ctx, "billing", false); err != nil {
			t.Fatalf("SetEnabled: %v", err)
		}
		ps, err := NewSQLProcessModuleStore(db)
		if err != nil {
			t.Fatalf("NewSQLProcessModuleStore: %v", err)
		}
		if err := ps.Install(ctx, DesiredState{
			Module: "reports", DesiredGeneration: 4, Enabled: false,
			ArtifactSHA256:  "abc123",
			EffectiveGrants: []access.Permission{"posts:read"},
			RevokedGrants:   []access.Permission{"posts:delete"},
		}); err != nil {
			t.Fatalf("Install: %v", err)
		}
	}
	forEachRestore(t, build, seed, func(t *testing.T, dst *sql.DB, _ migrate.Dialect) {
		ms, err := NewSQLModuleStore(dst)
		if err != nil {
			t.Fatalf("NewSQLModuleStore: %v", err)
		}
		state, err := ms.Load(ctx)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if on, ok := state["billing"]; !ok || on {
			t.Errorf("after the restore billing is %v (row present %v), want the operator's off", on, ok)
		}
		ps, err := NewSQLProcessModuleStore(dst)
		if err != nil {
			t.Fatalf("NewSQLProcessModuleStore: %v", err)
		}
		d, err := ps.GetDesired(ctx, "reports")
		if err != nil {
			t.Fatalf("the restore lost the process module's desired state: %v", err)
		}
		if d.Enabled || d.DesiredGeneration != 4 || d.ArtifactSHA256 != "abc123" ||
			!slices.Equal(d.EffectiveGrants, []access.Permission{"posts:read"}) ||
			!slices.Equal(d.RevokedGrants, []access.Permission{"posts:delete"}) {
			t.Errorf("restored desired state = %+v, want the exported row", d)
		}
	})
}

// withoutModuleState drops the module-state tables every app with a
// database now exports, for a test that counts its own sources.
func withoutModuleState(es []manifestEntry) []manifestEntry {
	return slices.DeleteFunc(slices.Clone(es), func(e manifestEntry) bool {
		return e.Source == "modules" || e.Source == "processmodules"
	})
}
