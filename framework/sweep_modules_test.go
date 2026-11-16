package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
)

// Batteries init in dependency order, and batteries free to go in any
// order go by name. The names here sort against the dependency order.
func TestSweepModulesBatteryInitFollowsDependencies(t *testing.T) {
	bm := NewBatteryManager()
	var order []string
	add := func(name string, deps ...string) {
		t.Helper()
		b := &depsBattery{name: name, onInit: func() { order = append(order, name) }}
		if err := bm.Register(b, deps...); err != nil {
			t.Fatal(err)
		}
	}
	add("alpha", "mid")
	add("mid", "zeta")
	add("zeta")
	add("omega")
	add("beta")
	if err := bm.InitAll(&App{}); err != nil {
		t.Fatal(err)
	}
	want := []string{"beta", "omega", "zeta", "mid", "alpha"}
	if !slices.Equal(order, want) {
		t.Fatalf("init order = %v, want %v", order, want)
	}
}

// stopPanicBattery panics in OnStop.
type stopPanicBattery struct{ mockBattery }

func (s *stopPanicBattery) OnStop(context.Context) error {
	s.stopped = true
	panic("stop kaboom")
}

// One battery's failing or panicking OnStop does not keep the others from
// stopping, and every failure reaches the returned error.
func TestSweepModulesStopAllStopsEveryBattery(t *testing.T) {
	bm := NewBatteryManager()
	base := &mockBattery{name: "base", stopErr: errors.New("base stop failed")}
	mid := &stopPanicBattery{mockBattery{name: "mid"}}
	top := &mockBattery{name: "top", stopErr: errors.New("top stop failed")}
	for _, r := range []struct {
		b    Battery
		deps []string
	}{{base, nil}, {mid, []string{"base"}}, {top, []string{"mid"}}} {
		if err := bm.Register(r.b, r.deps...); err != nil {
			t.Fatal(err)
		}
	}
	if err := bm.InitAll(&App{}); err != nil {
		t.Fatal(err)
	}
	err := bm.StopAll(context.Background())
	if !base.stopped || !mid.stopped || !top.stopped {
		t.Fatalf("stopped base=%v mid=%v top=%v, want all three", base.stopped, mid.stopped, top.stopped)
	}
	for _, want := range []string{`battery "top" stop failed`, `battery "mid" stop panicked`, `battery "base" stop failed`} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("StopAll error %v lacks %q", err, want)
		}
	}
}

// A second plugin under a name already registered is refused.
func TestSweepModulesPluginDuplicateNameRefused(t *testing.T) {
	pm := NewPluginManager()
	if err := pm.Register(&countingPlugin{name: "p"}); err != nil {
		t.Fatal(err)
	}
	err := pm.Register(&countingPlugin{name: "p"})
	if err == nil || !strings.Contains(err.Error(), `plugin "p" already registered`) {
		t.Fatalf("duplicate Register = %v, want an already-registered error", err)
	}
	if got := pm.Names(); !slices.Equal(got, []string{"p"}) {
		t.Fatalf("Names = %v, want [p]", got)
	}
}

// A process module with no supervisor to toggle it reads as disabled, in
// the gate and in List.
func TestSweepModulesProcessModuleWithoutTogglerIsClosed(t *testing.T) {
	mm := NewModuleManager(nil, nil)
	mm.register("proc", ModuleManifest{})
	mm.markProcess("proc")
	if err := mm.loadFromStore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if mm.Enabled("proc") {
		t.Fatal("a process module with no toggler reads as enabled")
	}
	if l := mm.List(); len(l) != 1 || l[0].Enabled {
		t.Fatalf("List = %+v, want proc disabled", l)
	}
	if err := mm.Enable(context.Background(), "proc"); err == nil || !strings.Contains(err.Error(), "no supervisor") {
		t.Fatalf("Enable = %v, want a no-supervisor error", err)
	}
}

// The toggle audit callback runs only for a toggle that took effect.
func TestSweepModulesRefusedToggleWritesNoAudit(t *testing.T) {
	ctx := context.Background()
	mm := NewModuleManager(nil, nil)
	mm.register("base", ModuleManifest{})
	mm.register("top", ModuleManifest{DependsOn: []string{"base"}})
	if err := mm.loadFromStore(ctx); err != nil {
		t.Fatal(err)
	}
	var audited []string
	mm.auditToggle = func(_ context.Context, name string, on bool) {
		audited = append(audited, fmt.Sprintf("%s=%v", name, on))
	}
	if err := mm.Disable(ctx, "base"); err == nil {
		t.Fatal("disabling a dependency of an enabled module succeeded")
	}
	if err := mm.Enable(ctx, "missing"); err == nil {
		t.Fatal("enabling an unregistered module succeeded")
	}
	if len(audited) != 0 {
		t.Fatalf("refused toggles were audited: %v", audited)
	}
	if err := mm.Disable(ctx, "top"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(audited, []string{"top=false"}) {
		t.Fatalf("audited = %v, want [top=false]", audited)
	}
}

// sweepRegister registers name at version on reg and returns the error.
func sweepRegister(reg *Registry, name, version string, cfg entity.EntityConfig) error {
	ent := entity.Define(name, cfg.WithTimestamps(false))
	ent.Version = version
	return reg.Register(ent)
}

// At most one version of an entity declares a Seed.
func TestSweepModulesSeedInTwoVersionsRefused(t *testing.T) {
	seed := func(context.Context, *sql.DB) error { return nil }
	reg := NewRegistry()
	cfg := entity.EntityConfig{Table: "posts", Fields: []schema.Field{{Name: "title", Type: schema.String}}}
	cfg.Seed = seed
	if err := sweepRegister(reg, "posts", "/v1", cfg); err != nil {
		t.Fatal(err)
	}
	plain := cfg
	plain.Seed = nil
	if err := sweepRegister(reg, "posts", "/v2", plain); err != nil {
		t.Fatalf("a version without a Seed was refused: %v", err)
	}
	err := sweepRegister(reg, "posts", "/v3", cfg)
	if err == nil || !strings.Contains(err.Error(), "declares a Seed in multiple versions") {
		t.Fatalf("second Seed = %v, want a multiple-seed refusal", err)
	}
}

// An unmanaged declaration over a managed entity's table is not held to
// its columns: migrate emits no DDL for it.
func TestSweepModulesUnmanagedSkipsColumnCheck(t *testing.T) {
	reg := NewRegistry()
	if err := sweepRegister(reg, "posts", "", entity.EntityConfig{
		Table: "posts", Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := sweepRegister(reg, "post_view", "", entity.EntityConfig{
		Table: "posts", Unmanaged: true, Fields: []schema.Field{{Name: "title", Type: schema.Int}},
	}); err != nil {
		t.Fatalf("an unmanaged read model over the table was refused: %v", err)
	}
	err := sweepRegister(reg, "post_copy", "", entity.EntityConfig{
		Table: "posts", Fields: []schema.Field{{Name: "title", Type: schema.Int}},
	})
	if err == nil || !strings.Contains(err.Error(), "incompatible schema") {
		t.Fatalf("a managed conflicting declaration = %v, want an incompatible-schema refusal", err)
	}
}

// Two versions on one table declare the same primary key, columns in the
// same order.
func TestSweepModulesVersionsAgreeOnPrimaryKey(t *testing.T) {
	fields := []schema.Field{{Name: "store", Type: schema.String, Required: true}, {Name: "no", Type: schema.String, Required: true}}
	reg := NewRegistry()
	if err := sweepRegister(reg, "orders", "/v1", entity.EntityConfig{Table: "orders", PrimaryKey: []string{"store", "no"}, Fields: fields}); err != nil {
		t.Fatal(err)
	}
	err := sweepRegister(reg, "orders", "/v2", entity.EntityConfig{Table: "orders", PrimaryKey: []string{"no", "store"}, Fields: fields})
	if err == nil || !strings.Contains(err.Error(), "must declare the same primary key") {
		t.Fatalf("reordered key = %v, want a primary-key refusal", err)
	}
	if err := sweepRegister(reg, "orders", "/v3", entity.EntityConfig{Table: "orders", PrimaryKey: []string{"store", "no"}, Fields: fields}); err != nil {
		t.Fatalf("the same key was refused: %v", err)
	}
}

// Column names compare case-folded: "Title" and "title" are one column.
func TestSweepModulesColumnNamesFoldCase(t *testing.T) {
	reg := NewRegistry()
	if err := sweepRegister(reg, "posts", "/v1", entity.EntityConfig{
		Table: "posts", Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}); err != nil {
		t.Fatal(err)
	}
	err := sweepRegister(reg, "posts", "/v2", entity.EntityConfig{
		Table: "posts", Fields: []schema.Field{{Name: "Title", Type: schema.Int}},
	})
	if err == nil || !strings.Contains(err.Error(), "incompatible schema") {
		t.Fatalf("case-differing column = %v, want an incompatible-schema refusal", err)
	}
}

// ValidateExposure refuses MCP with CRUD off, and nothing else.
func TestSweepModulesValidateExposureRefusesMCPWithoutCRUD(t *testing.T) {
	off, on := false, true
	for _, c := range []struct {
		x    *entity.ExposureConfig
		fail bool
	}{
		{nil, false},
		{&entity.ExposureConfig{MCP: true}, false},
		{&entity.ExposureConfig{MCP: true, CRUD: &on}, false},
		{&entity.ExposureConfig{CRUD: &off}, false},
		{&entity.ExposureConfig{MCP: true, CRUD: &off}, true},
	} {
		err := ValidateExposure("gadgets", EntityConfig{Exposure: c.x})
		if c.fail != (err != nil) {
			t.Errorf("exposure %+v: err = %v, want failure %v", c.x, err, c.fail)
		}
		if c.fail && err != nil && !strings.Contains(err.Error(), `entity "gadgets"`) {
			t.Errorf("refusal does not name the entity: %v", err)
		}
	}
}

// ValidateDisplay answers a declaration entity.Define panics on with an
// error, not a panic.
func TestSweepModulesValidateDisplayReportsDefinePanic(t *testing.T) {
	err := ValidateDisplay("invoices", EntityConfig{
		Fields:       displayCheckFields(),
		SearchFields: []string{"nope"},
		Display:      &entity.DisplayConfig{},
	})
	if err == nil || !strings.Contains(err.Error(), `SearchFields entry "nope"`) {
		t.Fatalf("ValidateDisplay = %v, want the SearchFields refusal", err)
	}
}

// A record's audit trail returns at most 200 rows, whatever limit asks.
func TestSweepModulesAuditTrailCapsLimit(t *testing.T) {
	db := memDB(t)
	app := entityUIAuditApp(t, db, "notes", false)
	t0 := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	for i := range 205 {
		putAuditRow(t, db, fmt.Sprintf("aud_%d", i), "notes", "update", "x", t0.Add(time.Duration(i)*time.Second))
	}
	got, err := entityUIHost{a: app}.Audit().Trail(context.Background(), "notes", "x", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 200 {
		t.Fatalf("trail = %d rows, want the 200 cap", len(got))
	}
}

// App start leaves a job created moments ago alone: its confirm may still
// be about to enqueue it.
func TestSweepModulesStartSkipsFreshUnenqueuedJob(t *testing.T) {
	db := memDB(t)
	ctx := context.Background()
	s, err := newSQLBulkStore(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, bulkStoreJob("fresh", "k1", 1), []string{"a"}); err != nil {
		t.Fatal(err)
	}
	app := entityUIAuditApp(t, db, "notes", false)
	runner := &recordingJobs{}
	app.EntityUI(entityui.Extensions{Jobs: runner})
	if err := app.runStartHooks(); err != nil {
		t.Fatal(err)
	}
	if len(runner.got) != 0 {
		t.Fatalf("start enqueued %v, want nothing inside the grace period", runner.got)
	}
}

// A typed hook's written-back integer lands as int64, the type the untyped
// body carries, not float64.
func TestSweepModulesTypedHookIntegersStayInt64(t *testing.T) {
	type rec struct {
		Meta map[string]any `json:"meta"`
	}
	before := rec{}
	after := rec{Meta: map[string]any{"n": 3}}
	dest := map[string]any{}
	if err := mergeStructIntoMap(&before, &after, dest); err != nil {
		t.Fatal(err)
	}
	meta, _ := dest["meta"].(map[string]any)
	if got, ok := meta["n"].(int64); !ok || got != 3 {
		t.Fatalf("meta.n = %#v (%T), want int64(3)", meta["n"], meta["n"])
	}

	after = rec{Meta: map[string]any{"n": uint64(18446744073709551615)}}
	dest = map[string]any{}
	if err := mergeStructIntoMap(&before, &after, dest); err != nil {
		t.Fatal(err)
	}
	meta, _ = dest["meta"].(map[string]any)
	if got, ok := meta["n"].(json.Number); !ok || got != "18446744073709551615" {
		t.Fatalf("meta.n = %#v (%T), want the json.Number with its digits", meta["n"], meta["n"])
	}
}
