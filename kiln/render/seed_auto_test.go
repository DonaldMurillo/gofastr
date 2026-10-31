package render

import (
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// seedIDs applies one users seed and returns every stored id.
func seedIDs(t *testing.T, rows ...map[string]any) []string {
	t.Helper()
	db := openSeedDB(t)
	seed := &world.Seed{Entity: "users", Rows: rows}
	if err := ApplySeeds(db, seedWorld(seed, "")); err != nil {
		t.Fatalf("seed rejected: %v", err)
	}
	res, err := db.Query(`SELECT id FROM users ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Close()
	var ids []string
	for res.Next() {
		var id *string
		if err := res.Scan(&id); err != nil {
			t.Fatal(err)
		}
		if id == nil {
			t.Fatal("a seeded row has a NULL id")
		}
		ids = append(ids, *id)
	}
	if err := res.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
}

// A seed row with no id stored a NULL primary key: the row could not be
// fetched, edited, deleted or referenced (2026-10-27 K12).
func TestSeedRowWithoutIDGetsOne(t *testing.T) {
	ids := seedIDs(t, map[string]any{"name": "ada"}, map[string]any{"name": "bob", "id": nil})
	if len(ids) != 2 {
		t.Fatalf("got %d rows, want 2", len(ids))
	}
	for _, id := range ids {
		if _, err := uuid.Parse(id); err != nil {
			t.Errorf("id %q is not a UUID: %v", id, err)
		}
	}
	if ids[0] == ids[1] {
		t.Errorf("two rows share id %q", ids[0])
	}
}

// An id the agent gives is kept, whatever its case, so other seeds can
// reference it.
func TestSeedKeepsGivenID(t *testing.T) {
	ids := seedIDs(t, map[string]any{"name": "ada", "id": "u-1"}, map[string]any{"name": "bob", "ID": "u-2"})
	if strings.Join(ids, ",") != "u-1,u-2" {
		t.Errorf("ids = %v, want [u-1 u-2]", ids)
	}
}

// Two keys naming one column reached the INSERT twice, and SQLite keeps
// one of them silently.
func TestSeedRefusesFoldedDuplicateKeys(t *testing.T) {
	db := openSeedDB(t)
	seed := &world.Seed{Entity: "users", Rows: []map[string]any{{"name": "ada", "NAME": "eve"}}}
	err := ApplySeeds(db, seedWorld(seed, ""))
	if err == nil {
		t.Fatal("a row naming one column twice was accepted")
	}
	refusedBeforeSQL(t, "folded duplicate keys", err)
}

func TestSeedFillsTimestamps(t *testing.T) {
	old := seedNow
	seedNow = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 500_000_000, time.UTC) }
	t.Cleanup(func() { seedNow = old })
	db := openSeedDB(t)
	seed := &world.Seed{Entity: "users", Rows: []map[string]any{
		{"name": "ada"},
		{"name": "bob", "created_at": "2020-01-02T03:04:05.000000Z"},
	}}
	if err := ApplySeeds(db, seedWorld(seed, "")); err != nil {
		t.Fatal(err)
	}
	res, err := db.Query(`SELECT name, created_at, updated_at FROM users ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Close()
	for res.Next() {
		var name string
		var created, updated *string
		if err := res.Scan(&name, &created, &updated); err != nil {
			t.Fatal(err)
		}
		if created == nil || updated == nil {
			t.Fatalf("%s: a timestamp is NULL", name)
		}
		for _, v := range []string{*created, *updated} {
			if _, err := time.Parse(time.RFC3339Nano, v); err != nil {
				t.Errorf("%s: timestamp %q does not parse: %v", name, v, err)
			}
		}
		if name == "bob" && *created != "2020-01-02T03:04:05.000000Z" {
			t.Errorf("bob: created_at = %q, want the given value", *created)
		}
		// crud's fixed-width microsecond layout: SQLite compares these
		// as strings, so a trimmed "05.5Z" would sort wrong.
		if name == "ada" && *created != "2026-01-02T03:04:05.500000Z" {
			t.Errorf("ada: created_at = %q, want the pinned clock in the stored layout", *created)
		}
	}
	if err := res.Err(); err != nil {
		t.Fatal(err)
	}
}

// With timestamps off the seed adds no timestamp columns.
func TestSeedNoTimestampsWhenOff(t *testing.T) {
	db := openSeedDB(t)
	off := false
	seed := &world.Seed{Entity: "plain", Rows: []map[string]any{{"name": "ada"}}}
	if _, err := db.Exec(`CREATE TABLE plain (id TEXT PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatal(err)
	}
	w := &world.World{
		Entities: map[string]*world.Entity{"plain": {Name: "plain", Timestamps: &off}},
		Seeds:    []*world.Seed{seed},
	}
	if err := ApplySeeds(db, w); err != nil {
		t.Fatalf("seed rejected: %v", err)
	}
}

// An entity Define refuses (it panics) fails the seed instead of the
// process: seeds are agent-authored and ApplySeeds runs on boot.
func TestSeedRefusedEntityIsAnError(t *testing.T) {
	db := openSeedDB(t)
	seed := &world.Seed{Entity: "users", Rows: []map[string]any{{"name": "ada"}}}
	w := &world.World{
		Entities: map[string]*world.Entity{"users": {Name: "users", SearchFields: []string{"nope"}}},
		Seeds:    []*world.Seed{seed},
	}
	err := ApplySeeds(db, w)
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v, want the refusal naming the search field", err)
	}
	refusedBeforeSQL(t, "refused entity", err)
}

// The row handed to the INSERT names each column once. SQLite accepts a
// column twice and keeps one, so the stored row alone cannot show a
// given "ID" sent beside a generated "id".
func TestSeedAutoFieldsKeepOneKey(t *testing.T) {
	idField := []schema.Field{{Name: "id", AutoGenerate: schema.AutoUUID}}
	got := withAutoFields(map[string]any{"ID": "u-2"}, idField)
	if len(got) != 1 || got["ID"] != "u-2" {
		t.Errorf("given ID: row = %v, want only ID=u-2", got)
	}
	got = withAutoFields(map[string]any{"ID": nil}, idField)
	if len(got) != 1 || got["id"] == nil {
		t.Errorf("null ID: row = %v, want only a generated id", got)
	}
}
