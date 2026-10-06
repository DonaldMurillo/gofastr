package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// detailUnderListFixture is a blueprint with a notes list at /notes and a
// notes detail screen at detailRoute, its block inside a section when
// nested.
func detailUnderListFixture(detailRoute string, nested bool) string {
	detail := `
      - kind: entity_detail
        entity: notes`
	if nested {
		detail = `
      - kind: section
        props:
          heading: Note
        children:
          - kind: entity_detail
            entity: notes`
	}
	return `
app:
  name: Chroma
  module: github.com/example/chroma
entities:
  - name: notes
    crud: true
    fields:
      - name: title
        type: string
        required: true
screens:
  - name: notes
    route: /notes
    layout: app
    title: Notes
    body:
      - kind: entity_list
        entity: notes
        fields: [title]
  - name: note
    route: ` + detailRoute + `
    layout: app
    title: Note
    body:` + detail + "\n"
}

// A detail screen must sit at <list route>/{id}, where the list's record
// links point, whether its block is at the top of the body or inside a
// layout block.
func TestDetailScreenMustSitUnderList(t *testing.T) {
	for _, nested := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "gofastr.yml")
		writeTestFile(t, path, detailUnderListFixture("/note/{id}", nested))
		if _, err := loadBlueprint(path); err == nil || !strings.Contains(err.Error(), `must sit at "/notes"/{id}`) {
			t.Errorf("nested=%v: a detail screen off the list route loaded: %v", nested, err)
		}
		writeTestFile(t, path, detailUnderListFixture("/notes/{id}", nested))
		bp, err := loadBlueprint(path)
		if err != nil {
			t.Fatalf("nested=%v: the detail screen under its list was refused: %v", nested, err)
		}
		if base, ok := blueprintDetailBase(bp, "notes"); !ok || base != "/notes" {
			t.Errorf("nested=%v: detail base = %q, %v; want /notes", nested, base, ok)
		}
	}
}
