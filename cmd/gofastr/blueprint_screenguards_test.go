package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// The validate-time guards the entityui screens need: the detail route
// sits where the list links at, the list's mode names a presentation, and
// the replaced block kinds are refused with the fix in the error.

// A detail screen whose route is not <list route>/{id} strands the list's
// record links, which entityui draws at <base>/<id>: refused, naming the
// expected route.
func TestDetailRouteMustSitUnderList(t *testing.T) {
	base := `
app:
  name: DR
  module: example.com/dr
entities:
  - name: posts
    crud: true
    fields:
      - name: title
        type: string
screens:
  - name: posts
    route: /posts
    body:
      - kind: entity_list
        entity: posts
        fields: [title]
        create: true
%s`
	for _, tc := range []struct {
		name  string
		route string
	}{
		{"detail elsewhere", `
  - name: post_detail
    route: /records/posts/{id}
    body:
      - kind: entity_detail
        entity: posts
`},
		{"detail flat", `
  - name: post_detail
    route: /posts/{id}/view
    body:
      - kind: entity_detail
        entity: posts
`},
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "gofastr.yml")
		writeTestFile(t, path, strings.Replace(base, "%s", tc.route, 1))
		_, err := loadBlueprint(path)
		if err == nil {
			t.Fatalf("%s: a detail route off the list route must be refused", tc.name)
		}
		if !strings.Contains(err.Error(), "/posts/{id}") {
			t.Fatalf("%s: error should name the expected route /posts/{id}, got: %v", tc.name, err)
		}
	}

	// The right route validates, and a detail-only entity (no list screen)
	// keeps only its own {id} requirement.
	ok := strings.Replace(base, "%s", `
  - name: post_detail
    route: /posts/{id}
    body:
      - kind: entity_detail
        entity: posts
`, 1)
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, ok)
	if _, err := loadBlueprint(path); err != nil {
		t.Fatalf("a detail screen at <list>/{id} must validate, got %v", err)
	}
}

// entity_list mode picks a presentation; anything but table and cards is
// refused naming the two.
func TestEntityListModeRefused(t *testing.T) {
	yml := `
app:
  name: MD
  module: example.com/md
entities:
  - name: posts
    crud: true
    fields:
      - name: title
        type: string
screens:
  - name: posts
    route: /posts
    body:
      - kind: entity_list
        entity: posts
        fields: [title]
        mode: board
`
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, yml)
	_, err := loadBlueprint(path)
	if err == nil {
		t.Fatal("an unknown entity_list mode must be refused")
	}
	if !strings.Contains(err.Error(), "table or cards") {
		t.Fatalf("error should name table or cards, got: %v", err)
	}
}

// Authored entity_create / entity_edit blocks are refused, each naming
// where the screen went.
func TestEntityCreateEditKindsRefused(t *testing.T) {
	for kind, want := range map[string]string{
		"entity_create": "create:",
		"entity_edit":   "entity_detail",
	} {
		yml := `
app:
  name: CE
  module: example.com/ce
entities:
  - name: posts
    crud: true
    fields:
      - name: title
        type: string
screens:
  - name: post_form
    route: /posts/form
    body:
      - kind: ` + kind + `
        entity: posts
`
		dir := t.TempDir()
		path := filepath.Join(dir, "gofastr.yml")
		writeTestFile(t, path, yml)
		_, err := loadBlueprint(path)
		if err == nil {
			t.Fatalf("an authored %s block must be refused", kind)
		}
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: error should name %q, got: %v", kind, want, err)
		}
	}
}
