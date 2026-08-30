package sdkdocs

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Pins the #266 invariant on the SDK docs site, the advertising surface
// that never received the route predicate #266 threaded into openapi.json
// and /api/llm.md.
//
// Exposure.CRUD alone cannot answer "were this entity's routes mounted".
// App mounts auto-CRUD only when a DB is attached, so a DB-less app —
// screens plus hand-written endpoints, no persistence — registers no CRUD
// routes at all while every entity still reads Exposure.CRUD == nil
// ("auto"). Documenting those entities ships a reference page, and an SDK
// download, for an API that answers 404 on every path.
func TestIncludedEntitiesHonorsRouteReality(t *testing.T) {
	reg := &fakeRegistry{entities: []*entity.Entity{
		entity.Define("posts", entity.EntityConfig{
			Exposure: &entity.ExposureConfig{Public: true},
			Fields:   []schema.Field{{Name: "title", Type: schema.String}},
		}),
		entity.Define("drafts", entity.EntityConfig{
			Exposure: &entity.ExposureConfig{Public: true},
			Fields:   []schema.Field{{Name: "title", Type: schema.String}},
		}),
	}}

	// Only posts got routes; drafts did not (the no-DB / unmounted case).
	s := &site{cfg: Config{
		Registry:    reg,
		BasePath:    "/docs/api",
		CRUDMounted: func(e *entity.Entity) bool { return e.GetTable() == "posts" },
	}}

	var names []string
	for _, e := range s.includedEntities() {
		names = append(names, e.GetTable())
	}
	if len(names) != 1 || names[0] != "posts" {
		t.Fatalf("includedEntities = %v, want [posts]: an entity whose CRUD routes were never mounted must not be documented", names)
	}

	// The reference-page path set is built from the same predicate, so a
	// 404-only entity must not get a screen either.
	sc := &entityScreen{site: s}
	paths := sc.StaticPaths(context.Background())
	if len(paths) != 1 || paths[0]["name"] != "posts" {
		t.Fatalf("StaticPaths = %+v, want only posts", paths)
	}
}
