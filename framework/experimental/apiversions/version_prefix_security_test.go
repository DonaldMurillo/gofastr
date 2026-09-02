package apiversions_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/experimental/apiversions"
)

// Property: a version prefix is a single route segment, never a route
// pattern. normalizeVersion funnels every accepted prefix through
// validVersionRe (v<major>[.<minor>]) before the prefix becomes a route
// group, so path separators, dot-dot traversal, query glue, fragments,
// and whitespace cannot smuggle extra structure into the router (a
// "v1/admin" would mount the version's whole entity surface under an
// admin namespace it never asked for). This pins the fail-loud guard:
// each smuggle shape must panic at Version() time, not be silently
// trimmed into a mountable prefix.
func TestVersionPrefixRejectsSmuggling(t *testing.T) {
	for _, bad := range []string{
		"v1/admin",
		"v1/../v2",
		"../v1",
		"v1?x=1",
		"v1#frag",
		"v1 ",
		"v1%2fadmin",
	} {
		func(shape string) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("Version(%q) accepted a prefix carrying route structure; want a startup panic", shape)
				}
			}()
			_ = apiversions.Version(router.New(), shape)
		}(bad)
	}
}
