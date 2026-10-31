package desktop_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

// The runtime lets an external link navigate the window in place, and
// no shell had a navigation policy, so a foreign page ran in the app's
// window. The guard on every eval (TestEventsReachOnlyTheAppOrigin)
// keeps events from it; the shells now also refuse the navigation, so
// the window stays on the app's own pages (2026-10-27 L15, second half).
func TestWindowsStayOnTheAppOrigin(t *testing.T) {
	h, _ := chromeApp(t, desktop.Config{Title: "Nav"})
	for raw, want := range map[string]bool{
		h.URL("/"):                              true,
		h.URL("/notes?x=1#top"):                 true,
		"about:blank":                           true,
		"https://example.com/":                  false,
		"http://" + h.Addr() + ".evil/":         false,
		"http://" + h.Addr() + "@evil.example/": false,
		"https://" + h.Addr() + "/":             false,
		"http://localhost:1/":                   false,
		"file:///etc/passwd":                    false,
		"data:text/html,<p>x</p>":               false,
		"javascript:alert(1)":                   false,
		"about:srcdoc":                          false,
		"":                                      false,
		"http://[::1":                           false,
	} {
		if got := h.AllowsNavigation(raw); got != want {
			t.Errorf("AllowsNavigation(%q) = %v, want %v", raw, got, want)
		}
	}
}

// DangerouslyAllowForeignNavigation is the named escape hatch: every
// navigation goes through, as it did before the policy.
func TestForeignNavigationOptIn(t *testing.T) {
	h, _ := chromeApp(t, desktop.Config{Title: "Nav", DangerouslyAllowForeignNavigation: true})
	if !h.AllowsNavigation("https://example.com/") {
		t.Fatal("the opt-in still refused a foreign navigation")
	}
}
