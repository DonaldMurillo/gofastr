package live_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// A seed names its entity, and the rows belong in that entity's table:
// the declared Table, else the framework's default (snake_case of the
// name). insertSeed used to INSERT INTO the entity name itself, so an
// entity whose table differs from its name answered add_seed with an
// internal error ("no such table: post").
func TestSeedWritesEntityTable(t *testing.T) {
	cases := []struct {
		name   string
		entity *world.Entity
		table  string
	}{
		{"declared table", &world.Entity{Name: "post", Table: "posts"}, "posts"},
		{"default snake table", &world.Entity{Name: "blogPosts"}, "blog_posts"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, db := newTestLive(t, journal.NewMemory())
			tools := protocol.New(l)
			tc.entity.Fields = []world.Field{{Name: "title", Type: "string"}}
			if res := tools.AddEntity(t.Context(), protocol.AddEntityArgs{Entity: tc.entity}); !res.OK {
				t.Fatalf("add_entity: %+v", res)
			}
			res := tools.AddSeed(t.Context(), protocol.AddSeedArgs{
				Seed: &world.Seed{Entity: tc.entity.Name, Rows: []map[string]any{{"title": "hello"}}},
			})
			if !res.OK {
				t.Fatalf("add_seed for %s (table %s): %+v", tc.entity.Name, tc.table, res)
			}
			var n int
			if err := db.QueryRow(`SELECT count(*) FROM ` + tc.table).Scan(&n); err != nil {
				t.Fatalf("count %s: %v", tc.table, err)
			}
			if n != 1 {
				t.Errorf("%s has %d rows, want 1", tc.table, n)
			}
		})
	}
}
