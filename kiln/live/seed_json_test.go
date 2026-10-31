package live_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// insertSeed bound a json field's decoded object or array as-is, and
// database/sql refuses a map or a non-byte slice, so the seed failed
// where a CRUD create of the same row succeeds (2026-10-27 K21). The
// value is now encoded as JSON text, as CRUD does, and reads back as
// the value seeded; a string is JSON text already and is stored as is.
func TestSeedJSONFieldInserts(t *testing.T) {
	l, db := newTestLive(t, journal.NewMemory())
	tools := protocol.New(l)
	posts := &world.Entity{Name: "posts", Fields: []world.Field{
		{Name: "title", Type: "string"},
		{Name: "meta", Type: "json"},
		{Name: "tags", Type: "json"},
		{Name: "raw", Type: "json"},
	}}
	if res := tools.AddEntity(t.Context(), protocol.AddEntityArgs{Entity: posts}); !res.OK {
		t.Fatalf("add_entity: %+v", res)
	}
	res := tools.AddSeed(t.Context(), protocol.AddSeedArgs{Seed: &world.Seed{Entity: "posts", Rows: []map[string]any{{
		"title": "hello",
		"meta":  map[string]any{"a": float64(1)},
		"tags":  []any{"x", "y"},
		"raw":   `{"b":2}`,
	}}}})
	if !res.OK {
		t.Fatalf("add_seed: %+v", res)
	}
	var id, meta, tags, raw string
	if err := db.QueryRow(`SELECT id, meta, tags, raw FROM posts`).Scan(&id, &meta, &tags, &raw); err != nil {
		t.Fatal(err)
	}
	if meta != `{"a":1}` || tags != `["x","y"]` || raw != `{"b":2}` {
		t.Fatalf("stored meta=%s tags=%s raw=%s", meta, tags, raw)
	}
	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/posts/"+id, nil))
	var got struct {
		Data struct {
			Meta map[string]any `json:"meta"`
			Tags []string       `json:"tags"`
		} `json:"data"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil || got.Data.Meta["a"] != float64(1) || len(got.Data.Tags) != 2 {
		t.Fatalf("GET = %d %s", rec.Code, rec.Body)
	}
}
