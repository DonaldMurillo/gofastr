package app

import (
	"strings"
	"testing"
)

// A browser deletes tab, CR and LF from a Location before it parses it,
// so a substituted redirect target of "/\t/evil.example" is the
// protocol-relative "//evil.example". ResolveRedirect fails closed on a
// control byte in the substituted value, the way it already does for a
// "//host" or backslash-bearing one.
func TestResolveRedirectRefusesControlBytes(t *testing.T) {
	a := NewApp("t")
	a.RedirectPattern("/old/{rest...}", "/{rest...}")
	for _, p := range []string{
		"/old/\t/evil.example",
		"/old/\n/evil.example",
		"/old/\r/evil.example",
		"/old/a\x01b",
		"/old/a\x7fb",
	} {
		if target, ok := a.ResolveRedirect(p); ok {
			t.Errorf("SECURITY: [app] ResolveRedirect(%q) = %q; a control byte in the substituted target must fail closed", p, target)
		}
	}
	if got, ok := a.ResolveRedirect("/old/docs/intro"); !ok || got != "/docs/intro" {
		t.Errorf("ResolveRedirect(/old/docs/intro) = %q, %v; want /docs/intro, true", got, ok)
	}
}

func TestRedirectTargetControlBytePanics(t *testing.T) {
	a := NewApp("t")
	for _, bad := range []string{"/\t/evil.com", "/a\nb", "/a\x00b"} {
		msg := panicMsg(t, func() { a.Redirect("/old", bad) })
		if !strings.Contains(msg, "control byte") {
			t.Errorf("control-byte guard for %q did not fire: %s", bad, msg)
		}
	}
}
