package appgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// A screen added to a project with api_prefix: v1 posts to the project's
// API, not to the fragment's empty prefix.
func TestAddedScreenUsesProjectAPIPrefix(t *testing.T) {
	base := strings.Replace(addTestAppSpec("example.com/addtest"), "  name: AddTest\n", "  name: AddTest\n  api_prefix: v1\n", 1)
	dir, spec := clitest.AddSetup(t, base)
	clitest.CovT_capStdout(t, func() { GenerateFromAppSpec(GenerateOptions{Spec: spec}) })
	fragment := filepath.Join(dir, "fragment.yml")
	clitest.WriteTestFile(t, fragment, "screens:\n  - name: new_post\n    route: /posts/write\n    title: Write\n    body:\n      - kind: entity_form\n        entity: posts\n        fields: [title]\n")
	clitest.CovT_capStdout(t, func() { GenerateFromAppSpec(GenerateOptions{Spec: fragment, add: true}) })
	src, err := os.ReadFile(filepath.Join(dir, "screen_new_post.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `interactive.Post("/v1/posts")`) {
		t.Errorf("added form does not post to /v1/posts:\n%s", src)
	}
}
