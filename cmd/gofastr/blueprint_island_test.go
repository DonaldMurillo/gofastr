package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestGeneratedEntityListIsIsland pins Hard rule 1: a generated entity_list
// must sort and paginate as an ISLAND (a data-fui-rpc swap of just the table),
// never a full document/route navigation. The resource engine's island mode
// needs the list's Config to carry an island path AND a TableHandler mounted
// at that path — exactly the wiring examples/meridian/app.go does for its
// customers list. Without it, sort headers + pagination emit plain <a href>
// links that full-navigate (Hard rule 1 violation).
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
	if !strings.Contains(crud, `.WithIsland("/api/tables/posts/posts")`) {
		t.Errorf("list-bearing entity must set an island path so sort/page swaps the table island, not the whole page:\n%s", crud)
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

// The island endpoint is a second route onto the rows a screen shows. It must
// carry that screen's own gate: mounting it ungated publishes an admin-only
// list to every signed-in user, who can read it by calling the very path the
// page's sort header points at.
func TestGeneratedIslandCarriesScreenPolicy(t *testing.T) {
	crud := generatedPostsCrudFile(t)

	if !strings.Contains(crud, `.WithIslandPolicy(authPolicy("/login", "admin"))`) {
		t.Errorf("the admin-gated screen's island must carry authPolicy with its role:\n%s", crud)
	}
	if !strings.Contains(crud, `.WithIslandPolicy(authPolicy("/login", ""))`) {
		t.Errorf("the auth-only screen's island must still require sign-in:\n%s", crud)
	}
}

// Two screens showing the same entity refine it differently — different
// columns, a search box on one. One shared endpoint cannot serve both: a sort
// click on the filtered page would come back unfiltered. Each screen gets its
// own endpoint serving its own refined config.
func TestGeneratedIslandIsPerScreenAndKeepsRefinements(t *testing.T) {
	crud := generatedPostsCrudFile(t)

	for _, want := range []string{
		`"GET", "/api/tables/posts/posts"`,
		`"GET", "/api/tables/board/posts"`,
	} {
		if !strings.Contains(crud, want) {
			t.Errorf("expected a per-screen island endpoint %s:\n%s", want, crud)
		}
	}
	// The mounted config must be the refined one, not the bare registry entry.
	if !strings.Contains(crud, `appResources["posts"].WithColumns("title").WithSearch("title").WithIsland("/api/tables/posts/posts")`) {
		t.Errorf("the posts screen's island must serve that screen's columns and search:\n%s", crud)
	}
	if !strings.Contains(crud, `appResources["posts"].WithColumns("title", "status").WithIsland("/api/tables/board/posts")`) {
		t.Errorf("the board screen's island must serve that screen's own columns:\n%s", crud)
	}
	if strings.Contains(crud, `appResources["posts"].TableHandler()`) {
		t.Error("the island must not be mounted on the bare registry entry — it would drop the screen's columns, search and filters")
	}
}

// generatedPostsCrudFile renders a blueprint whose entity appears on two
// screens with different access and different refinements.
func generatedPostsCrudFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, `app:
  name: Island
  module: example.com/island
  auth:
    enabled: true
entities:
  - name: posts
    fields:
      - name: title
        type: string
        required: true
      - name: status
        type: enum
        values: [open, done]
screens:
  - name: posts
    route: /posts
    access:
      auth: true
      role: admin
    body:
      - kind: entity_list
        entity: posts
        fields: [title]
        search: title
  - name: board
    route: /board
    access:
      auth: true
    body:
      - kind: entity_list
        entity: posts
        fields: [title, status]
`)
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	for _, f := range mustRenderBlueprintFiles(t, bp) {
		if strings.Contains(f.content, `appResources["posts"] = resource.Config{`) {
			return f.content
		}
	}
	t.Fatal(`no generated file assigns appResources["posts"]`)
	return ""
}

// A list on a public screen must stay public through its island. With no
// policy at all the engine falls back to requiring sign-in, so an anonymous
// visitor's first sort click would come back 401 on a page they can read.
func TestGeneratedPublicScreenIslandStaysPublic(t *testing.T) {
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
	var crud string
	for _, f := range mustRenderBlueprintFiles(t, bp) {
		if strings.Contains(f.content, `appResources["posts"] = resource.Config{`) {
			crud = f.content
		}
	}
	if !strings.Contains(crud, ".WithIslandPolicy(resource.PublicIsland())") {
		t.Errorf("an ungated screen's island must declare itself public:\n%s", crud)
	}
}
