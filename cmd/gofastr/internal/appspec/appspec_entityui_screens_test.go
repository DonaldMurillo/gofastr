package appspec

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// One screen cannot list the same entity twice: both lists share the
// entity's query-param namespace on that page, so sorting either rewrites
// both. Refused at validate time.
func TestAppSpecRejectsDuplicateListPerScreen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, path, `app:
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
	_, err := LoadAppSpec(path)
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
		// A list with create: true adds a posts_create screen at
		// /posts/create, unless a screen already holds that route.
		"authored vs synthesized create": {"  - name: posts_create\n    route: /new\n" + text + "  - name: Posts\n    route: /posts\n" + list("posts") + "        create: true\n", "PostsCreateScreen"},
		// The dashboard files under posts, its first list; the create
		// screen it adds for customers writes screen_customers_crud.go.
		"authored vs synthesized file": {"  - name: Dash\n    route: /dash\n" + list("posts") + "      - kind: entity_list\n        entity: customers\n        fields: [title]\n        create: true\n" +
			"  - name: customers_crud\n    route: /c\n" + text, "screen_customers_crud.go"},
		"authored create route": {"  - name: posts_create\n    route: /posts/create\n" + text + "  - name: Posts\n    route: /posts\n" + list("posts") + "        create: true\n", ""},
		// Both write screen_posts_crud.go, as one file, as two types.
		"one entity's screens": {"  - name: API\n    route: /a\n" + list("posts") + "  - name: Api\n    route: /b\n" + list("posts"), ""},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "gofastr.yml")
			clitest.WriteTestFile(t, path, head+c.screens)
			_, err := LoadAppSpec(path)
			switch {
			case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
				t.Fatalf("want an error naming %q, got: %v", c.want, err)
			case c.want == "" && err != nil:
				t.Fatalf("one entity's screens share their file: %v", err)
			}
		})
	}
}
