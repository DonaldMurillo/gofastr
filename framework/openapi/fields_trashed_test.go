package openapi

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestListGetDeclareFields asserts the list and get ops advertise ?fields=
// (projection) so SDK generators can see it.
func TestListGetDeclareFields(t *testing.T) {
	e := entity.Define("posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	})
	doc := EntityOpenAPI(reg(e), "Test", "1.0.0").Build()
	paths := getMap(t, doc, "paths")

	listOp := getMap(t, getMap(t, paths, "/posts"), "get")
	if findParam(listOp["parameters"], "fields") == nil {
		t.Error("list op missing 'fields' parameter")
	}

	getOp := getMap(t, getMap(t, paths, "/posts/{id}"), "get")
	if findParam(getOp["parameters"], "fields") == nil {
		t.Error("get op missing 'fields' parameter")
	}
}

// TestSoftDeleteListDeclaresTrashed asserts ?trashed= is advertised on the
// list op only for soft-delete entities.
func TestSoftDeleteListDeclaresTrashed(t *testing.T) {
	soft := entity.Define("notes", entity.EntityConfig{
		Table:      "notes",
		SoftDelete: true,
		Fields:     []schema.Field{{Name: "title", Type: schema.String}},
	})
	doc := EntityOpenAPI(reg(soft), "Test", "1.0.0").Build()
	listOp := getMap(t, getMap(t, getMap(t, doc, "paths"), "/notes"), "get")
	if findParam(listOp["parameters"], "trashed") == nil {
		t.Error("soft-delete list op missing 'trashed' parameter")
	}
}

// TestNonSoftDeleteHasNoTrashed asserts ?trashed= is NOT advertised when the
// entity has no soft-delete.
func TestNonSoftDeleteHasNoTrashed(t *testing.T) {
	hard := entity.Define("posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	})
	doc := EntityOpenAPI(reg(hard), "Test", "1.0.0").Build()
	listOp := getMap(t, getMap(t, getMap(t, doc, "paths"), "/posts"), "get")
	if findParam(listOp["parameters"], "trashed") != nil {
		t.Error("non-soft-delete list op should not declare 'trashed'")
	}
}
