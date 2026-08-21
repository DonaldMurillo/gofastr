package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// entity_detail renders .Detail(ctx, s.id) and the resource list links to
// BasePath+"/"+id. A detail screen whose route has no {id} param renders an
// empty record and its "View" link matches no registered route. The validator
// must reject it and say what to change.
func TestEntityDetailRequiresIDInRoute(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, `
app:
  name: Demo
  module: ex.com/demo
entities:
  - name: products
    crud: true
    fields:
      - name: title
        type: string
screens:
  - name: product_detail
    route: /product-detail
    body:
      - type: entity_detail
        entity: products
`)
	_, err := loadBlueprint(path)
	if err == nil {
		t.Fatal("entity_detail screen with no {id} in route should fail validation")
	}
	if !strings.Contains(err.Error(), "{id}") {
		t.Fatalf("error should mention {id}, got: %v", err)
	}
}

// entity_form mode: edit posts to {entity}/{id} and targets a specific record,
// so it has the same {id} requirement. mode: create does not (new record).
func TestEntityFormEditRequiresIDInRoute(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, `
app:
  name: Demo
  module: ex.com/demo
entities:
  - name: products
    crud: true
    fields:
      - name: title
        type: string
screens:
  - name: product_edit
    route: /product-edit
    body:
      - type: entity_form
        entity: products
        mode: edit
`)
	_, err := loadBlueprint(path)
	if err == nil {
		t.Fatal("entity_form mode:edit screen with no {id} in route should fail validation")
	}
	if !strings.Contains(err.Error(), "{id}") {
		t.Fatalf("error should mention {id}, got: %v", err)
	}
}

// Controls: a detail screen WITH {id}, and an edit form WITH {id}, validate.
// A create form (no id needed) on a param-less route also validates.
func TestEntityDetailAndEditAcceptIDInRoute(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, `
app:
  name: Demo
  module: ex.com/demo
entities:
  - name: products
    crud: true
    fields:
      - name: title
        type: string
screens:
  - name: product_detail
    route: /products/{id}
    body:
      - type: entity_detail
        entity: products
  - name: product_edit
    route: /products/{id}/edit
    body:
      - type: entity_form
        entity: products
        mode: edit
  - name: product_new
    route: /products/new
    body:
      - type: entity_form
        entity: products
        mode: create
`)
	if _, err := loadBlueprint(path); err != nil {
		t.Fatalf("valid detail/edit/new screens should validate, got: %v", err)
	}
}
