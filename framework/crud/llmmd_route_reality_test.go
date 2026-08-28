package crud

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/core/schema"
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
	crudMounted := func(e *entity.Entity) bool { return e.GetTable() == "posts" }

	md := RegistryLLMMD(reg, "MyApp", crudMounted)

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
	legacy := RegistryLLMMD(reg, "MyApp", nil)
	if !strings.Contains(legacy, "secrets") {
		t.Errorf("nil predicate must keep the pre-#266 behavior:\n%s", legacy)
	}
}
