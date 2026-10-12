package admin

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/auth"
)

var userRowRe = regexp.MustCompile(`<tr id="[^"]+" role="row"><td data-label="User" role="cell">([^<]+)</td>`)

// usersShown lists the emails a User roles page draws, in order.
func usersShown(body string) []string {
	var out []string
	for _, m := range userRowRe.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// A huge ?p= never turns the store's offset negative: Postgres refuses a
// negative OFFSET, which would fail the page instead of showing the last.
func TestUsersHugePageOffsetStays(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/admin/rbac/users?limit=500&p=9223372036854775807", nil)
	if opts, _ := listUsersOpts(r); opts.Offset < 0 {
		t.Errorf("offset %d for a huge page", opts.Offset)
	}
}

// The User roles page pages through every account, a page at a time,
// in the store's order; a page turn keeps the page size, and a page past
// the end shows the last one.
func TestUsersPagePagesAccounts(t *testing.T) {
	r := newRBACEnv(t, Config{})
	lister, ok := r.mgr.UserStore().(interface {
		CreateUser(context.Context, string, string, []string) (auth.User, error)
	})
	if !ok {
		t.Fatal("the test user store cannot create users")
	}
	for _, email := range []string{"c@example.com", "d@example.com", "e@example.com"} {
		if _, err := lister.CreateUser(context.Background(), email, "$2a$10$hash", nil); err != nil {
			t.Fatal(err)
		}
	}
	var seen []string
	for _, c := range []struct {
		query string
		n     int
	}{{"limit=2", 2}, {"limit=2&p=2", 2}, {"limit=2&p=3", 1}} {
		got := usersShown(get(r.as(theAdmin), "/admin/rbac/users?"+c.query).Body.String())
		if len(got) != c.n {
			t.Errorf("%s shows %v, want %d accounts", c.query, got, c.n)
		}
		seen = append(seen, got...)
	}
	slices.Sort(seen)
	if want := []string{"admin@example.com", "c@example.com", "d@example.com", "e@example.com", "editor@example.com"}; !slices.Equal(seen, want) {
		t.Errorf("the three pages showed %v, want every account once", seen)
	}
	last := usersShown(get(r.as(theAdmin), "/admin/rbac/users?limit=2&p=3").Body.String())
	for _, p := range []string{"99", "9223372036854775807"} {
		if got := usersShown(get(r.as(theAdmin), "/admin/rbac/users?limit=2&p="+p).Body.String()); !slices.Equal(got, last) {
			t.Errorf("page %s shows %v, want the last page %v", p, got, last)
		}
	}
	body := get(r.as(theAdmin), "/admin/rbac/users?limit=2").Body.String()
	link := regexp.MustCompile(`<a[^>]*href="([^"]*)"[^>]*>3</a>`).FindStringSubmatch(body)
	if link == nil {
		t.Fatalf("the first page has no link to page 3:\n%s", body)
	}
	if href := html.UnescapeString(link[1]); !strings.Contains(href, "limit=2") || !strings.Contains(href, "p=3") {
		t.Errorf("page 3's link %q drops the page size or the page", href)
	}
}
