package packcmd

import (
	"strings"
	"testing"
)

func TestYAMLKeyRejectReason_EachVectorIndependently(t *testing.T) {
	for _, tc := range []struct{ name, key, wantSubstr string }{
		{"empty", "", "empty key"},
		{"newline only", "a\nb", "control or invisible"},
		{"carriage return only", "a\rb", "control or invisible"},
		{"flow indicator only", "a[b", "flow indicators"},
		{"tab only", "a\tb", "control or invisible"},
		{"leading space only", " a", "edge whitespace"},
		{"trailing space only", "a ", "edge whitespace"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := yamlKeyRejectReason(tc.key); !strings.Contains(got, tc.wantSubstr) {
				t.Errorf("yamlKeyRejectReason(%q) = %q, want it to mention %q", tc.key, got, tc.wantSubstr)
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
