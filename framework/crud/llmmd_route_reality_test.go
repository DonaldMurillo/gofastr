package crud

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Pins #266 for /api/llm.md: the index must describe routes that exist.
// With a route predicate, a CRUD-less entity lists only its declared
// custom endpoints (unlinked: its per-entity llm.md route rides the
// CRUD mount), and an entity with no routes at all is omitted.
func TestRegistryLLMMD_RouteReality(t *testing.T) {
	reg := stubRegistry{byName: map[string]*entity.Entity{
		"posts": covRelEntity(), // CRUD mounted, 2 custom endpoints
		"jobs": entity.Define("jobs", entity.EntityConfig{
			Name: "jobs", Table: "jobs",
			Fields: []schema.Field{{Name: "n", Type: schema.String}},
			Endpoints: []entity.Endpoint{
				{Method: "POST", Path: "/jobs/retry", Description: "retry"},
			},
		}.WithTimestamps(false)), // CRUD off: custom endpoint only
		"secrets": entity.Define("secrets", entity.EntityConfig{
			Name: "secrets", Table: "secrets",
			Fields: []schema.Field{{Name: "n", Type: schema.String}},
		}.WithTimestamps(false)), // CRUD off, no endpoints: no routes at all
	}}
	crudMount := func(e *entity.Entity) MountInfo { return MountInfo{Mounted: e.GetTable() == "posts"} }

	md := RegistryLLMMD(reg, "MyApp", crudMount, "")

	if !strings.Contains(md, "[posts](/posts/llm.md)") {
		t.Errorf("CRUD-mounted entity should keep its linked row:\n%s", md)
	}
	if !strings.Contains(md, "| jobs | `/jobs` | 1 |") {
		t.Errorf("CRUD-less entity should list only its 1 custom endpoint, unlinked:\n%s", md)
	}
	if strings.Contains(md, "jobs/llm.md") {
		t.Errorf("CRUD-less entity must not link a per-entity llm.md that 404s:\n%s", md)
	}
	if strings.Contains(md, "secrets") {
		t.Errorf("entity with no routes must be omitted from the index:\n%s", md)
	}

	// nil predicate keeps the historical Exposure-blind behavior for
	// direct callers.
	legacy := RegistryLLMMD(reg, "MyApp", nil, "")
	if !strings.Contains(legacy, "secrets") {
		t.Errorf("nil predicate must keep the pre-#266 behavior:\n%s", legacy)
	}
}

// llm.md told an agent the cursor response carries "total"; the cursor
// route never sends one (serveCursorList skips the COUNT), so an agent
// following the doc read a missing field (2026-11-01 bug bash). Every key
// each documented list envelope names must be in what the route answers.
func TestLLMMDListEnvelopesMatchRoute(t *testing.T) {
	db := setupDB(t, `CREATE TABLE items (id TEXT PRIMARY KEY, title TEXT)`)
	ent := entity.Define("items", entity.EntityConfig{
		Table:    "items",
		Fields:   []schema.Field{{Name: "title", Type: schema.String}},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
	ent.SetDB(db)
	seedRows(t, db, "items", []map[string]any{{"id": "i1", "title": "a"}, {"id": "i2", "title": "b"}})
	ch := NewCrudHandler(ent, db)
	doc := EntityLLMMD(ent)
	keyRE := regexp.MustCompile(`"(\w+)":`)
	for _, c := range []struct{ heading, query string }{
		{"**Offset response:**", "/items?page=1&limit=1"},
		{"**Cursor response**", "/items?cursor=&limit=1"},
	} {
		i := strings.Index(doc, c.heading)
		if i < 0 {
			t.Fatalf("llm.md has no %s section", c.heading)
		}
		section := doc[i:]
		if j := strings.Index(section, "```\n\n"); j > 0 {
			section = section[:j]
		}
		rec := httptest.NewRecorder()
		ch.List()(rec, httptest.NewRequest(http.MethodGet, c.query, nil))
		var got map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("%s: %v (%s)", c.query, err, rec.Body.String())
		}
		keys := keyRE.FindAllStringSubmatch(section, -1)
		if len(keys) == 0 {
			t.Fatalf("llm.md %s names no keys:\n%s", c.heading, section)
		}
		for _, m := range keys {
			if _, ok := got[m[1]]; !ok {
				t.Errorf("llm.md %s documents %q; GET %s answers %s", c.heading, m[1], c.query, rec.Body.String())
			}
		}
	}
}
