package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// The entityui screens replaced the resource engine's island model: a list
// is a query-param page. The security property the island tests pinned —
// no second route onto the rows a screen shows, bypassing that screen's
// gate — is pinned here against the entityui output: the list's only route
// is the page's own, whose policy the mount statement carries, and the
// reads themselves run through the entity's read gates inside the render.

// TestGeneratedListMountsNoSecondRoute: a generated entity_list must not
// mount any endpoint beside its screen. Sort, page and filter ride the
// page's own URL as query params (Hard rule 1), so nothing may hand out a
// table refresh route the page's gate never sees.
func TestGeneratedListMountsNoSecondRoute(t *testing.T) {
	files := mustRenderBlueprintFiles(t, postsListBlueprint(t))
	for _, f := range files {
		if !strings.HasPrefix(f.name, "screen_") && f.name != "app.go" && f.name != "main.go" {
			continue
		}
		for _, banned := range []string{"TableHandler", "WithIsland", "HandleFunc(\"GET\""} {
			if strings.Contains(f.content, banned) {
				t.Errorf("%s must not mount a table endpoint (%q):\n%s", f.name, banned, f.content)
			}
		}
	}
	// The list renders through the appUI builder inside the screen.
	crud := fileContent(files, "screen_posts_crud.go")
	if !strings.Contains(crud, `appUI.List("posts")`) {
		t.Errorf("the posts screen must render appUI.List:\n%s", crud)
	}
	// Hard rule 7: generated apps ship ZERO bespoke classes.
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

// The list's read gate is the screen's own policy: an admin-gated list and
// an auth-only list each register with their policy, and no island
// expression exists to carry a weaker copy of it.
func TestGeneratedListReadsThroughScreenGate(t *testing.T) {
	crud := generatedPostsCrudFile(t)
	if !strings.Contains(crud, `WithPolicy(authPolicy("/login", "admin"))`) {
		t.Errorf("the admin-gated screen must register its policy:\n%s", crud)
	}
	if !strings.Contains(crud, `WithPolicy(authPolicy("/login", ""))`) {
		t.Errorf("the auth-only screen must still require sign-in:\n%s", crud)
	}
	for _, banned := range []string{"WithIslandPolicy", "PublicIsland"} {
		if strings.Contains(crud, banned) {
			t.Errorf("island policy expressions are gone with the islands (%q):\n%s", banned, crud)
		}
	}
}

// Two screens showing the same entity keep their own refinements: each
// screen's render call carries its own Columns chain, and nothing is shared
// that a sort click on one could rewrite under the other.
func TestGeneratedListsKeepPerScreenRefinements(t *testing.T) {
	crud := generatedPostsCrudFile(t)
	if !strings.Contains(crud, `appUI.List("posts").Columns("title")`) {
		t.Errorf("the posts screen must render its own columns:\n%s", crud)
	}
	if !strings.Contains(crud, `appUI.List("posts").Columns("title", "status")`) {
		t.Errorf("the board screen must render its own columns:\n%s", crud)
	}
}

// A list on a public screen stays public: the screen registers with no
// policy at all, and an anonymous visitor's sort click is just another GET
// of a page they can already read.
func TestGeneratedPublicListNeedsNoPolicy(t *testing.T) {
	files := mustRenderBlueprintFiles(t, postsListBlueprint(t))
	crud := fileContent(files, "screen_posts_crud.go")
	if !strings.Contains(crud, `site.Register("/posts", &PostsScreen{`) {
		t.Errorf("an ungated list screen registers with no policy:\n%s", crud)
	}
	if strings.Contains(crud, "authPolicy(") {
		t.Errorf("an ungated list screen must not grow a policy:\n%s", crud)
	}
}

// One screen cannot list the same entity twice: both lists share the
// entity's query-param namespace on that page, so sorting either rewrites
// both. Refused at validate time.
func TestBlueprintRejectsDuplicateListPerScreen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, `app:
  name: Dup
  module: example.com/dup
entities:
  - name: posts
    crud: true
    fields:
      - name: title
        type: string
      - name: status
        type: enum
        values: [open, done]
screens:
  - name: dashboard
    route: /dash
    body:
      - kind: entity_list
        entity: posts
        fields: [title]
        limit: 5
      - kind: entity_list
        entity: posts
        fields: [title, status]
        limit: 20
`)
	_, err := loadBlueprint(path)
	if err == nil {
		t.Fatal("one screen listing an entity twice must be rejected")
	}
	if !strings.Contains(err.Error(), "more than once") {
		t.Fatalf("error should name the duplicate list, got: %v", err)
	}
}

// Two screens whose names normalize to the same file name would emit one
// screen_<snake>.go twice; the second write silently replaces the first and
// its route never mounts. Refused at validate time.
func TestBlueprintRejectsCollidingScreenFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, `app:
  name: Collide
  module: example.com/collide
entities:
  - name: posts
    crud: true
    fields:
      - name: title
        type: string
screens:
  - name: API
    route: /a
    body:
      - kind: entity_list
        entity: posts
        fields: [title]
  - name: Api
    route: /b
    body:
      - kind: entity_list
        entity: posts
        fields: [title]
`)
	_, err := loadBlueprint(path)
	if err == nil {
		t.Fatal("two screens whose file names collide must be rejected")
	}
	if !strings.Contains(err.Error(), "screen file") {
		t.Fatalf("error should name the colliding screen file, got: %v", err)
	}
}

// postsListBlueprint is the minimal public list fixture.
func postsListBlueprint(t *testing.T) Blueprint {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, `app:
  name: List
  module: example.com/list
entities:
  - name: posts
    crud: true
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
	return bp
}

// generatedPostsCrudFile renders a blueprint whose entity appears on two
// screens with different access and different columns.
func generatedPostsCrudFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	writeTestFile(t, path, `app:
  name: Screens
  module: example.com/screens
  auth:
    enabled: true
entities:
  - name: posts
    crud: true
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
	return fileContent(mustRenderBlueprintFiles(t, bp), "screen_posts_crud.go")
}
