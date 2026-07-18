package framework

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// This file adds unit coverage for the store helpers + ListDesired + the
// dialect/encoding helpers in processmodule_store.go. Uses the shared-cache
// SQLite :memory: store (the same pattern the supervisor tests use), NOT
// Postgres.

// ---- NewSQLProcessModuleStore ----

func TestNewSQLStore_nilDBErrors(t *testing.T) {
	if _, err := NewSQLProcessModuleStore(nil); err == nil {
		t.Error("NewSQLProcessModuleStore(nil) must error")
	}
}

// ---- Dialect ----

func TestStore_DialectSQLite(t *testing.T) {
	store := newTestStore(t)
	if got := store.Dialect(); got != migrate.DialectSQLite {
		t.Errorf("Dialect = %v, want SQLite", got)
	}
}

// ---- ListDesired ----

func TestListDesired_emptyReturnsNil(t *testing.T) {
	store := newTestStore(t)
	got, err := store.ListDesired(context.Background())
	if err != nil {
		t.Fatalf("ListDesired empty: %v", err)
	}
	if got != nil && len(got) != 0 {
		t.Errorf("ListDesired empty = %+v, want nil/empty", got)
	}
}

func TestListDesired_ordersByModule(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, name := range []string{"zeta", "alpha", "mike"} {
		if err := store.Install(ctx, DesiredState{Module: name, ArtifactSHA256: hex256(name[:2])}); err != nil {
			t.Fatalf("install %s: %v", name, err)
		}
	}
	got, err := store.ListDesired(ctx)
	if err != nil {
		t.Fatalf("ListDesired: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ListDesired len = %d, want 3", len(got))
	}
	if got[0].Module != "alpha" || got[1].Module != "mike" || got[2].Module != "zeta" {
		names := []string{got[0].Module, got[1].Module, got[2].Module}
		t.Errorf("ListDesired order = %v, want [alpha mike zeta]", names)
	}
}

// ---- AssertRow ----

type fakeSQLResult struct {
	rows int64
	err  error
}

func (f fakeSQLResult) LastInsertId() (int64, error) { return 0, nil }
func (f fakeSQLResult) RowsAffected() (int64, error) { return f.rows, f.err }

func TestAssertRow_zeroRowsIsErrNoDesired(t *testing.T) {
	err := AssertRow(fakeSQLResult{rows: 0}, "demo")
	if !errors.Is(err, ErrNoDesiredRow) {
		t.Errorf("AssertRow(0) = %v, want ErrNoDesiredRow", err)
	}
}

func TestAssertRow_positiveRowsOK(t *testing.T) {
	if err := AssertRow(fakeSQLResult{rows: 1}, "demo"); err != nil {
		t.Errorf("AssertRow(1) = %v, want nil", err)
	}
}

func TestAssertRow_scanErrorPropagates(t *testing.T) {
	scanErr := errors.New("driver: rows affected unavailable")
	err := AssertRow(fakeSQLResult{err: scanErr}, "demo")
	if err == nil || !strings.Contains(err.Error(), "rows affected") {
		t.Errorf("AssertRow(err) = %v, want rows-affected wrapper", err)
	}
}

// ---- enabledBool ----

func TestEnabledBool_allShapes(t *testing.T) {
	cases := []struct {
		in   any
		want bool
	}{
		{true, true},
		{false, false},
		{int64(1), true},
		{int64(0), false},
		{int(1), true},
		{int(0), false},
		{[]byte{'1'}, true},
		{[]byte{'t'}, true},
		{[]byte{'T'}, true},
		{[]byte{'0'}, false},
		{[]byte{}, false},
		{"t", true},
		{"true", true},
		{"1", true},
		{"f", false},
		{nil, false},
		{3.14, false},
	}
	for _, c := range cases {
		if got := enabledBool(c.in); got != c.want {
			t.Errorf("enabledBool(%v (%T)) = %v, want %v", c.in, c.in, got, c.want)
		}
	}
}

// ---- marshalGrants / unmarshalGrants ----

func TestMarshalGrants_nilAndValues(t *testing.T) {
	b, err := marshalGrants(nil)
	if err != nil {
		t.Fatalf("marshalGrants(nil): %v", err)
	}
	if string(b) != "[]" {
		t.Errorf("marshalGrants(nil) = %s, want []", string(b))
	}
	b, err = marshalGrants([]access.Permission{"a:read", "b:write"})
	if err != nil || string(b) != `["a:read","b:write"]` {
		t.Errorf("marshalGrants(values) = %s err=%v", string(b), err)
	}
}

func TestUnmarshalGrants_emptyAndValues(t *testing.T) {
	if got := unmarshalGrants(""); len(got) != 0 {
		t.Errorf("unmarshalGrants('') = %+v", got)
	}
	if got := unmarshalGrants("not-json"); len(got) != 0 {
		t.Errorf("unmarshalGrants(garbage) = %+v", got)
	}
	got := unmarshalGrants(`["a:read","b:write"]`)
	if len(got) != 2 || got[0] != "a:read" || got[1] != "b:write" {
		t.Errorf("unmarshalGrants(values) = %+v", got)
	}
}

// ---- place ----

func TestPlace_dialectSpecific(t *testing.T) {
	if got := place(migrate.DialectPostgres, 3); got != "$3" {
		t.Errorf("postgres place(3) = %q, want $3", got)
	}
	if got := place(migrate.DialectSQLite, 3); got != "?" {
		t.Errorf("sqlite place(3) = %q, want ?", got)
	}
}

// ---- boolToInt ----

func TestBoolToInt_mapping(t *testing.T) {
	if boolToInt(true) != 1 || boolToInt(false) != 0 {
		t.Error("boolToInt mapping wrong")
	}
}

// ---- timeToMillis ----

func TestTimeToMillis_nilAndValue(t *testing.T) {
	if got := timeToMillis(nil); got != nil {
		t.Errorf("timeToMillis(nil) = %v, want nil", got)
	}
	now := time.UnixMilli(1_700_000_000_000)
	if got := timeToMillis(&now); got != now.UnixMilli() {
		t.Errorf("timeToMillis = %v, want %d", got, now.UnixMilli())
	}
}

// ---- SetMigrationsAppliedAt (set + clear) ----

func TestSetMigrationsAppliedAt_setAndClear(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.Install(ctx, DesiredState{Module: "demo", ArtifactSHA256: hex256("aa")}); err != nil {
		t.Fatalf("install: %v", err)
	}
	stamp := time.UnixMilli(1_700_000_000_000)
	if err := store.SetMigrationsAppliedAt(ctx, "demo", &stamp); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := store.GetDesired(ctx, "demo")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.MigrationsAppliedAt == nil || !got.MigrationsAppliedAt.Equal(stamp) {
		t.Errorf("after set MigrationsAppliedAt = %v, want %v", got.MigrationsAppliedAt, stamp)
	}
	// Clear.
	if err := store.SetMigrationsAppliedAt(ctx, "demo", nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got, _ = store.GetDesired(ctx, "demo")
	if got.MigrationsAppliedAt != nil {
		t.Errorf("after clear MigrationsAppliedAt = %v, want nil", got.MigrationsAppliedAt)
	}
}

// ---- scanDesired round-trips grants + enabled (covers the SQLite scan path) ----

func TestScanDesired_roundTripsEnabledAndGrants(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.Install(ctx, DesiredState{
		Module:          "demo",
		ArtifactSHA256:  hex256("aa"),
		Enabled:         true,
		EffectiveGrants: []access.Permission{"a:read"},
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	got, err := store.GetDesired(ctx, "demo")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.Enabled {
		t.Error("Enabled did not round-trip")
	}
	if len(got.EffectiveGrants) != 1 || got.EffectiveGrants[0] != "a:read" {
		t.Errorf("EffectiveGrants = %+v", got.EffectiveGrants)
	}
}

// ---- AssertRow with real *sql.Result (covers the success path via SetEnabled) ----

func TestSetEnabled_unknownModuleErrors(t *testing.T) {
	store := newTestStore(t)
	if err := store.SetEnabled(context.Background(), "ghost", true); !errors.Is(err, ErrNoDesiredRow) {
		t.Errorf("SetEnabled(unknown) = %v, want ErrNoDesiredRow", err)
	}
}

// keep sql reference used (fakeSQLResult satisfies sql.Result).
var _ sql.Result = fakeSQLResult{}

// ---- RecordHeartbeat empty-name error ----

func TestRecordHeartbeat_emptyNameErrors(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.RecordHeartbeat(ctx, "", "rep1", 1, "Ready"); err == nil {
		t.Error("RecordHeartbeat with empty module must error")
	}
	if err := store.RecordHeartbeat(ctx, "demo", "", 1, "Ready"); err == nil {
		t.Error("RecordHeartbeat with empty replicaID must error")
	}
}

// ---- SetMigrationsAppliedAt unknown-module error (AssertRow zero-rows) ----

func TestSetMigrationsAppliedAt_unknownModuleErrors(t *testing.T) {
	store := newTestStore(t)
	if err := store.SetMigrationsAppliedAt(context.Background(), "ghost", nil); !errors.Is(err, ErrNoDesiredRow) {
		t.Errorf("SetMigrationsAppliedAt(unknown) = %v, want ErrNoDesiredRow", err)
	}
}
