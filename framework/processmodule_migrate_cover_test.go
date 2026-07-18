package framework

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// This file adds unit coverage for the PURE helpers in processmodule_migrate.go
// (firstLine, isPlainDDL, lintMigrationSQL, stripSQLComments, sanitizeIdent,
// moduleSchemaRole, moduleRoleDSN, shortHash, truncate, randomHex) and the
// CoordinatorValidationError + NewMigrationCoordinator + Plan paths that need
// no Postgres. Apply itself is PG-gated and covered elsewhere.

// ---- firstLine ----

func TestFirstLine_extractsFirstLine(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"CREATE TABLE x", "CREATE TABLE x"},
		{"CREATE TABLE x\n  (id int)", "CREATE TABLE x"},
		{"  CREATE TABLE x  ", "CREATE TABLE x"}, // surrounding whitespace trimmed
		{"  \n  CREATE TABLE x", ""},             // first line is whitespace-only → empty
		{"", ""},
	}
	for _, c := range cases {
		if got := firstLine(c.in); got != c.want {
			t.Errorf("firstLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ---- isPlainDDL ----

func TestIsPlainDDL_classifies(t *testing.T) {
	pass := []string{
		"CREATE TABLE x (id int)",
		"CREATE INDEX i ON x (a)",
		"CREATE UNIQUE INDEX i ON x (a)",
		"ALTER TABLE x ADD COLUMN name text",
		"CREATE SCHEMA s",
	}
	for _, s := range pass {
		if !isPlainDDL(s) {
			t.Errorf("isPlainDDL(%q) = false, want true", s)
		}
	}
	fail := []string{
		"DROP TABLE x",
		"CREATE TABLE y AS SELECT * FROM x", // bypass: AS SELECT
		"TRUNCATE x",
		"GRANT SELECT ON x TO y",
		"INSERT INTO x VALUES (1)",
	}
	for _, s := range fail {
		if isPlainDDL(s) {
			t.Errorf("isPlainDDL(%q) = true, want false", s)
		}
	}
}

// ---- lintMigrationSQL ----

func TestLintMigrationSQL_emptyReturnsNil(t *testing.T) {
	if got := lintMigrationSQL(""); got != nil {
		t.Errorf("lintMigrationSQL('') = %+v, want nil", got)
	}
	if got := lintMigrationSQL("   \n  "); got != nil {
		t.Errorf("lintMigrationSQL(whitespace) = %+v, want nil", got)
	}
}

func TestLintMigrationSQL_flagsAndPasses(t *testing.T) {
	if hints := lintMigrationSQL("CREATE TABLE x (id int)"); len(hints) != 0 {
		t.Errorf("plain DDL hints = %+v, want none", hints)
	}
	hints := lintMigrationSQL("CREATE TABLE x (id int); DROP TABLE y; -- ok")
	if len(hints) == 0 {
		t.Error("mixed DDL produced no lint hints")
	}
	// The hint must reference the non-plain statement, not the plain one.
	joined := strings.Join(hints, " ")
	if !strings.Contains(joined, "DROP TABLE y") {
		t.Errorf("lint hints = %q, want DROP TABLE y flagged", joined)
	}
}

// ---- stripSQLComments ----

func TestStripSQLComments_lineAndBlock(t *testing.T) {
	got := stripSQLComments("-- line comment\nCREATE TABLE x (id int) /* block */ ;")
	if strings.Contains(got, "--") || strings.Contains(got, "/*") || strings.Contains(got, "block") {
		t.Errorf("comments not stripped: %q", got)
	}
	if !strings.Contains(got, "CREATE TABLE") {
		t.Errorf("code stripped: %q", got)
	}
}

func TestStripSQLComments_unterminatedBlock(t *testing.T) {
	// An unterminated /* is tolerated (loop bound by len(s)); the function
	// must not panic and must return the prefix before the block opener.
	got := stripSQLComments("SELECT 1 /* never closed")
	if !strings.Contains(got, "SELECT 1") {
		t.Errorf("prefix lost on unterminated block: %q", got)
	}
}

// ---- sanitizeIdent ----

func TestSanitizeIdent_normalizes(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Demo", "demo"},
		{"my-module", "my_module"},
		{"Module_1", "module_1"},
		{"with space!", "with_space_"},
		{"", "module"}, // empty → fallback
		{"UPPER", "upper"},
	}
	for _, c := range cases {
		if got := sanitizeIdent(c.in); got != c.want {
			t.Errorf("sanitizeIdent(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ---- moduleSchemaRole ----

func TestModuleSchemaRole_derivesNames(t *testing.T) {
	schema, role := moduleSchemaRole("demo")
	if schema == "" || role == "" || schema == role {
		t.Errorf("moduleSchemaRole(demo) = %q,%q", schema, role)
	}
	// A hyphenated name is sanitized to underscores in both halves.
	schema2, role2 := moduleSchemaRole("my-mod")
	if !strings.Contains(schema2, "my_mod") || !strings.Contains(role2, "my_mod") {
		t.Errorf("moduleSchemaRole(my-mod) = %q,%q, want sanitized", schema2, role2)
	}
}

// ---- moduleRoleDSN ----

func TestModuleRoleDSN_rewritesUserinfo(t *testing.T) {
	got, err := moduleRoleDSN("postgres://host:5432/db", "role1", "secret")
	if err != nil {
		t.Fatalf("moduleRoleDSN: %v", err)
	}
	if !strings.Contains(got, "role1:secret@") {
		t.Errorf("moduleRoleDSN = %q, want role1:secret userinfo", got)
	}
}

func TestModuleRoleDSN_rejectsNonPostgresScheme(t *testing.T) {
	if _, err := moduleRoleDSN("mysql://host/db", "r", "p"); err == nil {
		t.Error("moduleRoleDSN must reject a non-postgres scheme")
	}
	if _, err := moduleRoleDSN(":::not-a-url:::", "r", "p"); err == nil {
		t.Error("moduleRoleDSN must reject an unparseable DSN")
	}
}

// ---- shortHash ----

func TestShortHash_trimsTo12(t *testing.T) {
	if got := shortHash("abcdef"); got != "abcdef" {
		t.Errorf("shortHash(short) = %q", got)
	}
	if got := shortHash("0123456789abcdef"); len(got) != 12 || got != "0123456789ab" {
		t.Errorf("shortHash(long) = %q, want first 12", got)
	}
}

// ---- truncate ----

func TestTruncate_clips(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate(short) = %q", got)
	}
	if got := truncate("0123456789abc", 10); !strings.HasPrefix(got, "0123") || !strings.Contains(got, "…") {
		t.Errorf("truncate(long) = %q, want ellipsis suffix", got)
	}
}

// ---- randomHex ----

func TestRandomHex_length(t *testing.T) {
	if got := randomHex(16); len(got) != 32 { // 16 bytes → 32 hex chars
		t.Errorf("randomHex(16) = len %d, want 32", len(got))
	}
	// Two calls produce different values (probabilistic; fixed-length random).
	if a, b := randomHex(8), randomHex(8); a == b {
		t.Errorf("randomHex not random: %q == %q", a, b)
	}
}

// ---- Dialect (coordinator) ----

func TestCoord_DialectSQLite(t *testing.T) {
	store := newTestStore(t)
	c, err := NewMigrationCoordinator(store, store.db)
	if err != nil {
		t.Fatalf("NewMigrationCoordinator: %v", err)
	}
	if got := c.Dialect(); got != migrate.DialectSQLite {
		t.Errorf("Dialect = %v, want SQLite", got)
	}
}

// ---- CoordinatorValidationError ----

func TestCoordinatorValidationError_errorAndIs(t *testing.T) {
	e := coordErr("f", "rule-x", "boom")
	if e.Error() != "boom" {
		t.Errorf("Error() = %q", e.Error())
	}
	// Is matches on Field+Rule.
	target := coordErr("f", "rule-x", "different message")
	if !errors.Is(e, target) {
		t.Error("Is must match same Field+Rule")
	}
	// Is does NOT match a different rule.
	other := coordErr("f", "rule-y", "boom")
	if errors.Is(e, other) {
		t.Error("Is must not match different Rule")
	}
	// Is does NOT match a non-CoordinatorValidationError.
	if errors.Is(e, errors.New("plain")) {
		t.Error("Is must not match a plain error")
	}
	// nil receiver renders safely.
	var nilE *CoordinatorValidationError
	if nilE.Error() != "<nil>" {
		t.Errorf("nil Error() = %q, want <nil>", nilE.Error())
	}
}

// ---- NewMigrationCoordinator nil-arg errors ----

func TestNewMigrationCoordinator_nilArgs(t *testing.T) {
	if _, err := NewMigrationCoordinator(nil, nil); err == nil {
		t.Error("nil store must error")
	}
	store := newTestStore(t)
	if _, err := NewMigrationCoordinator(store, nil); err == nil {
		t.Error("nil adminDB must error")
	}
}

// ---- Plan extra validation paths ----

func TestPlan_rejectsEmptyMigrationName(t *testing.T) {
	store, desc := coordStoreWithModule(t, "demo", TrustTrusted)
	c, _ := NewMigrationCoordinator(store, store.db)
	_, err := c.Plan(desc, []ApprovedMigration{{Version: 1, Name: "", Up: "CREATE TABLE x (id int)"}})
	var ve *CoordinatorValidationError
	if !errors.As(err, &ve) || ve.Rule != "empty" {
		t.Fatalf("empty-name Plan = %v, want empty-rule error", err)
	}
}

func TestPlan_emptyShaSkipsCheck(t *testing.T) {
	store, desc := coordStoreWithModule(t, "demo", TrustTrusted)
	c, _ := NewMigrationCoordinator(store, store.db)
	// SHA256 == "" skips the per-migration digest check (artifact-level digest
	// is the trust anchor); the plan succeeds and carries the computed digest.
	plan, err := c.Plan(desc, []ApprovedMigration{{Version: 1, Name: "a", Up: "CREATE TABLE x (id int)"}})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].Computed == "" || plan.Steps[0].Approved != "" {
		t.Errorf("plan step = %+v", plan.Steps[0])
	}
}

func TestPlan_multipleStepsAndWarnings(t *testing.T) {
	store, desc := coordStoreWithModule(t, "demo", TrustTrusted)
	c, _ := NewMigrationCoordinator(store, store.db)
	migs := []ApprovedMigration{
		{Version: 1, Name: "a", Up: "CREATE TABLE a (id int)"},
		{Version: 2, Name: "b", Up: "CREATE TABLE b (id int); DROP TABLE a"},
	}
	plan, err := c.Plan(desc, migs)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(plan.Steps))
	}
	// Step 2 has non-plain DDL → a warning must be attached.
	if len(plan.Warnings) == 0 {
		t.Error("expected lint warnings for step 2's DROP TABLE")
	}
}

// ---- Apply: PG path rejects empty adminDSN (covers the non-PG guard) ----

func TestApply_postgresRequiresAdminDSN(t *testing.T) {
	store, desc := coordStoreWithModule(t, "demo", TrustTrusted)
	c, _ := NewMigrationCoordinator(store, store.db) // no WithCoordinatorAdminDSN
	// Force the postgres branch by hand-setting dialect + a pg-shaped plan.
	c.dialect = migrate.DialectPostgres
	err := c.applyPostgres(context.Background(), desc, &CoordinatorPlan{Group: "demo"})
	if err == nil || !strings.Contains(err.Error(), "WithCoordinatorAdminDSN") {
		t.Errorf("applyPostgres without admin DSN = %v, want WithCoordinatorAdminDSN error", err)
	}
}

// ---- stampApplied (SQLite: SetMigrationsAppliedAt + BumpGeneration) ----

func TestStampApplied_recordsTimestampAndBumps(t *testing.T) {
	store, _ := coordStoreWithModule(t, "demo", TrustTrusted)
	stamp := time.UnixMilli(1_700_000_000_000)
	c, _ := NewMigrationCoordinator(store, store.db, WithCoordinatorClock(func() time.Time { return stamp }))
	if err := c.stampApplied(context.Background(), "demo"); err != nil {
		t.Fatalf("stampApplied: %v", err)
	}
	got, err := store.GetDesired(context.Background(), "demo")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.MigrationsAppliedAt == nil || !got.MigrationsAppliedAt.Equal(stamp) {
		t.Errorf("MigrationsAppliedAt = %v, want %v", got.MigrationsAppliedAt, stamp)
	}
	if got.DesiredGeneration != 2 {
		t.Errorf("DesiredGeneration = %d, want 2 (bumped from 1)", got.DesiredGeneration)
	}
}

func TestStampApplied_unknownModuleErrors(t *testing.T) {
	store := newTestStore(t)
	c, _ := NewMigrationCoordinator(store, store.db)
	if err := c.stampApplied(context.Background(), "ghost"); err == nil {
		t.Error("stampApplied on unknown module must error")
	}
}
