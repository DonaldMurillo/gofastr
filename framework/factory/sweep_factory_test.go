package factory

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestSweepNewRefusesNilRegistryAndDBlessEntity(t *testing.T) {
	base := func() map[string]any { return map[string]any{} }
	if _, err := New(nil, "widget", base); err == nil {
		t.Error("New accepted a nil registry")
	}
	reg := newFakeRegistry()
	reg.put(entity.Define("loose", entity.EntityConfig{Name: "loose", Fields: []schema.Field{{Name: "n", Type: schema.String}}}))
	if _, err := New(reg, "loose", base); err == nil {
		t.Error("New accepted an entity with no DB")
	}
}

func TestSweepWithHandlerReplacesTheDefault(t *testing.T) {
	reg, db := migratedRegistry(t)
	e, _ := reg.Get("widget")
	h := crud.NewCrudHandler(e, db)
	f, err := New(reg, "widget", func() map[string]any { return nil }, WithHandler(h))
	if err != nil {
		t.Fatal(err)
	}
	if f.handler != h {
		t.Error("WithHandler did not replace the default handler")
	}
}

func TestSweepBuildToleratesANilBase(t *testing.T) {
	reg, _ := migratedRegistry(t)
	f, err := New(reg, "widget", func() map[string]any { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Build(map[string]any{"qty": 2}); got["qty"] != 2 {
		t.Errorf("Build over a nil base = %v, want the override", got)
	}
}

func TestSweepCreateManyMergesPerIndex(t *testing.T) {
	reg, db := migratedRegistry(t)
	f, _ := New(reg, "widget", func() map[string]any { return map[string]any{"name": "w", "qty": 0} })
	if _, err := f.CreateMany(context.Background(), 3, func(i int) map[string]any { return map[string]any{"qty": i + 1} }); err != nil {
		t.Fatal(err)
	}
	var sum int
	if err := db.QueryRow("SELECT SUM(qty) FROM widgets").Scan(&sum); err != nil || sum != 6 {
		t.Errorf("sum of qty = %d (%v), want 6 from the per-index overrides", sum, err)
	}
}

func TestSweepCreateManyOfNothingWritesNothing(t *testing.T) {
	reg, db := migratedRegistry(t)
	f, _ := New(reg, "widget", func() map[string]any { return map[string]any{"name": "w"} })
	rows, err := f.CreateMany(context.Background(), 0, nil)
	if rows != nil || err != nil {
		t.Fatalf("CreateMany(0) = %v, %v; want nil, nil", rows, err)
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM widgets").Scan(&n); err != nil || n != 0 {
		t.Errorf("CreateMany(0) wrote %d rows (%v)", n, err)
	}
}
