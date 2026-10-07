package admin

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/schema"
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

// A record's drawer over its list steps to the list's neighbours, in the
// list's order, each step a link that swaps into the same drawer.
func TestRecordDrawerSteps(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig()}, Config{Entities: []string{"posts"}}, nil)
	for _, p := range [][2]string{{"p1", "Alpha"}, {"p2", "Bravo"}, {"p3", "Charlie"}} {
		x.insert("posts", map[string]any{"id": p[0], "title": p[1], "status": "draft"})
	}
	posts, err := x.app.Registry.Get("posts")
	if err != nil {
		t.Fatal(err)
	}
	list := x.b.entityBase(posts)
	req := httptest.NewRequest(http.MethodGet, list+"/p2", nil)
	req.Header.Set("X-Gofastr-Navigate", "1")
	req.Header.Set("X-Gofastr-Intercept", "1")
	req.Header.Set("X-Gofastr-From", list+"?sort=title&dir=desc")
	body := serve(x.as(theAdmin), req).Body.String()
	for label, want := range map[string]string{"Previous": list + "/p3", "Next": list + "/p1"} {
		tag := regexp.MustCompile(`<a[^>]*aria-label="` + label + ` record"[^>]*>`).FindString(body)
		if !strings.Contains(tag, `href="`+want+`"`) || !strings.Contains(tag, "data-cui-intercept-swap") {
			t.Errorf("%s step = %q, want a swap link to %s", label, tag, want)
		}
	}
}

// A comment's create form also opens over a post's record, whose Related
// tab adds comments; the post's create form opens over no record.
func TestCreateOpensOverRelatedRecord(t *testing.T) {
	comments := entity.EntityConfig{
		Table: "comments",
		Fields: []schema.Field{
			{Name: "body", Type: schema.String, Required: true},
			{Name: "post_id", Type: schema.Relation, To: "posts"},
		},
		Relations: []entity.Relation{entity.BelongsTo("post", "posts", "post_id")},
	}.WithTimestamps(false)
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig(), "comments": comments},
		Config{Entities: []string{"posts", "comments"}}, nil)
	base := func(name string) string {
		e, err := x.app.Registry.Get(name)
		if err != nil {
			t.Fatal(err)
		}
		return x.b.entityBase(e)
	}
	also := map[string][]string{}
	for _, r := range x.site.Routes() {
		if r.Intercept != nil {
			also[r.Path] = r.Intercept.AlsoFrom
		}
	}
	if got := also[base("comments")+"/create"]; !slices.Equal(got, []string{base("posts") + "/:id"}) {
		t.Errorf("comment create opens over %v, want the post record", got)
	}
	if got := also[base("posts")+"/create"]; len(got) != 0 {
		t.Errorf("post create opens over %v, want only its list", got)
	}
}
