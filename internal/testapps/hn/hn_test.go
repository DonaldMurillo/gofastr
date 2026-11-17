package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The app is built once per test binary: seeding writes a few thousand
// rows, and every test reads the same data.
var srv http.Handler

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	dir, err := os.MkdirTemp("", "hn-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(dir)
	app, err := buildApp(filepath.Join(dir, "hn.db"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer app.DB.Close()
	srv = app.Router()
	return m.Run()
}

func itoa(n int) string { return strconv.Itoa(n) }

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func handler(t *testing.T) http.Handler {
	t.Helper()
	return srv
}

func get(t *testing.T, path string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	handler(t).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	b, _ := io.ReadAll(rec.Result().Body)
	return rec.Code, string(b)
}

func TestListsServeFirstAndLastPage(t *testing.T) {
	for _, tc := range []struct {
		path  string
		pages int
	}{
		{"/", 10}, {"/news", 10}, {"/newest", 12}, {"/ask", 2}, {"/show", 2}, {"/jobs", 1},
	} {
		for _, page := range []int{1, tc.pages} {
			url := tc.path
			if page > 1 {
				url += "?page=" + itoa(page)
			}
			code, body := get(t, url)
			if code != http.StatusOK {
				t.Errorf("GET %s = %d, want 200", url, code)
				continue
			}
			if n := strings.Count(body, `class="fui-entry-list__item"`); n != perPage {
				t.Errorf("GET %s: %d rows, want %d", url, n, perPage)
			}
			start := `start="` + itoa((page-1)*perPage+1) + `"`
			if !strings.Contains(body, start) {
				t.Errorf("GET %s: list does not number from %s", url, start)
			}
		}
	}
}

func TestPagerLinksAreRealAnchors(t *testing.T) {
	_, body := get(t, "/?page=2")
	for _, want := range []string{`href="/?page=1"`, `href="/?page=3"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page 2 has no %s anchor", want)
		}
	}
}

func TestPageOutOfRangeIs404(t *testing.T) {
	for _, url := range []string{"/?page=11", "/newest?page=13", "/ask?page=3", "/jobs?page=2", "/?page=0", "/?page=x"} {
		if code, _ := get(t, url); code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", url, code)
		}
	}
}

func TestUnknownItemAndUserAre404(t *testing.T) {
	for _, url := range []string{"/item/1", "/user/nobody-here", "/nope"} {
		if code, _ := get(t, url); code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", url, code)
		}
	}
}

var itemHref = regexp.MustCompile(`href="/item/(\d+)"`)

// busiest is the story with the most comments on the front page.
func busiest(t *testing.T) string {
	t.Helper()
	_, body := get(t, "/")
	best, most := "", -1
	for _, m := range regexp.MustCompile(`href="/item/(\d+)">(\d+)\x{a0}comments`).FindAllStringSubmatch(body, -1) {
		if n := atoi(m[2]); n > most {
			best, most = m[1], n
		}
	}
	if best == "" {
		t.Fatal("no comment links on the front page")
	}
	return best
}

func TestItemPageNestsComments(t *testing.T) {
	id := busiest(t)
	code, body := get(t, "/item/"+id)
	if code != http.StatusOK {
		t.Fatalf("GET /item/%s = %d", id, code)
	}
	if n := strings.Count(body, "<article"); n < 80 {
		t.Errorf("busiest story shows %d comments, want 80+", n)
	}
	if d := maxDepth(body); d < 5 {
		t.Errorf("deepest reply is %d levels down, want 5+", d)
	}
}

// maxDepth is how many reply lists the deepest comment sits in.
func maxDepth(body string) int {
	depth, best := 0, 0
	for i := 0; i < len(body); {
		open := strings.Index(body[i:], `<ul class="fui-comment-thread__replies"`)
		end := strings.Index(body[i:], `</ul>`)
		switch {
		case open >= 0 && (end < 0 || open < end):
			depth++
			best = max(best, depth)
			i += open + 1
		case end >= 0:
			depth = max(depth-1, 0)
			i += end + 1
		default:
			i = len(body)
		}
	}
	return best
}

func TestAskItemLinksToItself(t *testing.T) {
	_, body := get(t, "/ask")
	m := itemHref.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no item links on /ask")
	}
	if !strings.Contains(body, `class="fui-entry-list__title" href="/item/`+m[1]+`"`) {
		t.Errorf("Ask titles should link to their own item page")
	}
}

func TestUserPageShowsProfile(t *testing.T) {
	_, body := get(t, "/")
	m := regexp.MustCompile(`href="/user/([^"]+)"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no user links on the front page")
	}
	code, page := get(t, "/user/"+m[1])
	if code != http.StatusOK {
		t.Fatalf("GET /user/%s = %d", m[1], code)
	}
	for _, want := range []string{m[1], "Created", "Karma"} {
		if !strings.Contains(page, want) {
			t.Errorf("user page missing %q", want)
		}
	}
}

func TestSameSeedSameBytes(t *testing.T) {
	a, b := generate(), generate()
	if len(a.comments) != len(b.comments) || a.stories[7].Title != b.stories[7].Title {
		t.Fatal("generate is not deterministic")
	}
	if n := len(a.comments); n < 2000 || n > 12000 {
		t.Errorf("comments = %d, want a few thousand", n)
	}
	for _, s := range a.stories {
		if n := len(s.Title); n < 40 || n > 90 {
			t.Errorf("title %q is %d chars", s.Title, n)
		}
	}
}
