package migrate

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// TestRunSeeds_PicksUpSeedFromNonRepresentative (F11): a Seed declared only on
// a NON-representative version IS run. RunSeeds iterates the version union
// (UnionEntities), which propagates the sole seed into the merged entity
// regardless of which version is the representative. The old path iterated
// Registry.All() — one representative per name — so a seed only on v2 was
// invisible: hasSeed stayed false, RunSeeds returned success, and the seed
// silently never ran.
func TestRunSeeds_PicksUpSeedFromNonRepresentative(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec("CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)"); err != nil {
		t.Fatalf("create table: %v", err)
	}

	seedRan := false
	v1 := rawEnt("posts", "posts",
		[]schema.Field{{Name: "id", Type: schema.String}, {Name: "title", Type: schema.String}},
		nil, "id")
	v1.Version = "/api/v1" // representative — AllSorted orders "/api/v1" first
	// v1 has NO seed.

	v2 := rawEnt("posts", "posts",
		[]schema.Field{{Name: "id", Type: schema.String}, {Name: "title", Type: schema.String}},
		nil, "id")
	v2.Version = "/api/v2"
	v2.Config.Seed = func(ctx context.Context, db *sql.DB) error {
		seedRan = true
		_, e := db.Exec("INSERT INTO posts (id, title) VALUES ('seeded', 'seed')")
		return e
	}

	// multiVersionRegistry.AllSorted returns both versions; UnionEntities merges
	// them and propagates v2's sole seed into the merged entity.
	reg := multiVersionRegistry{v1, v2}

	if err := RunSeeds(context.Background(), db, reg); err != nil {
		t.Fatalf("RunSeeds: %v", err)
	}
	if !seedRan {
		t.Fatal("seed declared on the non-representative v2 was never run — RunSeeds must iterate the version union")
	}

	var title string
	if err := db.QueryRow("SELECT title FROM posts WHERE id = 'seeded'").Scan(&title); err != nil {
		t.Fatalf("seeded row missing after RunSeeds: %v", err)
	}
	if title != "seed" {
		t.Errorf("seeded title = %q, want %q", title, "seed")
	}
}
