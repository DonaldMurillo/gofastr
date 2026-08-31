package main

import (
	"strings"
	"testing"
)

// Every branch of yamlKeyRejectReason needs its own kill. The two
// injection demonstrations both use a payload carrying a colon AND a
// newline, so the ':' case fires first and disabling the newline branch
// alone leaves them green — they pin "rejected", not "rejected for the
// reason that makes this vector work". Verified: neutering only the
// newline case survived both.
//
// Each payload below trips exactly one branch, so removing that branch
// turns exactly one subtest red and names it.
func TestYAMLKeyRejectReason_EachVectorIndependently(t *testing.T) {
	for _, tc := range []struct{ name, key, wantSubstr string }{
		{"empty", "", "empty key"},
		{"newline only", "a\nb", "line breaks"},
		{"carriage return only", "a\rb", "line breaks"},
		{"colon only", "a:b", "first ':'"},
		{"double quote leading", "\"status", "quote"},
		{"apostrophe interior", "it's", "quote"},
		{"hash only", "a#b", "comment"},
		{"flow indicator only", "a[b", "flow indicators"},
		{"list prefix only", "- a", "list item"},
		{"tab only", "a\tb", "tabs"},
		{"leading space only", " a", "edge whitespace"},
		{"trailing space only", "a ", "edge whitespace"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := yamlKeyRejectReason(tc.key)
			if got == "" {
				t.Fatalf("yamlKeyRejectReason(%q) accepted the key; want a rejection mentioning %q",
					tc.key, tc.wantSubstr)
			}
			if !strings.Contains(got, tc.wantSubstr) {
				t.Errorf("yamlKeyRejectReason(%q) = %q, want it to mention %q — a different branch "+
					"fired, so this vector is not independently pinned", tc.key, got, tc.wantSubstr)
			}
		})
	}
}

// Keys that must keep working. Over-rejection breaks real packs, which is
// worse than the bug this guard closes.
func TestYAMLKeyRejectReason_AcceptsRealKeys(t *testing.T) {
	for _, key := range []string{
		"name", "created_at", "data-label", "primary-fg", "text-muted",
		"icon", "-a", "a-b", "a.b", "a b", "Title", "id",
	} {
		if reason := yamlKeyRejectReason(key); reason != "" {
			t.Errorf("yamlKeyRejectReason(%q) = %q; this is a legitimate key shape", key, reason)
		}
	}
}
