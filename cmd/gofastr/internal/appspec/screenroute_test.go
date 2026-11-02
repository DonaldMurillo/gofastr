package appspec

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// Each route below passed ValidateAppSpec and generated an app that built
// and booted, and then the screen could not be reached: "café" mounted no
// path the browser can ask for (and the sitemap listed
// "http://localhost:8080café"), "/x?y=1" and "/x#frag" are only reachable
// with the ? or # percent-encoded, "/x/../admin" is folded to /admin by
// every browser, "//evil.com" is a protocol-relative href in any link to
// it, "/x/{id}/{id}" drops one value, and "/api/posts" stopped the boot
// with the framework's entity-collision panic.
func TestScreenRoutesRefused(t *testing.T) {
	posts := framework.EntityDeclaration{Name: "posts", Exposure: &framework.ExposureDeclaration{CRUD: new(true)}, Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}}
	cases := []struct {
		route, prefix string
		want          string
	}{
		{"café", "api", "start with /"},
		{"/a//b", "api", "empty segment"},
		{"//evil.com", "api", "empty segment"},
		{"/x?y=1", "api", "?"},
		{"/x#frag", "api", "#"},
		{"/a b", "api", "space or control"},
		{"/a\tb", "api", "space or control"},
		{"/x\\y", "api", "backslash"},
		{"/x/../admin", "api", `"." or ".." segment`},
		{"/x/./y", "api", `"." or ".." segment`},
		{"/x/{id}/{id}", "api", "{id} twice"},
		{"/api/posts", "api", "/api/posts"},
		{"/api/posts/", "api", "/api/posts"},
		// Under the mount, CRUD's own GET patterns ({id}, llm.md,
		// _events, a composite key's segments) win over the screen, which
		// UIHost serves from its catch-all: GET /api/posts/abc answered
		// the CRUD 401 instead of the page.
		{"/api/posts/{id}", "api", "/api/posts"},
		{"/api/posts/{slug}", "api", "/api/posts"},
		{"/api/posts/llm.md", "api", "/api/posts"},
		{"/api/posts/a/b", "api", "/api/posts"},
		{"/posts", "", "/posts"},
	}
	for _, tc := range cases {
		bp := AppSpec{
			App:      AppSpecApp{Name: "Routes", APIPrefix: tc.prefix},
			Entities: []framework.EntityDeclaration{posts},
			Screens:  []AppSpecScreen{{Name: "page", Route: tc.route, Body: []AppSpecBlock{{Type: "heading", Text: "Page"}}}},
		}
		err := ValidateAppSpec(bp)
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), `screen "page"`) {
			t.Errorf("route %q (api_prefix %q): got %v, want an error naming %q", tc.route, tc.prefix, err, tc.want)
		}
	}
	for _, ok := range []string{"/", "/posts", "/café", "/a/{id}", "/a/:id", "/docs/{path...}", "/app/posts/{id}/edit", "/api/reports", "/api/postsx"} {
		bp := AppSpec{
			App:      AppSpecApp{APIPrefix: "api"},
			Entities: []framework.EntityDeclaration{posts},
			Screens:  []AppSpecScreen{{Name: "page", Route: ok, Body: []AppSpecBlock{{Type: "heading", Text: "Page"}}}},
		}
		if err := ValidateAppSpec(bp); err != nil {
			t.Errorf("route %q is reachable and should pass: %v", ok, err)
		}
	}
}
