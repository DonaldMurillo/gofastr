package live_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// A seed row with no id went in with a NULL primary key, so the CRUD
// API could not fetch it (2026-10-27 K12). Through the migrated table,
// the row now has an id and GET /api/posts/<id> answers 200.
func TestSeededRowIsFetchable(t *testing.T) {
	l, db := newTestLive(t, journal.NewMemory())
	tools := protocol.New(l)
	posts := &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}}
	if res := tools.AddEntity(t.Context(), protocol.AddEntityArgs{Entity: posts}); !res.OK {
		t.Fatalf("add_entity: %+v", res)
	}
	res := tools.AddSeed(t.Context(), protocol.AddSeedArgs{
		Seed: &world.Seed{Entity: "posts", Rows: []map[string]any{{"title": "hello"}}},
	})
	if !res.OK {
		t.Fatalf("add_seed: %+v", res)
	}
	var id *string
	if err := db.QueryRow(`SELECT id FROM posts`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if id == nil || *id == "" {
		t.Fatal("the seeded row has no id")
	}
	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/posts/"+*id, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/posts/%s = %d, want 200: %s", *id, rec.Code, rec.Body)
	}
}
