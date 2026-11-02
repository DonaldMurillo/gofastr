package appgen

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
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
		if !strings.HasPrefix(f.Name, "screen_") && f.Name != "app.go" && f.Name != "main.go" {
			continue
		}
		for _, banned := range []string{"TableHandler", "WithIsland", "HandleFunc(\"GET\""} {
			if strings.Contains(f.Content, banned) {
				t.Errorf("%s must not mount a table endpoint (%q):\n%s", f.Name, banned, f.Content)
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
			if strings.Contains(f.Content, banned) {
				t.Errorf("%s: emitted file contains forbidden bespoke class marker %q", f.Name, banned)
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

// postsListAppSpec is the minimal public list fixture.
func postsListAppSpec(t *testing.T) appspec.AppSpec {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, path, `app:
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
	bp, err := appspec.LoadAppSpec(path)
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
	clitest.WriteTestFile(t, path, `app:
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
	bp, err := appspec.LoadAppSpec(path)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
	}
	return fileContent(mustRenderAppSpecFiles(t, bp), "screen_posts_crud.go")
}

// A list whose entity has no record screen draws its rows without record
// links: entityui links each row (and its row menu's View) to
// <list route>/<id>, and with no entity_detail screen there that path is a
// 404 on every row. A list with a detail screen keeps its links. Caught
// live on the ecommerce example, where every Categories and Reviews row
// linked to a page that did not exist.
func TestGeneratedListWithoutRecordScreenDrawsNoLinks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, path, `app:
  name: Links
  module: example.com/links
entities:
  - name: posts
    crud: true
    fields:
      - name: title
        type: string
        required: true
  - name: tags
    crud: true
    fields:
      - name: label
        type: string
        required: true
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
  - name: tags
    route: /tags
    body:
      - kind: entity_list
        entity: tags
        fields: [label]
  - name: home
    route: /
    body:
      - kind: entity_list
        entity: tags
        fields: [label]
      - kind: entity_list
        entity: posts
        fields: [title]
`)
	bp, err := appspec.LoadAppSpec(path)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
	}
	files := mustRenderAppSpecFiles(t, bp)
	tags := fileContent(files, "screen_tags_crud.go")
	if !strings.Contains(tags, `appUI.List("tags").Columns("label").NoCreate().NoLinks()`) {
		t.Errorf("a list with no record screen must draw no record links:\n%s", tags)
	}
	posts := fileContent(files, "screen_posts_crud.go")
	if strings.Contains(posts, "NoLinks()") {
		t.Errorf("a list with a record screen keeps its record links:\n%s", posts)
	}
	var home string
	for _, f := range files {
		if strings.Contains(f.Content, "type HomeScreen struct") {
			home = f.Content
		}
	}
	if !strings.Contains(home, `appUI.List("tags").Key("tags").Columns("label").NoCreate().NoLinks()`) {
		t.Errorf("an off-home list of an entity with no record screen draws no links either:\n%s", home)
	}
	if !strings.Contains(home, `appUI.List("posts").Key("posts").Columns("title").NoCreate().Base("/posts").RenderCtx(ctx)`) {
		t.Errorf("an off-home list of an entity with a record screen links to its home:\n%s", home)
	}
}
