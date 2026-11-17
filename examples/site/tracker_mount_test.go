package main

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// TestTrackerMountsUnderExamples pins the tracker's mount in the site:
// every page answers under /examples/tracker, and no link on them leaves
// the prefix for a tracker route the site does not serve (an unprefixed
// "/inbox" would land on the site's 404).
func TestTrackerMountsUnderExamples(t *testing.T) {
	href := regexp.MustCompile(`(?:href|action)="(/[^"]*)"`)
	for _, path := range []string{
		"/examples/tracker",
		"/examples/tracker/inbox",
		"/examples/tracker/projects",
		"/examples/tracker/projects/billing",
		"/examples/tracker/projects/billing/issues/42",
		"/examples/tracker/reports",
		"/examples/tracker/settings",
	} {
		rec := serve(t, http.MethodGet, path)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status %d, want 200", path, rec.Code)
			continue
		}
		body := rec.Body.String()
		if !strings.Contains(body, "Acme Tracker") {
			t.Errorf("%s: no tracker shell in the page", path)
		}
		for _, m := range href.FindAllStringSubmatch(body, -1) {
			for _, bare := range []string{"/inbox", "/projects", "/reports", "/settings"} {
				if m[1] == bare || strings.HasPrefix(m[1], bare+"/") || strings.HasPrefix(m[1], bare+"?") {
					t.Errorf("%s: link %q escapes the /examples/tracker prefix", path, m[1])
				}
			}
		}
	}
}
