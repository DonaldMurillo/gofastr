package appspec

import (
	"strings"
	"testing"
)

// A directory spec's parse and decode errors named no file:
// `gofastr validate specs/` answered "specs/: yaml:3:4: unexpected
// indentation" or "unknown key "feilds" in entities[0] at line 8" with
// every fragment a candidate for that line. Each error now carries the
// fragment's file name.
func TestDirSpecErrorNamesTheFragment(t *testing.T) {
	for _, tc := range []struct {
		bad, want string
	}{
		{"app:\n  name: Shop\n   module: x\n", "unexpected indentation"},
		{"entities:\n  - name: posts\n    feilds: []\n", `unknown key "feilds"`},
	} {
		dir := writeSpecDir(t, map[string]string{
			"00-app.yml":   "app:\n  name: Shop\n  module: example.com/shop\n",
			"10-posts.yml": tc.bad,
			"20-more.yml":  "entities:\n  - name: tags\n    fields:\n      - name: label\n        type: string\n",
		})
		_, err := LoadAppSpec(dir)
		if err == nil {
			t.Fatalf("LoadAppSpec accepted a directory holding %q", tc.bad)
		}
		if !strings.Contains(err.Error(), "10-posts.yml") || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("error does not name the fragment and the problem: %v", err)
		}
	}
}
