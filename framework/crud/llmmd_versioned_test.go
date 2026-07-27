package crud

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Sol #20. llm.md is the reference an agent follows to call the API. Only the
// "Resource:" header used the mounted path; every endpoint heading was built
// from the bare table, so /api/v1/posts/llm.md told an agent to call
// `GET /posts` — a 404, or worse, an unrelated unversioned resource.
func TestLLMMD_VersionedEndpointsUseMountedPath(t *testing.T) {
	ent := entity.Define("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "id", Type: schema.Int},
			{Name: "title", Type: schema.String},
		},
	}.WithTimestamps(false))
	ent.Version = "/api/v1"

	md := EntityLLMMD(ent)

	// Every advertised endpoint must carry the version prefix.
	for _, want := range []string{
		"### GET /api/v1/posts",
		"### GET /api/v1/posts/{id}",
		"### POST /api/v1/posts",
		"### PUT /api/v1/posts/{id}",
		"### PATCH /api/v1/posts/{id}",
		"### DELETE /api/v1/posts/{id}",
		"### POST /api/v1/posts/_batch",
		"### GET /api/v1/posts/_events",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("llm.md is missing %q", want)
		}
	}

	// And none may advertise the unmounted bare path. Anchored so
	// "/api/v1/posts" does not match as a false positive.
	bare := regexp.MustCompile(`(?m)^### (GET|POST|PUT|PATCH|DELETE) /posts`)
	if m := bare.FindAllString(md, -1); len(m) > 0 {
		t.Errorf("llm.md advertises %d unmounted bare-table endpoint(s): %v", len(m), m)
	}
}

// The unversioned path is unchanged — bare table is correct there.
func TestLLMMD_UnversionedUnchanged(t *testing.T) {
	ent := entity.Define("posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "id", Type: schema.Int}},
	}.WithTimestamps(false))

	md := EntityLLMMD(ent)
	if !strings.Contains(md, "### GET /posts") {
		t.Error("unversioned llm.md should advertise the bare table path")
	}
}
