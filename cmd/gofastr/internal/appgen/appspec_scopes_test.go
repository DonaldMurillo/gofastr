package appgen

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/core/settings"
)

func TestPhraseScopesNeverFold(t *testing.T) {
	bp := appspec.AppSpec{Screens: []appspec.AppSpecScreen{{Name: "PostsList", Route: "/a"}, {Name: "postslist", Route: "/b"}}}
	scopes := appSpecPhraseScopes(bp)
	if a, b := scopes["screen:PostsList"], scopes["screen:postslist"]; a == b {
		t.Fatalf("two screens share the phrase scope %q: i18n.Define panics on one key with two texts", a)
	}
}

func TestMergeEndpointsKeepsTheProjectsEntry(t *testing.T) {
	project := []settings.Endpoint{{Name: "publish", Entity: "posts", Method: "POST"}}
	fragment := []settings.Endpoint{{Name: "publish", Entity: "posts", Method: "GET"}, {Name: "archive", Entity: "posts", Method: "POST"}}
	got := mergeEndpoints(project, fragment)
	if len(got) != 2 || got[0].Method != "POST" || got[1].Name != "archive" {
		t.Fatalf("merge = %+v, want the project's publish then the fragment's archive", got)
	}
}
