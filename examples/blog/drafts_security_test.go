package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/search"
	"github.com/DonaldMurillo/gofastr/framework"
)

// TestBlogDraftsHiddenFromAnonymous: posts are public to read, and the
// seed's draft "won't appear on the public site", yet the auto-CRUD
// GET /posts and GET /posts/{id} handed it to anonymous callers; only
// the custom /posts/published and /posts/search routes filtered status.
// A read scope keeps anonymous reads to published posts.
func TestBlogDraftsHiddenFromAnonymous(t *testing.T) {
	app := bootBlog(t)
	for _, path := range []string{"/posts", "/posts?status=draft", "/posts/p3"} {
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if body := rec.Body.String(); strings.Contains(body, "Work in progress") {
			t.Errorf("SECURITY: [blog-drafts] anonymous GET %s returned the draft post (code %d): %s", path, rec.Code, body)
		}
	}
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/posts", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Hello World") {
		t.Errorf("anonymous GET /posts lost the published posts: %d %s", rec.Code, rec.Body)
	}
}

// TestBlogWritersReadDrafts: a caller who may write posts reads drafts,
// and the search rebuild still indexes them (the public search route
// filters its answers itself).
func TestBlogWritersReadDrafts(t *testing.T) {
	app := bootBlog(t)
	posts, err := app.CrudHandler("posts")
	if err != nil {
		t.Fatal(err)
	}
	policy := framework.NewRolePolicy()
	if err := policy.Grant("author", "posts:write"); err != nil {
		t.Fatal(err)
	}
	ctx := framework.WithRoles(framework.WithPolicy(context.Background(), policy), []string{"author"})
	row, err := posts.GetOne(ctx, "p3", nil)
	if err != nil || row["status"] != "draft" {
		t.Errorf("a posts:write caller could not read the draft: %v %v", row, err)
	}
	fresh := search.NewMemory()
	if err := indexPosts(context.Background(), app, fresh); err != nil {
		t.Fatal(err)
	}
	res, err := fresh.Search(context.Background(), search.Query{Text: "progress", Type: "posts"})
	if err != nil || len(res) != 1 {
		t.Errorf("the search rebuild skipped the draft: %v %v", res, err)
	}
}
