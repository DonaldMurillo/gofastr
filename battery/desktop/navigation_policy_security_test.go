package desktop_test

import (
	"os"
	"strings"
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

// WebView2 answers window.open and a target=_blank link with a second
// window: a web view with none of the shell's handlers, so the page
// could open a foreign site in a window that looks like the app's. The
// Windows shell registers NewWindowRequested beside its navigation
// policy and marks every request handled, so no window opens, as on
// macOS and Linux. No Windows runner exists, so this reads the source.
//
//gofastr:test-reads /battery/desktop/windows/shell_windows.go
func TestWindowsShellRefusesNewWindows(t *testing.T) {
	src, err := os.ReadFile("windows/shell_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	start := strings.Index(s, "func (s *winShell) installNavigationPolicy(")
	if start < 0 {
		t.Fatal("installNavigationPolicy not found")
	}
	policy := s[start:]
	policy = policy[:strings.Index(policy, "\n}\n")]
	if !strings.Contains(policy, "return installNewWindowPolicy(w)") {
		t.Error("installNavigationPolicy does not install the new-window policy")
	}
	nw := s[strings.Index(s, "func installNewWindowPolicy("):]
	nw = nw[:strings.Index(nw, "\n}\n")]
	for _, want := range []string{
		"44 /* add_NewWindowRequested */",
		"iidNewWindowRequested",
	} {
		if !strings.Contains(nw, want) {
			t.Errorf("installNewWindowPolicy lacks %q", want)
		}
	}
	// Handled is set before anything that can fail or panic.
	handled := strings.Index(nw, "6 /* put_Handled */, 1)")
	if handled < 0 || handled > strings.Index(nw, "defer func()") || handled > strings.Index(nw, "get_Uri") {
		t.Error("the handler does not set Handled first, unconditionally")
	}
}
