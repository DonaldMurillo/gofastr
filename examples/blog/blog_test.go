package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/battery/search"
	"github.com/DonaldMurillo/gofastr/framework"
)

// bootBlog wires the blog exactly as main() does (entities + migrate + seed)
// and returns the app router, without binding a port. This is the boot test
// the assessment flagged as missing, it would have caught the dead
// entity-loading path that made `go run ./examples/blog` fail.
func bootBlog(t *testing.T) *framework.App {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	app := framework.NewApp(
		framework.WithDB(db),
		framework.WithConfig(framework.AppConfig{Name: "blog-test"}),
	)
	registerEntities(app)
	app.Router().Get("/posts/published", postsByStatus(app, "published"))
	searchIndex := search.NewMemory()
	syncSearch(app, searchIndex)
	app.Router().Get("/posts/search", searchPosts(searchIndex))
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	seed(db)
	if err := indexPosts(context.Background(), app, searchIndex); err != nil {
		t.Fatalf("index posts: %v", err)
	}
	return app
}

// searchHits answers whether GET /posts/search?q= lists post id.
func searchHits(t *testing.T, app *framework.App, q, id string) bool {
	t.Helper()
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/posts/search?q="+q, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("search %q = %d, want 200", q, rec.Code)
	}
	return strings.Contains(rec.Body.String(), `"`+id+`"`)
}

// The index follows the posts table: a soft-deleted post stopped
// answering nothing, since the seed indexed it once and no write ever
// reached the index. A delete removes it, a restore and an edit
// re-index it, and a rebuild from the table leaves the trash out.
func TestBlogSearchFollowsWrites(t *testing.T) {
	app := bootBlog(t)
	posts, err := app.CrudHandler("posts")
	if err != nil {
		t.Fatal(err)
	}
	if !searchHits(t, app, "first", "p1") {
		t.Error("search missed a word of the post's body")
	}
	ctx := framework.WithServerWrites(context.Background())
	if err := posts.DeleteOne(ctx, "p1"); err != nil {
		t.Fatal(err)
	}
	if searchHits(t, app, "hello", "p1") {
		t.Error("search found the deleted post")
	}
	fresh := search.NewMemory()
	if err := indexPosts(context.Background(), app, fresh); err != nil {
		t.Fatal(err)
	}
	if res, _ := fresh.Search(context.Background(), search.Query{Text: "hello", Type: "posts"}); len(res) != 0 {
		t.Errorf("a rebuild indexed the deleted post: %v", res)
	}
	if err := posts.RestoreOne(ctx, "p1"); err != nil {
		t.Fatal(err)
	}
	if !searchHits(t, app, "hello", "p1") {
		t.Error("search missed the restored post")
	}
	if _, err := posts.UpdateOne(ctx, "p2", map[string]any{"title": "Zephyr"}); err != nil {
		t.Fatal(err)
	}
	if !searchHits(t, app, "zephyr", "p2") || searchHits(t, app, "gofastr", "p2") {
		t.Error("search kept the old title after the edit")
	}
	created, err := posts.CreateOne(ctx, map[string]any{"title": "Quokka", "status": "published"})
	if err != nil {
		t.Fatal(err)
	}
	if id, _ := created["id"].(string); id == "" || !searchHits(t, app, "quokka", id) {
		t.Errorf("search missed the created post %v", created["id"])
	}
}

// A write whose transaction rolls back never reaches the index: the
// hooks queue the index change behind the commit.
func TestBlogSearchSkipsRolledBackWrite(t *testing.T) {
	app := bootBlog(t)
	posts, err := app.CrudHandler("posts")
	if err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	err = app.InTx(framework.WithServerWrites(context.Background()), func(ctx context.Context, _ *sql.Tx) error {
		if err := posts.DeleteOne(ctx, "p1"); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("InTx = %v, want boom", err)
	}
	if !searchHits(t, app, "hello", "p1") {
		t.Error("a rolled-back delete removed the post from search")
	}
}

func TestBlogBoots(t *testing.T) {
	app := bootBlog(t)
	for _, path := range []string{"/posts", "/comments"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200. body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestBlogUsersFailClosed(t *testing.T) {
	app := bootBlog(t)
	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Errorf("anonymous GET /users = 200, want fail-closed (users carries email PII). body=%s", rec.Body.String())
	}
}

func TestBlogSeedAndPublishedFilter(t *testing.T) {
	app := bootBlog(t)
	req := httptest.NewRequest(http.MethodGet, "/posts/published", nil)
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("published = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Hello World") {
		t.Errorf("published list missing seeded post: %s", body)
	}
	if strings.Contains(body, "Work in progress") {
		t.Errorf("published filter leaked a draft post: %s", body)
	}
}

// TestBlogSearchHidesDrafts: the public search route answers only from
// published posts, the same line /posts/published draws. The draft is
// indexed (an editor tool could search it), so the route filters.
func TestBlogSearchHidesDrafts(t *testing.T) {
	app := bootBlog(t)
	for _, q := range []string{"draft", "progress"} {
		req := httptest.NewRequest(http.MethodGet, "/posts/search?q="+q, nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("search %q = %d, want 200", q, rec.Code)
		}
		if body := rec.Body.String(); strings.Contains(body, `"p3"`) {
			t.Errorf("search %q returned the draft post: %s", q, body)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/posts/search?q=hello", nil)
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `"p1"`) {
		t.Errorf("search hello missed the published post: %s", rec.Body)
	}
}
