package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// listFiltersYAML is a minimal blueprint whose entity_list declares facet
// filters over an enum, a relation, and a bool column.
func listFiltersYAML() string {
	return `
app:
  name: Demo
  module: example.com/demo
  db:
    driver: sqlite
    url: file:demo.db
entities:
  - name: users
    crud: true
    fields:
      - name: email
        type: string
        required: true
  - name: posts
    crud: true
    fields:
      - name: title
        type: string
        required: true
      - name: status
        type: enum
        values: [draft, published]
      - name: featured
        type: bool
      - name: author_id
        type: relation
        to: users
    relations:
      - type: belongs_to
        name: author
        entity: users
        foreign_key: author_id
screens:
  - name: home
    route: /
    body:
      - kind: entity_list
        entity: posts
        text: Posts
        fields: [title, status]
        search: title
        filters: [status, author_id, featured]
`
}

func TestListFiltersEmit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, listFiltersYAML())
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	files, err := renderBlueprintFiles(bp)
	if err != nil {
		t.Fatalf("renderBlueprintFiles: %v", err)
	}
	byName := filesByName(files)

	// The screen wires the facets with resolved type + enum values so the
	// framework engine needs no schema at render time.
	screens := allScreenContent(files)
	assertContains(t, screens,
		`.WithFilters(resource.Filter{Key: "status", Label: "Status", Type: "enum", Values: []string{"draft", "published"}}, resource.Filter{Key: "author_id", Label: "Author", Type: "relation"}, resource.Filter{Key: "featured", Label: "Featured", Type: "bool"})`)
	assertContains(t, screens, `resource.Config{`)
	assertContains(t, screens, `Entity: "posts"`)

	// The owned file keeps only the per-app registry and hooks. Rendering,
	// filtering, and formatting live in framework/ui/resource.
	res := byName["resource.go"]
	assertContains(t, res, `"github.com/DonaldMurillo/gofastr/framework/ui/resource"`)
	assertContains(t, res, `var appResources = resource.Registry{}`)
	for _, copiedEngineSymbol := range []string{`func (c ResourceConfig)`, `filterToolbar`, `resFormat`, `groupCounts`} {
		if strings.Contains(res, copiedEngineSymbol) {
			t.Errorf("resource.go still contains engine symbol %q:\n%s", copiedEngineSymbol, res)
		}
	}
}

func TestListFiltersRejectBadColumn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yml")
	writeTestFile(t, path, `
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
  - name: home
    route: /
    body:
      - kind: entity_list
        entity: posts
        fields: [title, status]
        filters: [nope]
`)
	_, err := loadBlueprint(path)
	if err == nil || !strings.Contains(err.Error(), `entity_list filter "nope" is not defined on entity "posts"`) {
		t.Fatalf("loadBlueprint err = %v, want unknown-filter-column error", err)
	}
}

func TestListFiltersRejectBadType(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yml")
	writeTestFile(t, path, `
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
  - name: home
    route: /
    body:
      - kind: entity_list
        entity: posts
        fields: [title, status]
        filters: [title]
`)
	_, err := loadBlueprint(path)
	if err == nil || !strings.Contains(err.Error(), `only enum, bool, and relation columns can be faceted`) {
		t.Fatalf("loadBlueprint err = %v, want unsupported-filter-type error", err)
	}
}
