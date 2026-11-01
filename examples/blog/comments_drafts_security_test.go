package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// TestBlogDraftCommentsHidden: posts hide drafts from anonymous reads,
// but a comment on a draft post stayed readable through /comments, which
// told anyone the draft existed and quoted the discussion of it.
func TestBlogDraftCommentsHidden(t *testing.T) {
	app := bootBlog(t)
	if _, err := app.DB.Exec(`INSERT INTO comments (id, body, post_id, author_id) VALUES ('c3', 'Secret draft note', 'p3', 'u2')`); err != nil {
		t.Fatal(err)
	}
	get := func(path string) (int, string) {
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec.Code, rec.Body.String()
	}
	for _, path := range []string{"/comments", "/comments?post_id=p3", "/comments/c3", "/comments?include=post", "/comments?count=true"} {
		if code, body := get(path); strings.Contains(body, "Secret draft note") || strings.Contains(body, `"c3"`) {
			t.Errorf("SECURITY: [blog-drafts] anonymous GET %s returned a draft post's comment (code %d): %s", path, code, body)
		}
	}
	if code, body := get("/comments"); code != http.StatusOK || !strings.Contains(body, "Great post!") {
		t.Errorf("anonymous GET /comments lost a published post's comment: %d %s", code, body)
	}
	if code, body := get("/posts/p1?include=comments"); code != http.StatusOK || !strings.Contains(body, "Great post!") {
		t.Errorf("a published post's included comments went missing: %d %s", code, body)
	}

	// A caller who may write posts reads drafts, and their comments.
	policy := framework.NewRolePolicy()
	if err := policy.Grant("author", "posts:write"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/comments/c3", nil)
	req = req.WithContext(framework.WithRoles(framework.WithPolicy(req.Context(), policy), []string{"author"}))
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Secret draft note") {
		t.Errorf("a posts:write caller could not read a draft's comment: %d %s", rec.Code, rec.Body)
	}
}
