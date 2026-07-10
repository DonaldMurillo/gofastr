package migrate

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// mustReg registers a migration, failing the test on error. Needed because
// Register now returns an error (duplicate/invalid-group rejection).
func mustReg(t *testing.T, m *Migrator, mig Migration) {
	t.Helper()
	if err := m.Register(mig); err != nil {
		t.Fatalf("Register %d/%q: %v", mig.Version, mig.Group, err)
	}
}

// seqOrder reads the `who` column of the seq table in insertion order — the
// observable proof of the (version, group) apply sequence.
func seqOrder(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query("SELECT who FROM seq ORDER BY id")
	if err != nil {
		t.Fatalf("read seq: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var w string
		if err := rows.Scan(&w); err != nil {
			t.Fatalf("scan seq: %v", err)
		}
		out = append(out, w)
	}
	return out
}

// pkColumns returns the tracking-table PK columns via SQLite pragma.
func pkColumns(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query("SELECT name FROM pragma_table_info('_migrations') WHERE pk > 0 ORDER BY pk")
	if err != nil {
		t.Fatalf("pragma_table_info: %v", err)
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("scan: %v", err)
		}
		cols = append(cols, c)
	}
	return cols
}

// ---- parse ----

func TestParseGroupDirective(t *testing.T) {
	m, _ := newSQLiteMigrator(t)
	content := `-- +migrate Version 1
-- +migrate Name kb
-- +migrate Group knowledge
-- +migrate Up
CREATE TABLE kb (id INTEGER);`
	if err := m.RegisterFromReader(strings.NewReader(content)); err != nil {
		t.Fatalf("RegisterFromReader: %v", err)
	}
	if got := m.migrations[0].Group; got != "knowledge" {
		t.Errorf("Group = %q, want knowledge", got)
	}
}

func TestParseInvalidGroupName(t *testing.T) {
	m, _ := newSQLiteMigrator(t)
	content := `-- +migrate Version 1
-- +migrate Group bad name!
-- +migrate Up
SELECT 1;`
	if err := m.RegisterFromReader(strings.NewReader(content)); err == nil {
		t.Fatal("expected error for invalid group name")
	}
}

func TestParseNoGroupIsDefault(t *testing.T) {
	m, _ := newSQLiteMigrator(t)
	content := `-- +migrate Version 1
-- +migrate Name plain
-- +migrate Up
SELECT 1;`
	if err := m.RegisterFromReader(strings.NewReader(content)); err != nil {
		t.Fatalf("RegisterFromReader: %v", err)
	}
	if got := m.migrations[0].Group; got != "" {
		t.Errorf("Group = %q, want empty (default)", got)
	}
}

// ---- register ----

func TestRegisterDupGroupVerRejected(t *testing.T) {
	m, _ := newSQLiteMigrator(t)
	mustReg(t, m, Migration{Group: "knowledge", Version: 1, Name: "a", Up: "SELECT 1"})
	err := m.Register(Migration{Group: "knowledge", Version: 1, Name: "b", Up: "SELECT 2"})
	if err == nil {
		t.Fatal("expected error for duplicate (group, version)")
	}
}

func TestRegisterSameVerTwoGroups(t *testing.T) {
	m, _ := newSQLiteMigrator(t)
	mustReg(t, m, Migration{Group: "knowledge", Version: 1, Name: "kb", Up: "SELECT 1"})
	mustReg(t, m, Migration{Group: "search", Version: 1, Name: "srch", Up: "SELECT 1"})
	if len(m.migrations) != 2 {
		t.Fatalf("expected 2 migrations, got %d", len(m.migrations))
	}
}

func TestRegisterInvalidGroup(t *testing.T) {
	m, _ := newSQLiteMigrator(t)
	if err := m.Register(Migration{Group: "bad name", Version: 1, Up: "SELECT 1"}); err == nil {
		t.Fatal("expected error for invalid group name")
	}
}

// ---- Up ordering + selection ----

func TestGroupUpOrdersByVersionThenGroup(t *testing.T) {
	m, db := newSQLiteMigrator(t)
	ctx := context.Background()
	mustReg(t, m, Migration{Group: "a", Version: 1, Name: "create", Up: "CREATE TABLE seq (id INTEGER PRIMARY KEY AUTOINCREMENT, who TEXT)"})
	mustReg(t, m, Migration{Group: "b", Version: 1, Name: "b1", Up: "INSERT INTO seq (who) VALUES ('b1')"})
	mustReg(t, m, Migration{Group: "a", Version: 2, Name: "a2", Up: "INSERT INTO seq (who) VALUES ('a2')"})
	mustReg(t, m, Migration{Group: "b", Version: 2, Name: "b2", Up: "INSERT INTO seq (who) VALUES ('b2')"})

	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	got := seqOrder(t, db)
	want := []string{"b1", "a2", "b2"} // (1,a)create → (1,b) → (2,a) → (2,b)
	if len(got) != len(want) {
		t.Fatalf("seq order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("seq[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestGroupUpSingleGroupLeavesOthers(t *testing.T) {
	m, _ := newSQLiteMigrator(t)
	ctx := context.Background()
	mustReg(t, m, Migration{Version: 1, Name: "core", Up: "CREATE TABLE core (id INTEGER)", Down: "DROP TABLE core"})
	mustReg(t, m, Migration{Group: "knowledge", Version: 1, Name: "kb", Up: "CREATE TABLE kb (id INTEGER)", Down: "DROP TABLE kb"})

	if err := m.Up(ctx, "knowledge"); err != nil {
		t.Fatalf("Up(knowledge): %v", err)
	}
	st, err := m.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(st.Applied) != 1 || st.Applied[0].Group != "knowledge" {
		t.Fatalf("expected only knowledge applied, got %+v", st.Applied)
	}
	if len(st.Pending) != 1 || st.Pending[0].Group != "" {
		t.Fatalf("expected default-group pending, got %+v", st.Pending)
	}
}

func TestGroupUpEnableLaterAppliesPending(t *testing.T) {
	m, db := newSQLiteMigrator(t)
	ctx := context.Background()
	mustReg(t, m, Migration{Version: 1, Name: "core", Up: "CREATE TABLE core (id INTEGER)", Down: "DROP TABLE core"})

	// First boot: default group only.
	if err := m.Up(ctx); err != nil {
		t.Fatalf("first Up: %v", err)
	}

	// Second boot: knowledge module registered. Only its pending set applies.
	m2 := New(db, WithDialect(DialectSQLite))
	mustReg(t, m2, Migration{Version: 1, Name: "core", Up: "CREATE TABLE core (id INTEGER)", Down: "DROP TABLE core"})
	mustReg(t, m2, Migration{Group: "knowledge", Version: 1, Name: "kb", Up: "CREATE TABLE kb (id INTEGER)", Down: "DROP TABLE kb"})

	if err := m2.Up(ctx, "knowledge"); err != nil {
		t.Fatalf("Up(knowledge): %v", err)
	}
	st, _ := m2.Status(ctx)
	if len(st.Applied) != 2 {
		t.Fatalf("expected 2 applied after enabling knowledge, got %d", len(st.Applied))
	}
	if len(st.Pending) != 0 {
		t.Fatalf("expected 0 pending, got %d", len(st.Pending))
	}
}

// ---- Down scoped ----

func TestGroupDownScopedToSelected(t *testing.T) {
	m, _ := newSQLiteMigrator(t)
	ctx := context.Background()
	mustReg(t, m, Migration{Version: 1, Name: "core", Up: "CREATE TABLE core (id INTEGER)", Down: "DROP TABLE core"})
	mustReg(t, m, Migration{Group: "knowledge", Version: 1, Name: "kb", Up: "CREATE TABLE kb (id INTEGER)", Down: "DROP TABLE kb"})
	mustReg(t, m, Migration{Group: "knowledge", Version: 2, Name: "kb2", Up: "CREATE TABLE kb2 (id INTEGER)", Down: "DROP TABLE kb2"})

	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	// Roll back 1 migration in knowledge only → kb2 dropped, core + kb remain.
	if err := m.Down(ctx, 1, "knowledge"); err != nil {
		t.Fatalf("Down(knowledge): %v", err)
	}
	st, _ := m.Status(ctx)
	appliedGroups := make(map[string]int)
	for _, rec := range st.Applied {
		appliedGroups[rec.Group]++
	}
	if appliedGroups[""] != 1 {
		t.Errorf("default group: %d applied, want 1", appliedGroups[""])
	}
	if appliedGroups["knowledge"] != 1 {
		t.Errorf("knowledge group: %d applied, want 1 (kb2 rolled back)", appliedGroups["knowledge"])
	}
}

// ---- Status scoped ----

func TestGroupStatusScoped(t *testing.T) {
	m, _ := newSQLiteMigrator(t)
	ctx := context.Background()
	mustReg(t, m, Migration{Version: 1, Name: "core", Up: "CREATE TABLE core (id INTEGER)", Down: "DROP TABLE core"})
	mustReg(t, m, Migration{Group: "knowledge", Version: 1, Name: "kb", Up: "CREATE TABLE kb (id INTEGER)", Down: "DROP TABLE kb"})

	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	st, err := m.Status(ctx, "knowledge")
	if err != nil {
		t.Fatalf("Status(knowledge): %v", err)
	}
	if len(st.Applied) != 1 || st.Applied[0].Group != "knowledge" {
		t.Fatalf("expected only knowledge in scoped status, got %+v", st.Applied)
	}
	if len(st.Pending) != 0 {
		t.Fatalf("expected 0 pending, got %d", len(st.Pending))
	}
}

// ---- Force group args ----

func TestForceZeroArgsTargetsDefault(t *testing.T) {
	m, _ := newSQLiteMigrator(t)
	ctx := context.Background()
	mustReg(t, m, Migration{Version: 1, Name: "core", Up: "CREATE TABLE core (id INTEGER)", Down: "DROP TABLE core"})
	mustReg(t, m, Migration{Group: "knowledge", Version: 1, Name: "kb", Up: "CREATE TABLE kb (id INTEGER)", Down: "DROP TABLE kb"})

	if err := m.Force(ctx, 1, true); err != nil {
		t.Fatalf("Force default: %v", err)
	}
	st, _ := m.Status(ctx)
	// Default v1 forced applied, knowledge v1 still pending.
	var hasDefault, hasKBPending bool
	for _, rec := range st.Applied {
		if rec.Group == "" && rec.Version == 1 {
			hasDefault = true
		}
	}
	for _, mig := range st.Pending {
		if mig.Group == "knowledge" {
			hasKBPending = true
		}
	}
	if !hasDefault {
		t.Error("default v1 not force-applied")
	}
	if !hasKBPending {
		t.Error("knowledge v1 should be pending after Force on default")
	}
}

func TestForceOneArgTargetsGroup(t *testing.T) {
	m, _ := newSQLiteMigrator(t)
	ctx := context.Background()
	mustReg(t, m, Migration{Group: "knowledge", Version: 1, Name: "kb", Up: "CREATE TABLE kb (id INTEGER)", Down: "DROP TABLE kb"})

	if err := m.Force(ctx, 1, true, "knowledge"); err != nil {
		t.Fatalf("Force(knowledge): %v", err)
	}
	st, _ := m.Status(ctx)
	if len(st.Applied) != 1 || st.Applied[0].Group != "knowledge" {
		t.Fatalf("expected knowledge v1 force-applied, got %+v", st.Applied)
	}
}

func TestForceTwoArgsErrors(t *testing.T) {
	m, _ := newSQLiteMigrator(t)
	ctx := context.Background()
	if err := m.Force(ctx, 1, true, "a", "b"); err == nil {
		t.Fatal("expected error for Force with 2 group args")
	}
}

// ---- integrity per group ----

func TestGroupChecksumDriftDetected(t *testing.T) {
	m, db := newSQLiteMigrator(t)
	ctx := context.Background()
	mustReg(t, m, Migration{Group: "knowledge", Version: 1, Name: "kb", Up: "CREATE TABLE kb (id INTEGER)", Down: "DROP TABLE kb"})
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	// Same (group, version), mutated Up SQL → drift.
	m2 := New(db, WithDialect(DialectSQLite))
	mustReg(t, m2, Migration{Group: "knowledge", Version: 1, Name: "kb", Up: "CREATE TABLE kb (id INTEGER, extra INTEGER)", Down: "DROP TABLE kb"})
	err := m2.Up(ctx)
	var cm *ChecksumMismatchError
	if !errors.As(err, &cm) {
		t.Fatalf("expected ChecksumMismatchError, got %v", err)
	}
	if cm.Version != 1 {
		t.Errorf("drift version = %d, want 1", cm.Version)
	}
}

func TestGroupDirtyBlocksAndNamesGroup(t *testing.T) {
	m, _ := newSQLiteMigrator(t)
	ctx := context.Background()
	mustReg(t, m, Migration{
		Group:         "knowledge",
		Version:       1,
		Name:          "halfbad",
		Up:            "CREATE TABLE half (id INT); SELECT this_is_not_valid_sql;",
		Down:          "DROP TABLE IF EXISTS half",
		NoTransaction: true,
	})
	if err := m.Up(ctx); err == nil {
		t.Fatal("expected the failing NoTransaction migration to error")
	}
	// Re-run: must refuse with ErrDirty.
	m2 := New(m.db, WithDialect(DialectSQLite))
	mustReg(t, m2, Migration{Group: "knowledge", Version: 1, Name: "halfbad", Up: "SELECT 1", Down: "SELECT 1", NoTransaction: true})
	err := m2.Up(ctx)
	if !errors.Is(err, ErrDirty) {
		t.Fatalf("expected ErrDirty, got %v", err)
	}
	// The error message should name the group.
	if !strings.Contains(err.Error(), "knowledge") {
		t.Errorf("dirty error should name the group: %v", err)
	}
}

// ---- legacy-table upgrade (option a) ----

func TestLegacyTableUpgradedToGroups(t *testing.T) {
	m, db := newSQLiteMigrator(t)
	ctx := context.Background()
	// Phase 1: default-group-only → legacy single-column PK table.
	mustReg(t, m, Migration{Version: 1, Name: "core", Up: "CREATE TABLE core (id INTEGER)", Down: "DROP TABLE core"})
	if err := m.Up(ctx); err != nil {
		t.Fatalf("first Up: %v", err)
	}
	cols := pkColumns(t, db)
	if len(cols) != 1 || cols[0] != "version" {
		t.Fatalf("legacy table PK = %v, want [version]", cols)
	}

	// Phase 2: register a non-default group and Up → table upgraded to composite PK.
	mustReg(t, m, Migration{Group: "knowledge", Version: 1, Name: "kb", Up: "CREATE TABLE kb (id INTEGER)", Down: "DROP TABLE kb"})
	if err := m.Up(ctx); err != nil {
		t.Fatalf("second Up with groups: %v", err)
	}
	cols = pkColumns(t, db)
	if len(cols) != 2 || cols[0] != "group_name" || cols[1] != "version" {
		t.Fatalf("upgraded table PK = %v, want [group_name version]", cols)
	}
	// Both migrations applied.
	st, _ := m.Status(ctx)
	if len(st.Applied) != 2 {
		t.Fatalf("expected 2 applied after upgrade, got %d", len(st.Applied))
	}
}

// TestLegacyTableCollisionAllowedAfterUpgrade verifies the core uniqueness
// property: after upgrading a legacy table, two groups can each own a version 1
// without corrupting the tracking table — the exact hazard the composite key
// exists to prevent.
func TestLegacyTableCollisionAllowedAfterUpgrade(t *testing.T) {
	m, db := newSQLiteMigrator(t)
	ctx := context.Background()
	mustReg(t, m, Migration{Version: 1, Name: "core", Up: "CREATE TABLE core (id INTEGER)", Down: "DROP TABLE core"})
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up default: %v", err)
	}
	// Now add knowledge v1 — same version number, different group.
	mustReg(t, m, Migration{Group: "knowledge", Version: 1, Name: "kb", Up: "CREATE TABLE kb (id INTEGER)", Down: "DROP TABLE kb"})
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up with colliding version across groups: %v", err)
	}
	// Both rows coexist.
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM _migrations WHERE version = 1").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 rows with version=1 (different groups), got %d", n)
	}
}

// ---- F1: de-registered group must not brick Up/Status ----

// TestDeregisteredGroupDoesNotBrickUp verifies that after a group was applied
// (table upgraded, ('knowledge', N) row present), a later run that no longer
// registers that group reads the table's real group values — not the legacy
// path that scans Group="" for every row. Without table-state detection the
// knowledge row is misattributed to the default group, collides with/shadows
// the default-group row in the migKey map, and Up() returns a false
// checksum-mismatch error.
func TestDeregisteredGroupDoesNotBrickUp(t *testing.T) {
	m1, db := newSQLiteMigrator(t)
	ctx := context.Background()
	mustReg(t, m1, Migration{Version: 1, Name: "core", Up: "CREATE TABLE f1a (id INT)", Down: "DROP TABLE f1a"})
	mustReg(t, m1, Migration{Group: "knowledge", Version: 1, Name: "kb", Up: "CREATE TABLE f1b (id INT)", Down: "DROP TABLE f1b"})
	if err := m1.Up(ctx); err != nil {
		t.Fatalf("Up both: %v", err)
	}

	// Phase 2: fresh Migrator on the SAME db — only default v1 registered.
	// The knowledge group is de-registered (module disabled / file deleted).
	m2 := New(db, WithDialect(DialectSQLite))
	mustReg(t, m2, Migration{Version: 1, Name: "core", Up: "CREATE TABLE f1a (id INT)", Down: "DROP TABLE f1a"})

	// Status must show the knowledge row with its real group, not as default.
	st, err := m2.Status(ctx)
	if err != nil {
		t.Fatalf("Status after de-register: %v", err)
	}
	var foundKnowledge bool
	for _, rec := range st.Applied {
		if rec.Group == "knowledge" {
			foundKnowledge = true
		}
	}
	if !foundKnowledge {
		t.Fatalf("Status lost the knowledge row (misattributed to default?): applied=%+v", st.Applied)
	}

	// Up must not return a false checksum error.
	if err := m2.Up(ctx); err != nil {
		t.Fatalf("Up after de-register returned error (false checksum mismatch?): %v", err)
	}
}

// TestDeregisteredGroupDoesNotShadowNewDefaultVersion verifies F1's second
// variant: after knowledge v2 was applied, a newly registered default v2
// must still apply. Without table-state detection the knowledge v2 row is
// read with Group="" and shadows the default v2 key, so Status reports
// Pending: 0 and the default migration never runs.
func TestDeregisteredGroupDoesNotShadowNewDefaultVersion(t *testing.T) {
	m1, db := newSQLiteMigrator(t)
	ctx := context.Background()
	mustReg(t, m1, Migration{Version: 1, Name: "core", Up: "CREATE TABLE f1c (id INT)", Down: "DROP TABLE f1c"})
	mustReg(t, m1, Migration{Group: "knowledge", Version: 2, Name: "kb2", Up: "CREATE TABLE f1d (id INT)", Down: "DROP TABLE f1d"})
	if err := m1.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}

	// Phase 2: fresh Migrator — default v1 + NEW default v2 registered.
	m2 := New(db, WithDialect(DialectSQLite))
	mustReg(t, m2, Migration{Version: 1, Name: "core", Up: "CREATE TABLE f1c (id INT)", Down: "DROP TABLE f1c"})
	mustReg(t, m2, Migration{Version: 2, Name: "core2", Up: "CREATE TABLE f1e (id INT)", Down: "DROP TABLE f1e"})

	st, err := m2.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(st.Pending) != 1 || st.Pending[0].Version != 2 {
		t.Fatalf("expected default v2 pending, got pending=%+v", st.Pending)
	}

	if err := m2.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if !tableExists(t, db, "f1e") {
		t.Fatal("default v2 was not applied (shadowed by knowledge v2)")
	}
}

// ---- F4: Status must not run the PK upgrade ----

// TestStatusDoesNotUpgradePK proves Status is read-only: it must not call
// ensureCompositeKey (an unlocked check-then-act). After a legacy Up, adding
// a group and calling Status must leave the PK as single-column [version];
// only Up/Down/Force (which hold the advisory lock) may upgrade.
func TestStatusDoesNotUpgradePK(t *testing.T) {
	m, db := newSQLiteMigrator(t)
	ctx := context.Background()
	mustReg(t, m, Migration{Version: 1, Name: "core", Up: "CREATE TABLE f4a (id INT)", Down: "DROP TABLE f4a"})
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if cols := pkColumns(t, db); len(cols) != 1 || cols[0] != "version" {
		t.Fatalf("legacy PK = %v, want [version]", cols)
	}

	// Register a group → ga becomes true. Status must NOT upgrade the PK.
	mustReg(t, m, Migration{Group: "knowledge", Version: 1, Name: "kb", Up: "CREATE TABLE f4b (id INT)", Down: "DROP TABLE f4b"})
	if _, err := m.Status(ctx); err != nil {
		t.Fatalf("Status: %v", err)
	}
	if cols := pkColumns(t, db); len(cols) != 1 || cols[0] != "version" {
		t.Fatalf("after Status PK = %v, want [version] (Status must not upgrade)", cols)
	}
}

// TestRebuildSQLitePreservesGroupName proves the SQLite table rebuild copies
// the real group_name value instead of hardcoding ”. A row with a non-default
// group_name that exists before the rebuild must survive losslessly.
func TestRebuildSQLitePreservesGroupName(t *testing.T) {
	m, db := newSQLiteMigrator(t)
	ctx := context.Background()
	mustReg(t, m, Migration{Version: 1, Name: "core", Up: "CREATE TABLE f4c (id INT)", Down: "DROP TABLE f4c"})
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}

	// Simulate a table that has group_name data but a single-column PK:
	// manually add the column and insert a row with a non-default group.
	if _, err := db.Exec("ALTER TABLE _migrations ADD COLUMN group_name TEXT NOT NULL DEFAULT ''"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO _migrations (group_name, version, name, checksum, dirty) VALUES ('custom', 2, 'manual', '', 0)"); err != nil {
		t.Fatal(err)
	}

	// Trigger rebuildTableSQLite via Up with a group registered.
	mustReg(t, m, Migration{Group: "knowledge", Version: 1, Name: "kb", Up: "CREATE TABLE f4d (id INT)", Down: "DROP TABLE f4d"})
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up with groups: %v", err)
	}

	// The rebuilt table must have preserved the non-default group_name.
	var group string
	if err := db.QueryRow("SELECT group_name FROM _migrations WHERE name = 'manual'").Scan(&group); err != nil {
		t.Fatalf("query manual row: %v", err)
	}
	if group != "custom" {
		t.Fatalf("group_name = %q, want 'custom' (rebuild must be lossless)", group)
	}
}

// ---- F5: unknown group selection must error ----

// TestUpUnknownGroupErrors proves that Up with a group name that doesn't
// match any registered migration returns a descriptive error instead of
// silently no-op-ing.
func TestUpUnknownGroupErrors(t *testing.T) {
	m, _ := newSQLiteMigrator(t)
	ctx := context.Background()
	mustReg(t, m, Migration{Version: 1, Name: "core", Up: "CREATE TABLE f5a (id INT)", Down: "DROP TABLE f5a"})
	mustReg(t, m, Migration{Group: "knowledge", Version: 1, Name: "kb", Up: "CREATE TABLE f5b (id INT)", Down: "DROP TABLE f5b"})

	err := m.Up(ctx, "knowldge") // typo
	if err == nil {
		t.Fatal("Up with unknown group should error, got nil")
	}
	if !strings.Contains(err.Error(), "knowldge") {
		t.Fatalf("error should name the unknown group, got: %v", err)
	}
	if !strings.Contains(err.Error(), "knowledge") {
		t.Fatalf("error should name a known group, got: %v", err)
	}
}

// TestDownUnknownGroupErrors proves Down with an unknown group also errors.
func TestDownUnknownGroupErrors(t *testing.T) {
	m, _ := newSQLiteMigrator(t)
	ctx := context.Background()
	mustReg(t, m, Migration{Version: 1, Name: "core", Up: "CREATE TABLE f5c (id INT)", Down: "DROP TABLE f5c"})

	if err := m.Down(ctx, 1, "nonexistent"); err == nil {
		t.Fatal("Down with unknown group should error")
	}
}

// TestStatusUnknownGroupErrors proves Status with an unknown group errors.
func TestStatusUnknownGroupEmptyNotError(t *testing.T) {
	// Status is a read: an unregistered group is inspectable (it may be a
	// disabled module's applied rows), so selection is syntax-validated only
	// and an unknown group reports empty rather than erroring like Up/Down.
	m, _ := newSQLiteMigrator(t)
	ctx := context.Background()
	mustReg(t, m, Migration{Version: 1, Name: "core", Up: "CREATE TABLE f5d (id INT)", Down: "DROP TABLE f5d"})

	st, err := m.Status(ctx, "nope")
	if err != nil {
		t.Fatalf("Status with unknown group should read empty, got error: %v", err)
	}
	if len(st.Applied) != 0 || len(st.Pending) != 0 {
		t.Fatalf("unknown group status: applied=%d pending=%d, want 0/0", len(st.Applied), len(st.Pending))
	}
	// Syntactically invalid selection still errors.
	if _, err := m.Status(ctx, "bad name!"); err == nil {
		t.Fatal("Status with invalid group name should error")
	}
}

func TestDownNotBrickedByUnregisteredDirty(t *testing.T) {
	// A de-registered (disabled) module's dirty row must not block another
	// group's rollback — Down scopes its dirty scan to registered groups,
	// mirroring checkIntegrity. Recovery for the disabled module is Force.
	m, db := newSQLiteMigrator(t)
	ctx := context.Background()
	mustReg(t, m, Migration{Version: 1, Name: "core", Up: "CREATE TABLE dnb (id INT)", Down: "DROP TABLE dnb"})
	mustReg(t, m, Migration{Group: "knowledge", Version: 1, Name: "kb", Up: "CREATE TABLE dnb_kb (id INT)", Down: "DROP TABLE dnb_kb"})
	if err := m.Up(ctx); err != nil {
		t.Fatalf("up: %v", err)
	}
	if _, err := db.Exec(`UPDATE _migrations SET dirty = 1 WHERE group_name = 'knowledge'`); err != nil {
		t.Fatalf("mark dirty: %v", err)
	}

	// Fresh migrator WITHOUT the knowledge group registered. Its dirty row
	// must not block, and its rows must not be rollback candidates — the
	// unscoped Down rolls back the default group's row.
	m2 := New(db, WithDialect(DialectSQLite))
	mustReg(t, m2, Migration{Version: 1, Name: "core", Up: "CREATE TABLE dnb (id INT)", Down: "DROP TABLE dnb"})
	if err := m2.Down(ctx, 1); err != nil {
		t.Fatalf("down blocked by a disabled module's dirty row: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='dnb'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("default group's table should be rolled back (n=%d, err=%v)", n, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM _migrations WHERE group_name = 'knowledge'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("disabled module's row must survive unscoped Down (n=%d, err=%v)", n, err)
	}
	// The registered group's dirty row still blocks.
	if _, err := db.Exec(`UPDATE _migrations SET dirty = 1 WHERE group_name = 'knowledge'`); err != nil {
		t.Fatalf("re-mark dirty: %v", err)
	}
	m3 := New(db, WithDialect(DialectSQLite))
	mustReg(t, m3, Migration{Group: "knowledge", Version: 1, Name: "kb", Up: "CREATE TABLE dnb_kb (id INT)", Down: "DROP TABLE dnb_kb"})
	if err := m3.Down(ctx, 1); err == nil {
		t.Fatal("down should block on a REGISTERED group's dirty row")
	}
}

func TestStatusNeverAddsGroupColumn(t *testing.T) {
	// Status performs no group-schema DDL: with a group registered against a
	// legacy table, Status must not add group_name (that upgrade belongs to
	// Up/Down/Force under the advisory lock).
	m, db := newSQLiteMigrator(t)
	ctx := context.Background()
	mustReg(t, m, Migration{Version: 1, Name: "core", Up: "CREATE TABLE snad (id INT)", Down: "DROP TABLE snad"})
	if err := m.Up(ctx); err != nil { // legacy table exists now
		t.Fatalf("up: %v", err)
	}
	mustReg(t, m, Migration{Group: "knowledge", Version: 1, Name: "kb", Up: "CREATE TABLE snad_kb (id INT)", Down: "DROP TABLE snad_kb"})
	if _, err := m.Status(ctx); err != nil {
		t.Fatalf("status: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('_migrations') WHERE name = 'group_name'`).Scan(&n); err != nil {
		t.Fatalf("pragma: %v", err)
	}
	if n != 0 {
		t.Fatal("Status added group_name to a legacy table")
	}
}

func TestDefaultAliasSelectsDefaultGroup(t *testing.T) {
	// "default" addresses the default ("") group in selections — the only
	// CLI syntax for it — and is reserved as a registered group name.
	m, db := newSQLiteMigrator(t)
	ctx := context.Background()
	mustReg(t, m, Migration{Version: 1, Name: "core", Up: "CREATE TABLE dal (id INT)", Down: "DROP TABLE dal"})
	mustReg(t, m, Migration{Group: "knowledge", Version: 1, Name: "kb", Up: "CREATE TABLE dal_kb (id INT)", Down: "DROP TABLE dal_kb"})
	if err := m.Up(ctx, "default"); err != nil {
		t.Fatalf("up default alias: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='dal'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("default group not applied via alias (n=%d, err=%v)", n, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='dal_kb'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("alias selection leaked into knowledge group (n=%d, err=%v)", n, err)
	}
	if err := m.Register(Migration{Group: "default", Version: 9, Name: "bad", Up: "SELECT 1"}); err == nil {
		t.Fatal(`registering a group literally named "default" should be rejected (reserved)`)
	}
}
