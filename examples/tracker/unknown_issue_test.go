package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// S6: an issue id that does not exist (/projects/legacy/issues/LEG-12,
// any unknown number) renders "Issue not found" inside the project,
// with the PROJECT's toolbar (New issue — the group fill the screen
// fill declined to) and a breadcrumb that ends at the project.
func TestUnknownIssueShowsProjectToolbarAndCrumbs(t *testing.T) {
	trackerEnv(t)
	app := buildApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)

	for _, path := range []string{"/projects/legacy/issues/LEG-12", "/projects/billing/issues/999"} {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		page := string(body)
		if res.StatusCode != 200 {
			t.Errorf("%s: status %d, want 200", path, res.StatusCode)
		}
		if !strings.Contains(page, "Issue not found") {
			t.Errorf("%s: the page does not say the issue is missing", path)
		}
		// The project's toolbar, never the issue's actions on nothing.
		if !strings.Contains(page, "New issue") {
			t.Errorf("%s: the project toolbar (New issue) is missing", path)
		}
		for _, gone := range []string{"Close issue", "Change status", `>Assign<`} {
			if strings.Contains(page, gone) {
				t.Errorf("%s: the issue toolbar (%q) rendered for a nonexistent issue", path, gone)
			}
		}
		// The breadcrumb ends at the project: the project crumb is the
		// current one and no issue-key crumb follows it.
		toolbar := page[strings.Index(page, `data-fui-area="l:shell~crumbs"`):]
		end := strings.Index(toolbar, "</nav>")
		if end >= 0 {
			toolbar = toolbar[:end]
		}
		if !strings.Contains(toolbar, `aria-current="page"`) {
			t.Errorf("%s: no current crumb in %q", path, toolbar)
		}
		for _, notACrumb := range []string{"LEG-12", "BIL-999"} {
			if strings.Contains(toolbar, notACrumb) {
				t.Errorf("%s: the crumb trail names the nonexistent issue %q", path, notACrumb)
			}
		}
	}
}
