package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestGeneratedEntityListIsIsland pins Hard rule 1: a generated entity_list
// must sort and paginate as an ISLAND (a data-fui-rpc swap of just the table),
// never a full document/route navigation. The resource engine's island mode
// needs IslandPath set on the Config AND a TableHandler mounted at that path —
// exactly the wiring examples/meridian/app.go does for its customers list.
// Generated apps must match; without it, sort headers + pagination emit plain
// <a href> links that full-navigate (Hard rule 1 violation).
func TestGeneratedEntityListIsIsland(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, `app:
  name: Island
  module: example.com/island
entities:
  - name: posts
    fields:
      - name: title
        type: string
        required: true
screens:
  - name: posts
    route: /posts
    body:
      - kind: entity_list
        entity: posts
        fields: [title]
`)
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	files := mustRenderBlueprintFiles(t, bp)
	// The entity's crud file carries the resource.Config assignment AND the
	// island handler registration (both land in the mount func, which has fwApp).
	var crud string
	for _, f := range files {
		if strings.Contains(f.content, `appResources["posts"] = resource.Config{`) {
			crud = f.content
		}
	}
	if crud == "" {
		t.Fatalf("no crud file with appResources[\"posts\"] assignment among %d files", len(files))
	}
	if !strings.Contains(crud, "IslandPath:") {
		t.Errorf("list-bearing entity config must set IslandPath so sort/page swaps the table island, not the whole page:\n%s", crud)
	}
	if !strings.Contains(crud, "TableHandler()") {
		t.Errorf("list-bearing entity must register a TableHandler route serving the island sort/page RPC:\n%s", crud)
	}
	// Hard rule 7: generated apps ship ZERO bespoke classes. The dead
	// client-JS entity-list/detail emitters used to emit .gofastr-* / .detail-*;
	// they must never reach (or be resurrected into) emitted files.
	for _, f := range files {
		for _, banned := range []string{
			"gofastr-entity-list", "gofastr-entity-detail",
			`"detail-field"`, `"detail-label"`, `"detail-value"`,
		} {
			if strings.Contains(f.content, banned) {
				t.Errorf("%s: emitted file contains forbidden bespoke class marker %q", f.name, banned)
			}
		}
	}
}
