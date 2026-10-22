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
	files := mustRenderAppSpecFiles(t, postsListAppSpec(t))
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
	files := mustRenderAppSpecFiles(t, postsListAppSpec(t))
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
func TestAppSpecRejectsDuplicateListPerScreen(t *testing.T) {
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
	_, err := loadAppSpec(path)
	if err == nil {
		t.Fatal("one screen listing an entity twice must be rejected")
	}
	if !strings.Contains(err.Error(), "more than once") {
		t.Fatalf("error should name the duplicate list, got: %v", err)
	}
}

// Two screens that land in the same generated file would emit it twice;
// the second write silently replaces the first and its route never
// mounts. A standalone screen writes screen_<name>.go and an entity's
// screens share screen_<entity>_crud.go, so "Posts Crud" collides with
// the posts screens' file. Two screens with one name in different
// files would each declare <Name>Screen, and the app would not build.
// Refused at validate time.
func TestAppSpecRejectsCollidingScreenFiles(t *testing.T) {
	const head = `app:
  name: Collide
  module: example.com/collide
entities:
  - name: posts
    crud: true
    fields:
      - name: title
        type: string
  - name: customers
    crud: true
    fields:
      - name: title
        type: string
screens:
`
	list := func(entity string) string {
		return "    body:\n      - kind: entity_list\n        entity: " + entity + "\n        fields: [title]\n"
	}
	const text = `    body:
      - kind: heading
        text: Hi
`
	for name, c := range map[string]struct {
		screens string
		want    string // "" when the spec is valid
	}{
		"two standalone":          {"  - name: API\n    route: /a\n" + text + "  - name: Api\n    route: /b\n" + text, "screen file"},
		"standalone vs crud file": {"  - name: Posts Crud\n    route: /a\n" + text + "  - name: Posts\n    route: /b\n" + list("posts"), "screen file"},
		"one name, two entities":  {"  - name: Overview\n    route: /a\n" + list("posts") + "  - name: Overview\n    route: /b\n" + list("customers"), "OverviewScreen"},
		"one name, one entity":    {"  - name: Overview\n    route: /a\n" + list("posts") + "  - name: Overview\n    route: /b\n" + list("posts"), "OverviewScreen"},
		// Both write screen_posts_crud.go, as one file, as two types.
		"one entity's screens": {"  - name: API\n    route: /a\n" + list("posts") + "  - name: Api\n    route: /b\n" + list("posts"), ""},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "gofastr.yml")
			writeTestFile(t, path, head+c.screens)
			_, err := loadAppSpec(path)
			switch {
			case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
				t.Fatalf("want an error naming %q, got: %v", c.want, err)
			case c.want == "" && err != nil:
				t.Fatalf("one entity's screens share their file: %v", err)
			}
		})
	}
}

// postsListAppSpec is the minimal public list fixture.
func postsListAppSpec(t *testing.T) AppSpec {
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
	bp, err := loadAppSpec(path)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
	}
	return bp
}

// generatedPostsCrudFile renders an app spec whose entity appears on two
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
	bp, err := loadAppSpec(path)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
	}
	return fileContent(mustRenderAppSpecFiles(t, bp), "screen_posts_crud.go")
}
