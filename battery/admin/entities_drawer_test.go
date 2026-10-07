package admin

import (
	"testing"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A record and the create form both open as a drawer over their list;
// the list itself is a page.
func TestEntityScreensOpenAsDrawers(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig()}, Config{Entities: []string{"posts"}}, nil)
	posts, err := x.app.Registry.Get("posts")
	if err != nil {
		t.Fatal(err)
	}
	list := x.b.entityBase(posts)
	want := map[string]bool{list + "/create": false, list + "/:id": false}
	for _, r := range x.site.Routes() {
		if _, ok := want[r.Path]; !ok {
			if r.Path == list && r.Intercept != nil {
				t.Errorf("the list itself intercepts: %+v", r.Intercept)
			}
			continue
		}
		if r.Intercept == nil || r.Intercept.From != list || r.Intercept.As != appui.ScreenDrawer {
			t.Errorf("%s does not open as a drawer over %s: %+v", r.Path, list, r.Intercept)
		}
		want[r.Path] = true
	}
	for p, seen := range want {
		if !seen {
			t.Errorf("no screen at %s", p)
		}
	}
}
