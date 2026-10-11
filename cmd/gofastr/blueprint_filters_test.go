package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// listFiltersYAML is a minimal blueprint whose entity_list still declares
// the screen-level filters: key the entityui screens removed.
func listFiltersYAML() string {
	return `
app:
  name: Filters
  module: example.com/filters
entities:
  - name: posts
    crud: true
    fields:
      - name: title
        type: string
      - name: status
        type: enum
        values: [draft, published]
      - name: featured
        type: bool
      - name: author_id
        type: string
screens:
  - name: posts
    route: /posts
    body:
      - kind: entity_list
        entity: posts
        fields: [title, status]
        filters: [status]
`
}

// TestFiltersKeyNamesReplacement: the strict key check refuses filters: on
// an entity_list block and the error names where the setting moved (the
// entity's display: facets:), so an author holding an old blueprint hears
// the fix rather than "unknown key".
func TestFiltersKeyNamesReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, listFiltersYAML())
	_, err := loadBlueprint(path)
	if err == nil {
		t.Fatal("entity_list filters: should fail decode")
	}
	for _, want := range []string{"filters", "display:", "facets:"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error should name %q, got: %v", want, err)
		}
	}
}

// One refusal per removed key, each asserting the message names the
// replacement: filters → display facets, transitions → states, search →
// search_fields, island/widget → query-param pages.
func TestRemovedBlockKeysNameReplacements(t *testing.T) {
	base := `
app:
  name: RK
  module: example.com/rk
entities:
  - name: posts
    crud: true
    fields:
      - name: title
        type: string
      - name: status
        type: enum
        values: [draft, published]
screens:
  - name: posts
    route: /posts
    body:
      - kind: entity_list
        entity: posts
        fields: [title]
        %s
`
	for _, tc := range []struct {
		key  string
		want string
	}{
		{"filters: [status]", "display: facets:"},
		{"search: title", "search_fields:"},
		{"island: tables/posts", "query-param pages"},
		{"widget: posts", "query-param pages"},
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "gofastr.yml")
		writeTestFile(t, path, strings.Replace(base, "%s", tc.key, 1))
		_, err := loadBlueprint(path)
		if err == nil {
			t.Fatalf("%s should fail decode", tc.key)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: error should name the replacement %q, got: %v", tc.key, tc.want, err)
		}
	}
}

// transitions: on an entity_detail block points at the entity's states:.
func TestTransitionsKeyNamesStates(t *testing.T) {
	yml := `
app:
  name: TR
  module: example.com/tr
entities:
  - name: posts
    crud: true
    fields:
      - name: title
        type: string
      - name: status
        type: enum
        values: [draft, published]
screens:
  - name: posts
    route: /posts
    body:
      - kind: entity_list
        entity: posts
        fields: [title]
  - name: post_detail
    route: /posts/{id}
    body:
      - kind: entity_detail
        entity: posts
        transitions:
          - label: Publish
            status: published
`
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, yml)
	_, err := loadBlueprint(path)
	if err == nil {
		t.Fatal("entity_detail transitions: should fail decode")
	}
	for _, want := range []string{"transitions", "states:"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error should name %q, got: %v", want, err)
		}
	}
}

// The entityui facets spelling still works: display: facets: on the entity
// decodes and reaches the emitted registration.
func TestDisplayFacetsDecode(t *testing.T) {
	yml := `
app:
  name: FC
  module: example.com/fc
entities:
  - name: posts
    crud: true
    fields:
      - name: title
        type: string
      - name: status
        type: enum
        values: [draft, published]
    display:
      facets: [status]
screens:
  - name: posts
    route: /posts
    body:
      - kind: entity_list
        entity: posts
        fields: [title]
`
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, yml)
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	d := bp.Entities[0].Display
	if d == nil || len(d.Facets) != 1 || d.Facets[0] != "status" {
		t.Fatalf("display.facets did not decode: %+v", d)
	}
}
