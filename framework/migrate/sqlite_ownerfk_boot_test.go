package migrate

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// usersEntity satisfies the registry check that resolves taskEntity's
// BelongsTo("users") — boot topo-sorts relations, and an unknown target is a
// boot error before the scan ever runs.
func usersEntity(t *testing.T) *entity.Entity {
	t.Helper()
	return entity.Define("users", entity.EntityConfig{
		Table:  "users",
		Fields: []schema.Field{{Name: "id", Type: schema.String}, {Name: "name", Type: schema.String}},
	}.WithTimestamps(false))
}

// Boot must surface the stale owner-column key as a warning naming the table
// and the fix, not as a boot failure: an app that creates no rows is
// otherwise fine, and failing its boot on upgrade day would be a regression.
func TestBootWarnsOnStaleOwnerFK(t *testing.T) {
	db := legacyDB(t,
		`CREATE TABLE users (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE tasks (
			id      TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			title   TEXT,
			FOREIGN KEY (user_id) REFERENCES users(id)
		)`,
	)
	reg := testReg{"tasks": taskEntity(t), "users": usersEntity(t)}
	buf := captureMigrateSlog(t)

	if err := AutoMigratePlanContext(context.Background(), db, Plan{Registry: reg}); err != nil {
		t.Fatalf("boot failed on an affected database: %v", err)
	}
	logs := buf.String()
	if !strings.Contains(logs, "tasks") {
		t.Errorf("the warning does not name the affected table:\n%s", logs)
	}
	if !strings.Contains(logs, "migrate repair") {
		t.Errorf("the warning does not say how to fix it:\n%s", logs)
	}
}

// A clean database boots with no owner-key warning — the scan is a signal
// about a broken table, not per-boot noise.
func TestBootCleanDBNoOwnerWarning(t *testing.T) {
	db := legacyDB(t,
		`CREATE TABLE users (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE tasks (
			id      TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			title   TEXT
		)`,
	)
	reg := testReg{"tasks": taskEntity(t), "users": usersEntity(t)}
	buf := captureMigrateSlog(t)

	if err := AutoMigratePlanContext(context.Background(), db, Plan{Registry: reg}); err != nil {
		t.Fatalf("boot failed on a clean database: %v", err)
	}
	if logs := buf.String(); strings.Contains(logs, "owner column") {
		t.Errorf("clean database produced an owner-key warning:\n%s", logs)
	}
}

// A scan error after a successful migration must not fail boot — the schema
// is already converged, and a boot failure would trade a warning for a dead
// app. The entry below is one AutoMigrate skips (field-less entities get no
// DDL) but the scan still tries to read, with a table name SafeIdent
// refuses, so the scan fails after migration succeeded.
func TestBootScanErrorIsNotFatal(t *testing.T) {
	db := legacyDB(t, `CREATE TABLE users (id TEXT PRIMARY KEY)`)
	bad := &entity.Entity{Config: entity.EntityConfig{
		Name:  "bad",
		Table: "tasks x",
		Scope: &entity.ScopeConfig{OwnerField: "user_id"},
	}}
	buf := captureMigrateSlog(t)

	if err := AutoMigratePlanContext(context.Background(), db, Plan{Registry: testReg{"bad": bad}}); err != nil {
		t.Fatalf("a scan error failed boot: %v", err)
	}
	if !strings.Contains(buf.String(), "could not scan") {
		t.Errorf("the scan error was swallowed silently:\n%s", buf.String())
	}
}
