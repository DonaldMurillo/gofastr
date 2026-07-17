package framework

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// These tests pin the §7 migration coordinator: the pure Plan validation
// (group rules, digest, lint) runs on SQLite with no Docker; the real
// Postgres isolation boundary (module_<M> schema+role, public denied,
// SET ROLE escalation refused) runs against a testcontainer via pgtest
// and skips when Postgres is unreachable.

// coordStoreWithModule builds a SQLite store + a desired-state row for
// "demo" (group "demo") so Plan/Apply/stampApplied have a row to act on.
func coordStoreWithModule(t *testing.T, group string, tier TrustTier) (*SQLProcessModuleStore, ProcessModuleDescriptor) {
	t.Helper()
	store := newTestStore(t)
	desc := ProcessModuleDescriptor{
		Name:           "demo",
		Version:        "1.0.0",
		ArtifactSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		SurfaceSHA256:  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		TrustTier:      tier,
	}
	if group != "" {
		desc.MigrationGroup = group
	}
	if err := store.Install(context.Background(), DesiredState{
		Module:            "demo",
		DesiredGeneration: 1,
		Enabled:           false,
		ArtifactSHA256:    desc.ArtifactSHA256,
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	return store, desc
}

// ---- Plan: pure validation (SQLite, no Docker) ----

func TestCoord_PlanRejectsDefaultGroup(t *testing.T) {
	store, desc := coordStoreWithModule(t, "", TrustTrusted) // empty group
	c, err := NewMigrationCoordinator(store, store.db)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	_, err = c.Plan(desc, []ApprovedMigration{{Version: 1, Name: "a", Up: "CREATE TABLE x (id int)"}})
	if err == nil {
		t.Fatal("Plan must reject an empty (default) migration group")
	}
	var ve *CoordinatorValidationError
	if !errors.As(err, &ve) || ve.Rule != "empty" {
		t.Fatalf("want empty-group validation error, got %T %v", err, err)
	}
}

func TestCoord_PlanRejectsDuplicate(t *testing.T) {
	store, desc := coordStoreWithModule(t, "demo", TrustTrusted)
	c, _ := NewMigrationCoordinator(store, store.db)
	_, err := c.Plan(desc, []ApprovedMigration{
		{Version: 1, Name: "a", Up: "CREATE TABLE a (id int)"},
		{Version: 1, Name: "b", Up: "CREATE TABLE b (id int)"},
	})
	var ve *CoordinatorValidationError
	if !errors.As(err, &ve) || ve.Rule != "duplicate" {
		t.Fatalf("want duplicate-version error, got %v", err)
	}
}

func TestCoord_PlanRejectsDigestMismatch(t *testing.T) {
	store, desc := coordStoreWithModule(t, "demo", TrustTrusted)
	c, _ := NewMigrationCoordinator(store, store.db)
	_, err := c.Plan(desc, []ApprovedMigration{
		{Version: 1, Name: "a", Up: "CREATE TABLE a (id int)", SHA256: "deadbeef"},
	})
	var ve *CoordinatorValidationError
	if !errors.As(err, &ve) || ve.Rule != "mismatch" {
		t.Fatalf("want digest mismatch error, got %v", err)
	}
}

func TestCoord_PlanAcceptsMatchingDigest(t *testing.T) {
	store, desc := coordStoreWithModule(t, "demo", TrustTrusted)
	c, _ := NewMigrationCoordinator(store, store.db)
	up := "CREATE TABLE a (id int)"
	digest := sha256Hex([]byte(up))
	plan, err := c.Plan(desc, []ApprovedMigration{{Version: 1, Name: "a", Up: up, SHA256: digest}})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Group != "demo" || len(plan.Steps) != 1 || plan.Steps[0].Computed != digest {
		t.Fatalf("plan = %+v", plan)
	}
}

// ---- Lint (non-authoritative) ----

func TestCoord_LintFlagsNonPlainDDL(t *testing.T) {
	for _, up := range []string{
		"DROP TABLE x",
		"CREATE TABLE y AS SELECT * FROM x",
		"DELETE FROM x",
		"GRANT ALL ON x TO public",
	} {
		if h := lintMigrationSQL(up); len(h) == 0 {
			t.Errorf("lint(%q) = no hints; want a non-plain-DDL flag", up)
		}
	}
}

func TestCoord_LintPassesPlainDDL(t *testing.T) {
	for _, up := range []string{
		"CREATE TABLE x (id int)",
		"ALTER TABLE x ADD COLUMN name text",
		"CREATE INDEX idx_x ON x (id)",
		"-- comment only\nCREATE TABLE y (id int)",
	} {
		if h := lintMigrationSQL(up); len(h) != 0 {
			t.Errorf("lint(%q) = %v; want no hints (plain DDL)", up, h)
		}
	}
}

func TestCoord_PlanCarriesLintWarnings(t *testing.T) {
	store, desc := coordStoreWithModule(t, "demo", TrustTrusted)
	c, _ := NewMigrationCoordinator(store, store.db)
	plan, err := c.Plan(desc, []ApprovedMigration{
		{Version: 1, Name: "drop", Up: "DROP TABLE x"},
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Warnings) == 0 {
		t.Fatal("expected lint warning for DROP TABLE")
	}
}

// ---- SQLite apply paths ----

func TestCoord_SQLiteUntrustedFailsClosed(t *testing.T) {
	store, desc := coordStoreWithModule(t, "demo", TrustUntrusted)
	c, _ := NewMigrationCoordinator(store, store.db)
	err := c.Apply(context.Background(), desc, []ApprovedMigration{
		{Version: 1, Name: "a", Up: "CREATE TABLE a (id int)"},
	})
	if err == nil {
		t.Fatal("untrusted module on SQLite must fail closed")
	}
	if !strings.Contains(err.Error(), "SQLite") && !strings.Contains(err.Error(), "sqlite") {
		t.Fatalf("want sqlite-fail-closed error, got %v", err)
	}
}

func TestCoord_SQLiteTrustedApplies(t *testing.T) {
	store, desc := coordStoreWithModule(t, "demo", TrustTrusted)
	stamp := time.Now()
	c, _ := NewMigrationCoordinator(store, store.db, WithCoordinatorClock(func() time.Time { return stamp }))
	err := c.Apply(context.Background(), desc, []ApprovedMigration{
		{Version: 1, Name: "a", Up: "CREATE TABLE module_demo_a (id int)"},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	// Table created.
	var n int
	if err := store.db.QueryRow("SELECT count(*) FROM module_demo_a").Scan(&n); err != nil {
		t.Fatalf("query created table: %v", err)
	}
	// MigrationsAppliedAt stamped + generation advanced.
	d, err := store.GetDesired(context.Background(), "demo")
	if err != nil {
		t.Fatalf("get desired: %v", err)
	}
	if d.MigrationsAppliedAt == nil {
		t.Errorf("MigrationsAppliedAt not stamped")
	}
	// Stored as epoch millis: compare at second granularity (sub-ms +
	// monotonic clock are lost on the round-trip).
	if d.MigrationsAppliedAt != nil && d.MigrationsAppliedAt.Unix() != stamp.Unix() {
		t.Errorf("MigrationsAppliedAt unix = %d, want %d", d.MigrationsAppliedAt.Unix(), stamp.Unix())
	}
	if d.DesiredGeneration != 2 {
		t.Errorf("generation = %d, want 2 (bumped after apply)", d.DesiredGeneration)
	}
}

// ---- Postgres isolation (testcontainer; skips if PG unreachable) ----

// pgCoordEnv opens an admin pool + admin DSN for the shared Postgres.
func pgCoordEnv(t *testing.T) (adminDB *sql.DB, adminDSN string) {
	t.Helper()
	adminDSN = pgtest.BaseDSN(t)
	db, err := sql.Open("postgres", adminDSN)
	if err != nil {
		t.Fatalf("open admin: %v", err)
	}
	db.SetMaxOpenConns(2)
	if err := db.PingContext(context.Background()); err == nil {
		// ok
	} else {
		// Cold-start Postgres resets the first connection (pgtest.ping
		// retries for the same reason); mirror that so a just-started
		// container does not fail the admin ping.
		var perr error
		for i := 0; i < 30; i++ {
			pctx, pcancel := context.WithTimeout(context.Background(), 2*time.Second)
			perr = db.PingContext(pctx)
			pcancel()
			if perr == nil {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		if perr != nil {
			db.Close()
			t.Fatalf("ping admin: %v", perr)
		}
	}
	t.Cleanup(func() { db.Close() })
	return db, adminDSN
}

// pgCoordStore builds a schema-scoped store + a desired row for "demo".
func pgCoordStore(t *testing.T) *SQLProcessModuleStore {
	t.Helper()
	db := pgtest.DB(t)
	store, err := NewSQLProcessModuleStore(db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	if err := store.Install(context.Background(), DesiredState{
		Module:            "demo",
		DesiredGeneration: 1,
		Enabled:           false,
		ArtifactSHA256:    "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	return store
}

func TestCoord_PG_ModuleSchemaIsolation(t *testing.T) {
	adminDB, adminDSN := pgCoordEnv(t)
	store := pgCoordStore(t)
	desc := ProcessModuleDescriptor{
		Name:           "demo",
		Version:        "1.0.0",
		ArtifactSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		SurfaceSHA256:  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		TrustTier:      TrustTrusted,
		MigrationGroup: "demo",
	}
	c, err := NewMigrationCoordinator(store, adminDB, WithCoordinatorAdminDSN(adminDSN))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	err = c.Apply(context.Background(), desc, []ApprovedMigration{
		{Version: 1, Name: "create", Up: "CREATE TABLE widgets (id int)"},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	// The table landed in module_demo (the role's search_path), NOT public.
	schema, _ := moduleSchemaRole("demo")
	if !pgTableExists(t, adminDB, schema, "widgets") {
		t.Errorf("widgets not created in schema %s", schema)
	}
	if pgTableExists(t, adminDB, "public", "widgets") {
		t.Error("widgets must NOT exist in public (role has no CREATE there)")
	}
	// Stamp landed on the store.
	d, _ := store.GetDesired(context.Background(), "demo")
	if d.MigrationsAppliedAt == nil {
		t.Error("MigrationsAppliedAt not stamped")
	}
}

func TestCoord_PG_HostilePublicDenied(t *testing.T) {
	adminDB, adminDSN := pgCoordEnv(t)
	store := pgCoordStore(t)
	desc := ProcessModuleDescriptor{
		Name:           "demo",
		Version:        "1.0.0",
		ArtifactSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		SurfaceSHA256:  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		TrustTier:      TrustTrusted,
		MigrationGroup: "demo",
	}
	c, _ := NewMigrationCoordinator(store, adminDB, WithCoordinatorAdminDSN(adminDSN))
	err := c.Apply(context.Background(), desc, []ApprovedMigration{
		{Version: 1, Name: "evil", Up: "CREATE TABLE public.evil (id int)"},
	})
	if err == nil {
		t.Fatal("hostile migration writing to public must be denied by the role")
	}
	// The fence held: no evil table anywhere.
	if pgTableExists(t, adminDB, "public", "evil") {
		t.Error("public.evil was created — role fence failed")
	}
}

func TestCoord_PG_SetRoleEscalationFails(t *testing.T) {
	adminDB, adminDSN := pgCoordEnv(t)
	store := pgCoordStore(t)
	desc := ProcessModuleDescriptor{
		Name:           "demo",
		Version:        "1.0.0",
		ArtifactSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		SurfaceSHA256:  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		TrustTier:      TrustTrusted,
		MigrationGroup: "demo",
	}
	c, _ := NewMigrationCoordinator(store, adminDB, WithCoordinatorAdminDSN(adminDSN))
	// SET ROLE postgres from the module role must fail (not a member) — the
	// authenticate-as property (design §7 point 2). RESET ROLE is a no-op.
	err := c.Apply(context.Background(), desc, []ApprovedMigration{
		{Version: 1, Name: "escalate", Up: "SET ROLE postgres; CREATE TABLE public.evil2 (id int)"},
	})
	if err == nil {
		t.Fatal("SET ROLE escalation must be denied")
	}
	if pgTableExists(t, adminDB, "public", "evil2") {
		t.Error("public.evil2 was created — SET ROLE escalation succeeded")
	}
}

// pgTableExists checks information_schema for a (schema, table) pair.
func pgTableExists(t *testing.T, db *sql.DB, schema, table string) bool {
	t.Helper()
	var n int
	err := db.QueryRow(
		`SELECT count(*) FROM information_schema.tables WHERE table_schema = $1 AND table_name = $2`,
		schema, table,
	).Scan(&n)
	if err != nil {
		t.Fatalf("information_schema query for %s.%s: %v", schema, table, err)
	}
	return n > 0
}
