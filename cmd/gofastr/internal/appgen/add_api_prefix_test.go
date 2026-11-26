package appgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// addProjectSpec is a project whose app section moves the routes an
// added screen reaches: the API under /v1 and the auth battery under
// /account.
const addProjectSpec = `app:
  name: AddTest
  module: example.com/addtest
  api_prefix: v1
  db:
    driver: sqlite
    url: file:test.db
  auth:
    enabled: true
    dev_mode: true
    base_path: /account
entities:
  - name: posts
    fields:
      - name: title
        type: string
        required: true
screens:
  - name: signin
    route: /signin
    body:
      - kind: login_form
`

// addFragmentScreen generates addProjectSpec in a fresh project, adds
// the fragment screens (YAML list items under screens:) with --add, and
// returns the named file the add wrote.
func addFragmentScreen(t *testing.T, project, screens, file string) string {
	t.Helper()
	dir, spec := clitest.AddSetup(t, project)
	clitest.CovT_capStdout(t, func() { GenerateFromAppSpec(GenerateOptions{Spec: spec}) })
	fragment := filepath.Join(dir, "fragment.yml")
	clitest.WriteTestFile(t, fragment, "screens:\n"+screens)
	clitest.CovT_capStdout(t, func() { GenerateFromAppSpec(GenerateOptions{Spec: fragment, add: true}) })
	src, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		t.Fatal(err)
	}
	return string(src)
}

// A screen added to a project with api_prefix: v1 and auth under
// /account posts to, invalidates on and signs up through the project's
// routes, not the fragment's empty app section.
func TestAddedScreenUsesProjectAPIPrefix(t *testing.T) {
	for _, tc := range []struct{ name, screen, file, want string }{
		{"form", "  - name: new_post\n    route: /posts/write\n    title: Write\n    body:\n      - kind: entity_form\n        entity: posts\n        fields: [title]\n",
			"screen_new_post.go", `interactive.Post("/v1/posts")`},
		{"invalidation", "  - name: stats\n    route: /stats\n    title: Stats\n    body:\n      - kind: stat_card\n        props:\n          label: Posts\n          source:\n            entity: posts\n            agg: count\n",
			"screen_stats.go", `invalidateScreensOnWrite(fwApp, "/v1/posts"`},
		{"signup", "  - name: join\n    route: /join\n    body:\n      - kind: signup_form\n",
			"screen_join.go", `Action: "/account/register"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if src := addFragmentScreen(t, addProjectSpec, tc.screen, tc.file); !strings.Contains(src, tc.want) {
				t.Errorf("added %s lacks %s:\n%s", tc.file, tc.want, src)
			}
		})
	}
}
