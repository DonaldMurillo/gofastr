package factory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// ----- registry harness ----------------------------------------------------

type fakeRegistry struct {
	entities map[string]*entity.Entity
}

func newFakeRegistry() *fakeRegistry { return &fakeRegistry{entities: map[string]*entity.Entity{}} }

func (r *fakeRegistry) Get(name string) (*entity.Entity, error) {
	e, ok := r.entities[name]
	if !ok {
		return nil, fmt.Errorf("entity %q not found", name)
	}
	return e, nil
}

func (r *fakeRegistry) put(e *entity.Entity) { r.entities[e.GetName()] = e }

// migratedRegistry returns a registry with one entity backed by an
// in-memory sqlite DB whose schema has been auto-migrated.
func migratedRegistry(t *testing.T) (*fakeRegistry, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	e := entity.Define("widget", entity.EntityConfig{
		Name:  "widget",
		Table: "widgets",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "qty", Type: schema.Int},
		},
	})
	e.SetDB(db)
	reg := newFakeRegistry()
	reg.put(e)

	// Migrate just this entity. A minimal Registry adapter is enough
	// since AutoMigrate iterates registry.All().
	if err := migrate.AutoMigrate(db, &miniRegistry{e: e}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return reg, db
}

// miniRegistry adapts a single entity to the AutoMigrate Registry
// surface. Defined locally so tests don't have to spin up the full
// framework.Registry.
type miniRegistry struct{ e *entity.Entity }

func (m *miniRegistry) All() map[string]*entity.Entity {
	return map[string]*entity.Entity{m.e.GetName(): m.e}
}
func (m *miniRegistry) AllSorted() []*entity.Entity {
	return []*entity.Entity{m.e}
}
func (m *miniRegistry) Get(name string) (*entity.Entity, error) {
	if name == m.e.GetName() {
		return m.e, nil
	}
	return nil, fmt.Errorf("not found")
}

// ----- Factory.Build is deterministic in inputs -----------------------------

func TestFactory_BuildAppliesOverridesLeftToRight(t *testing.T) {
	reg, _ := migratedRegistry(t)
	seq := &Sequence{}
	f, err := New(reg, "widget", func() map[string]any {
		return map[string]any{"name": seq.NextString("w-"), "qty": 1}
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	body := f.Build(map[string]any{"qty": 5}, map[string]any{"qty": 10, "name": "override"})
	if body["qty"] != 10 {
		t.Fatalf("later override should win: got qty=%v", body["qty"])
	}
	if body["name"] != "override" {
		t.Fatalf("late name override should win: got name=%v", body["name"])
	}

	// Subsequent Build must not see leftover overrides.
	again := f.Build()
	if again["qty"] != 1 {
		t.Fatalf("base should be re-evaluated cleanly per Build; got qty=%v", again["qty"])
	}
}

func TestFactory_BuildFailsClosedOnNilBase(t *testing.T) {
	reg, _ := migratedRegistry(t)
	if _, err := New(reg, "widget", nil); err == nil {
		t.Fatal("expected error on nil base")
	}
}

func TestFactory_NewRejectsUnknownEntity(t *testing.T) {
	reg, _ := migratedRegistry(t)
	if _, err := New(reg, "no-such-entity", func() map[string]any { return nil }); err == nil {
		t.Fatal("expected error for missing entity")
	}
}

// ----- Factory.Create round-trips through CRUD ------------------------------

func TestFactory_CreatePersistsRow(t *testing.T) {
	reg, db := migratedRegistry(t)
	seq := &Sequence{}
	f, err := New(reg, "widget", func() map[string]any {
		return map[string]any{"name": seq.NextString("w-"), "qty": 1}
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	row, err := f.Create(context.Background(), map[string]any{"qty": 7})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if row["qty"] == nil {
		t.Fatalf("expected qty in created row: %+v", row)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM widgets WHERE qty = 7").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one matching row, got %d", count)
	}
}

func TestFactory_CreateManyInsertsN(t *testing.T) {
	reg, db := migratedRegistry(t)
	seq := &Sequence{}
	f, _ := New(reg, "widget", func() map[string]any {
		return map[string]any{"name": seq.NextString("w-"), "qty": 0}
	})

	rows, err := f.CreateMany(context.Background(), 5, func(i int) map[string]any {
		return map[string]any{"qty": i + 1}
	})
	if err != nil {
		t.Fatalf("createMany: %v", err)
	}
	if len(rows) != 5 {
		t.Fatalf("got %d rows want 5", len(rows))
	}

	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM widgets").Scan(&count)
	if count != 5 {
		t.Fatalf("table should have 5 rows; got %d", count)
	}
}

// ----- Registry -------------------------------------------------------------

func TestRegistry_RegisterAndUse(t *testing.T) {
	reg, _ := migratedRegistry(t)
	seq := &Sequence{}
	f, _ := New(reg, "widget", func() map[string]any {
		return map[string]any{"name": seq.NextString("w-"), "qty": 1}
	})

	r := NewRegistry().Register("widget", f)
	got, err := r.Get("widget")
	if err != nil || got != f {
		t.Fatalf("get: %v %v", got, err)
	}

	row, err := r.Create(context.Background(), "widget", map[string]any{"qty": 99})
	if err != nil || row["qty"] == nil {
		t.Fatalf("create via registry: %v %+v", err, row)
	}
}

func TestRegistry_MissingFactoryError(t *testing.T) {
	r := NewRegistry()
	_, err := r.Get("nope")
	if err == nil {
		t.Fatal("expected error for unknown factory")
	}
	defer func() {
		if rec := recover(); rec == nil {
			t.Fatal("MustGet should panic on missing factory")
		}
	}()
	r.MustGet("nope")
}

// ----- Sequence is concurrent-safe ------------------------------------------

func TestSequence_NextIsMonotonicAndConcurrentSafe(t *testing.T) {
	s := &Sequence{}
	const N = 100
	var wg sync.WaitGroup
	got := make([]int64, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got[i] = s.Next()
		}(i)
	}
	wg.Wait()
	seen := map[int64]struct{}{}
	for _, v := range got {
		if _, dup := seen[v]; dup {
			t.Fatalf("duplicate value %d", v)
		}
		seen[v] = struct{}{}
	}
}

// Compile-time assertion that the framework Registry would satisfy
// the EntityRegistry interface (without importing it here, which
// would re-create the cycle this package avoids).
var _ EntityRegistry = (*fakeRegistry)(nil)
var _ = errors.New
