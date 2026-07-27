package openapi

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestSchemaNameCollisionPanics pins that EntityOpenAPI surfaces a
// schema-component collision instead of silently overwriting. A
// versioned "posts" entity under /api/v1 computes the component name
// "posts_api_v1"; an unversioned entity literally named "posts_api_v1"
// computes the same name. Before the fix, the second AddSchema call
// silently clobbered the first, so /api/v1/posts referenced an
// unrelated entity's schema.
func TestSchemaNameCollisionPanics(t *testing.T) {
	versioned := entity.Define("posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	versioned.Version = "/api/v1"

	shadow := entity.Define("posts_api_v1", entity.EntityConfig{
		Table:  "posts_api_v1",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("EntityOpenAPI with colliding schema names did not panic")
		}
	}()
	EntityOpenAPI(reg(versioned, shadow), "Test", "1.0.0")
}
