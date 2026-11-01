package uihost

import (
	"runtime/debug"
	"strings"
	"testing"
)

//go:noinline
func buildThatPanics() string { panic("build refused") }

// A panicking build is not memoized. The panic reaches the caller with
// the stack where it happened (the recovery middleware logs that stack,
// so it must name the culprit, not the memo), a later get builds again,
// and a build that then succeeds is cached.
func TestPerLocalePanicIsNotMemoized(t *testing.T) {
	var p perLocale[string]
	stack := func() (s string) {
		defer func() {
			if r := recover(); r != nil {
				s = string(debug.Stack())
			}
		}()
		p.get("en", buildThatPanics)
		return ""
	}
	for i := range 2 {
		s := stack()
		if s == "" {
			t.Fatalf("get %d: the build's panic did not reach the caller", i)
		}
		if !strings.Contains(s, "buildThatPanics") {
			t.Fatalf("get %d: the panic's stack does not name the build that panicked:\n%s", i, s)
		}
	}
	calls := 0
	ok := func() string { calls++; return "v" }
	if v := p.get("en", ok); v != "v" {
		t.Fatalf("get after the failures = %q, want a fresh build", v)
	}
	if v := p.get("en", ok); v != "v" || calls != 1 {
		t.Fatalf("a successful build was not memoized: %q after %d builds", v, calls)
	}
}
