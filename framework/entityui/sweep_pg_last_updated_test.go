package entityui

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

func TestSweepPGLastUpdatedSkipsNullTimestamps(t *testing.T) {
	db := pgtest.DB(t)
	e := entity.Define("notes", entity.EntityConfig{
		Name:   "notes",
		Table:  "notes",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	})
	e.SetDB(db)
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
	reg := &testRegistry{ents: map[string]*entity.Entity{"notes": e}}
	if err := migrate.AutoMigrate(db, reg); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	for _, q := range []string{
		`ALTER TABLE notes ALTER COLUMN updated_at DROP NOT NULL`,
		`INSERT INTO notes (id, title, updated_at) VALUES ('n1', 'dated', '2026-01-02 10:00:00')`,
		`INSERT INTO notes (id, title, updated_at) VALUES ('n2', 'undated', NULL)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	x := &testUI{tb: t, db: db, host: &testHost{reg: reg, db: db, cruds: map[string]*crud.CrudHandler{}, apis: map[string]string{}}}
	ui, err := New(x.host, Extensions{})
	if err != nil {
		t.Fatalf("entityui.New: %v", err)
	}
	got, ok := ui.LastUpdated(x.userCtx("/dash", "", "u1"), "notes")
	if want := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC); !ok || !got.Equal(want) {
		t.Fatalf("last update = %v %v, want %v: Postgres sorts the NULL updated_at first in DESC", got, ok, want)
	}
}
