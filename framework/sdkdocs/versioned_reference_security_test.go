package sdkdocs

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The reference pages document the live registry, including entities
// registered via App.GroupEntity (they arrive with Version set to the
// group's full prefix and their routes mounted there). These tests pin
// that a versioned entity's documented paths are the paths the router
// actually serves, and that every registered version is individually
// reachable.

func versionedPosts(version string, fields ...schema.Field) *entity.Entity {
	e := entity.Define("posts", entity.EntityConfig{
		Table:    "posts",
		Exposure: &entity.ExposureConfig{Public: true},
		Fields:   fields,
	})
	e.Version = version
	return e
}

// Property: every path the reference page tells a client to call exists
// on the server. entityScreen renders cfg.APIPrefix+"/"+cfg.Table and
// builds the endpoints table and the curl example the same way, ignoring
// entity.Version entirely — a group-mounted entity (routes at /api/v1/)
// is documented at its unversioned path (/posts), so every URL a reader
// copies from the page 404s. Surfaces asserted: the "Base path" line, the
// endpoints table's auto rows, and the curl quickstart example.
func TestVersionedEntityDocumentsRealPath(t *testing.T) {
	reg := &fakeRegistry{entities: []*entity.Entity{
		versionedPosts("/api/v1", schema.Field{Name: "title", Type: schema.String, Required: true}),
	}}
	srv := mountedServer(t, Config{Registry: reg})
	resp, body := get(t, srv, "/docs/api/entities/posts")
	if resp.StatusCode != 200 {
		t.Fatalf("entity page status = %d", resp.StatusCode)
	}

	if !strings.Contains(body, "/api/v1/posts") {
		t.Errorf("reference page for the /api/v1-mounted entity never documents its real base path; every documented URL 404s")
	}
	if !strings.Contains(body, "/api/v1/posts/{id}") {
		t.Errorf("endpoints table documents the unversioned record path for a versioned entity")
	}
	if !strings.Contains(body, "/api/v1/posts?") {
		t.Errorf("curl example addresses the unversioned path for a versioned entity")
	}
}

// Property: each documented entity is individually reachable at some
// reference URL. lookup() resolves a URL segment against table or name
// only, so when the same entity name is registered under two versions
// (the exact shape App.GroupEntity exists for), the two entities share one
// URL and the second version's schema is unreachable — its fields are
// silently replaced by the first version's on the shared page.
func TestSecondVersionShadowedInDocs(t *testing.T) {
	reg := &fakeRegistry{entities: []*entity.Entity{
		versionedPosts("/api/v1", schema.Field{Name: "title", Type: schema.String, Required: true}),
		versionedPosts("/api/v2", schema.Field{Name: "headline", Type: schema.String, Required: true}),
	}}
	srv := mountedServer(t, Config{Registry: reg})

	resp, body := get(t, srv, "/docs/api/entities/posts")
	if resp.StatusCode != 200 {
		t.Fatalf("entity page status = %d", resp.StatusCode)
	}
	if !strings.Contains(body, "headline") {
		t.Errorf("the /api/v2 version of posts is unreachable: no reference URL documents its schema (the shared /entities/posts page shows only the first version's fields)")
	}
}
