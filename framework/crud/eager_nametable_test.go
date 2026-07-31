package crud

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestEagerLoadResolvesNameNotTable is the oracle for the exported
// EagerLoad helper when an entity's registry Name differs from its Table.
// Relation.Entity is the registry key (the entity NAME); the rows live in
// the entity's TABLE. EagerLoad used rel.Entity as the SQL table name, so
// a Name!=Table target produced `SELECT * FROM "<Name>"` against a table
// that does not exist and the relation silently failed. The live include
// path (loadIncludeNode) resolves via node.Target.GetTable() and is
// already pinned by TestIncludeResolvesByNameNotTable; the lower-level
// EagerLoad API must match it.
//
// The star's planets live in table "planets" but the relation targets the
// registered NAME "kepler_planet". A correct resolve hits "planets"; the
// bug hits "kepler_planet" (no such table) and errors.
func TestEagerLoadResolvesNameNotTable(t *testing.T) {
	ctx := context.Background()
	db := setupDB(t,
		`CREATE TABLE stars (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE planets (id TEXT PRIMARY KEY, star_id TEXT, name TEXT)`,
	)

	seedRows(t, db, "stars", []map[string]any{
		{"id": "s1", "name": "Sol"},
	})
	seedRows(t, db, "planets", []map[string]any{
		{"id": "p1", "star_id": "s1", "name": "Earth"},
	})

	// Target Name != Table: registered as "kepler_planet", rows in "planets".
	planetsEnt := entity.Define("kepler_planet", entity.EntityConfig{
		Name:  "kepler_planet",
		Table: "planets",
		Fields: []schema.Field{
			{Name: "star_id", Type: schema.String},
			{Name: "name", Type: schema.String},
		},
	})
	starsEnt := entity.Define("stars", entity.EntityConfig{
		Name:  "stars",
		Table: "stars",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String},
		},
		Relations: []entity.Relation{
			// Entity names the registered NAME; the table is "planets".
			entity.HasMany("planets", "kepler_planet", "star_id"),
		},
	})

	reg := stubRegistry{byName: map[string]*entity.Entity{
		"kepler_planet": planetsEnt,
		"stars":         starsEnt,
	}}

	got, err := EagerLoad(ctx, db, starsEnt, starsEnt.Config.Relations, []string{"s1"}, reg)
	if err != nil {
		t.Fatalf("EagerLoad on a Name!=Table target errored (it resolved the NAME as the table): %v", err)
	}

	planets, ok := got["s1"]["planets"].([]map[string]any)
	if !ok || len(planets) != 1 {
		t.Fatalf("EagerLoad did not load the relation through the entity's Table; got[%q]=%v (type %T)", "s1", got["s1"]["planets"], got["s1"]["planets"])
	}
	if name, _ := planets[0]["name"].(string); name != "Earth" {
		t.Errorf("loaded wrong planet row: got %v, want name=Earth", planets[0])
	}
}
