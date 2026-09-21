package admin

// Tests for the entity list's data-grid capabilities: column sorting and a
// search box. Both must be server-side (the CrudHandler does the SQL) and
// island-swapped through the /_rows endpoint, no client-side sort/filter math
// (per the framework's hard rules).

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func seedTitles(h http.Handler, titles ...string) {
	for _, ti := range titles {
		postForm(h, "/admin/e/posts/_create", url.Values{"title": {ti}, "status": {"draft"}})
	}
}

// indexOrder returns the byte offsets of each needle in body, or -1 if absent.
func indexOrder(body string, needles ...string) []int {
	out := make([]int, len(needles))
	for i, n := range needles {
		out[i] = strings.Index(body, n)
	}
	return out
}

func TestEntity_ListSortByColumnAsc(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"posts"}}, testUser{"u1"})
	seedTitles(h, "Cherry", "Apple", "Banana")

	rr := get(h, "/admin/e/posts/_rows?sort=title&dir=asc")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	pos := indexOrder(rr.Body.String(), "Apple", "Banana", "Cherry")
	for i, p := range pos {
		if p < 0 {
			t.Fatalf("row %d missing from sorted body", i)
		}
	}
	if !(pos[0] < pos[1] && pos[1] < pos[2]) {
		t.Fatalf("ascending sort not applied: Apple@%d Banana@%d Cherry@%d", pos[0], pos[1], pos[2])
	}
}

func TestEntity_ListSortByColumnDesc(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"posts"}}, testUser{"u1"})
	seedTitles(h, "Apple", "Cherry", "Banana")

	body := get(h, "/admin/e/posts/_rows?sort=title&dir=desc").Body.String()
	pos := indexOrder(body, "Cherry", "Banana", "Apple")
	if !(pos[0] < pos[1] && pos[1] < pos[2]) {
		t.Fatalf("descending sort not applied: Cherry@%d Banana@%d Apple@%d", pos[0], pos[1], pos[2])
	}
}

func TestEntity_ListRendersSortableHeaders(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"posts"}}, testUser{"u1"})
	seedTitles(h, "One")

	body := get(h, "/admin/e/posts").Body.String()
	// The title column header must offer a server-side sort affordance.
	if !strings.Contains(body, "sort=title") {
		t.Fatalf("list should render a sortable 'title' header (sort=title); got %q", body)
	}
}

func TestEntity_ListSearchFiltersRows(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"posts"}}, testUser{"u1"})
	seedTitles(h, "Apple pie", "Banana bread")

	body := get(h, "/admin/e/posts/_rows?q=Apple").Body.String()
	if !strings.Contains(body, "Apple pie") {
		t.Fatalf("search should keep the matching row; got %q", body)
	}
	if strings.Contains(body, "Banana bread") {
		t.Fatalf("search should filter out non-matching rows; got %q", body)
	}
}

func TestEntity_ListRendersSearchBox(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"posts"}}, testUser{"u1"})
	seedTitles(h, "One")

	body := get(h, "/admin/e/posts").Body.String()
	if !strings.Contains(body, `name="q"`) {
		t.Fatalf("list should render a search input (name=\"q\"); got %q", body)
	}
}

func TestEntity_ListPageOutOfRangeIsTheLastPage(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	// Limit 4 over 5 rows → 2 pages; page 2 holds the fifth post.
	h := mountEntityAdmin(t, app, Config{Entities: []string{"posts"}, EntityListLimit: 4}, testUser{"u1"})
	seedTitles(h, "Alpha", "Bravo", "Charlie", "Delta", "Echo")

	for _, tc := range []struct {
		raw       string
		wantPage  string
		wantFirst string
	}{
		{"/admin/e/posts?p=999", "2", "Echo"},
		{"/admin/e/posts?p=0", "1", "Alpha"},
		{"/admin/e/posts?p=-3", "1", "Alpha"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			// The typed pager refuses a page outside 1..Pages: without
			// the clamp the panic escapes renderTable and fails here.
			body := get(h, tc.raw).Body.String()
			if !strings.Contains(body, ">"+tc.wantFirst+"<") {
				t.Fatalf("[page-clamp] request %q did not render page %s's first row %q", tc.raw, tc.wantPage, tc.wantFirst)
			}
			m := regexp.MustCompile(`<a aria-current="page" href="([^"]*)"`).FindStringSubmatch(body)
			if m == nil {
				t.Fatalf("[page-clamp] request %q rendered no current page anchor", tc.raw)
			}
			if u, err := url.Parse(m[1]); err != nil || u.Query().Get("p") != tc.wantPage {
				t.Fatalf("[page-clamp] request %q: the pager's current page href is %q, want p=%s", tc.raw, m[1], tc.wantPage)
			}
		})
	}
}
