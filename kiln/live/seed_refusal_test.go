package live_test

import (
	"database/sql"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// seedFixture adds a posts entity with a unique title and a CRUD row the
// journal never recorded: the data a refused seed must not take with it.
func seedFixture(t *testing.T) (*protocol.Tools, *sql.DB) {
	t.Helper()
	l, db := newTestLive(t, journal.NewMemory())
	tools := protocol.New(l)
	if r := tools.AddEntity(t.Context(), protocol.AddEntityArgs{Entity: &world.Entity{
		Name:   "posts",
		Fields: []world.Field{{Name: "title", Type: "string", Unique: true}, {Name: "body", Type: "string"}},
	}}); !r.OK {
		t.Fatalf("add_entity: %+v", r)
	}
	if _, err := db.Exec(`INSERT INTO posts (id, title) VALUES ('crud-1', 'kept')`); err != nil {
		t.Fatalf("crud row: %v", err)
	}
	return tools, db
}

func postTitles(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT title FROM posts ORDER BY title`)
	if err != nil {
		t.Fatalf("select posts: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// A seed row naming a column the entity does not declare used to reach
// the INSERT, fail there as "internal", and send Apply down the rederive
// path, which drops every table: the CRUD rows already in the live DB
// went with it. Ingestion refuses it now, naming the column.
func TestSeedUnknownColumnIsValidation(t *testing.T) {
	tools, db := seedFixture(t)
	r := tools.AddSeed(t.Context(), protocol.AddSeedArgs{Seed: &world.Seed{
		Entity: "posts", Rows: []map[string]any{{"title": "a", "summary": "x"}},
	}})
	if r.OK || r.Kind != "validation" || !strings.Contains(r.Error, `"summary"`) {
		t.Fatalf("add_seed with an undeclared column = %+v, want a validation error naming it", r)
	}
	if got := postTitles(t, db); !slices.Equal(got, []string{"kept"}) {
		t.Fatalf("posts after a refused seed = %v, want the CRUD row kept", got)
	}
}

// A seed the database refuses part way (the second row repeats a unique
// title) leaves no rows of its own and keeps the CRUD rows: the insert
// runs in one transaction, so a refusal rolls back instead of rederiving.
func TestFailedSeedKeepsLiveRows(t *testing.T) {
	tools, db := seedFixture(t)
	r := tools.AddSeed(t.Context(), protocol.AddSeedArgs{Seed: &world.Seed{
		Entity: "posts", Rows: []map[string]any{{"title": "dup"}, {"title": "dup"}},
	}})
	if r.OK {
		t.Fatalf("add_seed repeating a unique title succeeded: %+v", r)
	}
	if got := postTitles(t, db); !slices.Equal(got, []string{"kept"}) {
		t.Fatalf("posts after a refused seed = %v, want only the CRUD row", got)
	}
}

// Dropping a field takes its column out of the entity's seed rows and
// weights in the same edit. Left behind, every rederive (boot, undo,
// reset) skipped the whole seed on "no such column" and the generator
// refused the frozen spec. A row whose only field goes keeps the id it
// was journaled with, as the live table keeps the row (2026-10-31 S52):
// another seed may name that id. Undo cuts the entry, so replay brings
// the column back.
func TestDropFieldStripsSeedRows(t *testing.T) {
	cases := []struct {
		op   string
		edit func(*protocol.Tools) protocol.Result
	}{
		{"delete_field", func(tools *protocol.Tools) protocol.Result {
			return tools.DeleteField(t.Context(), protocol.DeleteFieldArgs{Entity: "posts", Field: "body", PlanID: "p1"})
		}},
		{"update_entity", func(tools *protocol.Tools) protocol.Result {
			return tools.UpdateEntity(t.Context(), protocol.UpdateEntityArgs{PlanID: "p1", Entity: &world.Entity{
				Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}},
			}})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.op, func(t *testing.T) {
			l, db := newTestLive(t, journal.NewMemory())
			tools := protocol.New(l)
			ctx := t.Context()
			if r := tools.AddEntity(ctx, protocol.AddEntityArgs{Entity: &world.Entity{
				Name:   "posts",
				Fields: []world.Field{{Name: "title", Type: "string"}, {Name: "body", Type: "string"}},
			}}); !r.OK {
				t.Fatalf("add_entity: %+v", r)
			}
			if r := tools.AddSeed(ctx, protocol.AddSeedArgs{Seed: &world.Seed{
				Entity:  "posts",
				Rows:    []map[string]any{{"id": "s1", "title": "a", "body": "b"}, {"body": "only"}},
				Weights: map[string]map[string]int{"body": {"b": 1}, "title": {"a": 1}},
			}}); !r.OK {
				t.Fatalf("add_seed: %+v", r)
			}
			name := map[string]string{"delete_field": "posts.body", "update_entity": "posts"}[tc.op]
			if r := tools.ProposePlan(ctx, protocol.ProposePlanArgs{
				PlanID: "p1", Steps: []string{"drop body"},
				Targets: []journal.PlanTarget{{Op: tc.op, Name: name}},
			}); !r.OK {
				t.Fatalf("propose_plan: %+v", r)
			}
			if r := tools.ApprovePlan(ctx, protocol.ApprovePlanArgs{PlanID: "p1"}); !r.OK {
				t.Fatalf("approve_plan: %+v", r)
			}
			// world_get shares Seed pointers with the session (cloneWorld:
			// seeds are only replaced wholesale), so the edit must not
			// rewrite a seed in place under a snapshot already handed out.
			snap := tools.WorldGet(ctx, protocol.WorldGetArgs{}).Result.(*world.World)
			if r := tc.edit(tools); !r.OK {
				t.Fatalf("%s: %+v", tc.op, r)
			}
			if got := snap.Seeds[0].Rows[0]["body"]; got != "b" {
				t.Fatalf("snapshot taken before %s lost its seed body: %v", tc.op, got)
			}
			seed := func() (rows int, cols, last, weights []string) {
				l.ReadSession(func(s *journal.Session) {
					sd := s.World.Seeds[0]
					rows = len(sd.Rows)
					cols = slices.Sorted(maps.Keys(sd.Rows[0]))
					last = slices.Sorted(maps.Keys(sd.Rows[len(sd.Rows)-1]))
					weights = slices.Sorted(maps.Keys(sd.Weights))
				})
				return rows, cols, last, weights
			}
			if rows, cols, last, weights := seed(); rows != 2 || !slices.Equal(cols, []string{"id", "title"}) || !slices.Equal(last, []string{"id"}) || !slices.Equal(weights, []string{"title"}) {
				t.Fatalf("seed after %s: %d rows, columns %v and %v, weights %v; want a row of id and title, a row of its id, weights on title", tc.op, rows, cols, last, weights)
			}
			if err := l.Reload(); err != nil {
				t.Fatalf("reload: %v", err)
			}
			var titled, all int
			if err := db.QueryRow(`SELECT count(*), count(title) FROM posts`).Scan(&all, &titled); err != nil || all != 2 || titled != 1 {
				t.Fatalf("posts after rederive: %d rows, %d titled (%v); want both seed rows, one titled", all, titled, err)
			}
			if r := tools.Undo(ctx, protocol.UndoArgs{}); !r.OK {
				t.Fatalf("undo: %+v", r)
			}
			if rows, cols, last, weights := seed(); rows != 2 || !slices.Equal(cols, []string{"body", "id", "title"}) || !slices.Equal(last, []string{"body", "id"}) || !slices.Equal(weights, []string{"body", "title"}) {
				t.Fatalf("seed after undo: %d rows, columns %v and %v, weights %v; want both rows back with body", rows, cols, last, weights)
			}
		})
	}
}
