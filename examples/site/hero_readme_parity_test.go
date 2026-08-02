package main

import (
	"os"
	"strings"
	"testing"
)

// The homepage hero tabs must stay byte-identical to the README
// Quickstart programs. CI compiles, boots, and curls the README copies
// (cmd/gofastr/readme_quickstart_test.go), so identity here means the
// site never shows a program that doesn't run.
func TestHeroTabsMatchReadmeQuickstart(t *testing.T) {
	raw, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	readme := string(raw)
	// Git may check this file out with CRLF on Windows, while the embedded Go
	// strings intentionally use LF. Compare program content, not checkout
	// newline style.
	readme = strings.ReplaceAll(readme, "\r\n", "\n")
	for _, tc := range []struct {
		heading, src string
	}{
		{"### Core only", heroCoreSrc},
		{"### Framework", heroFrameworkSrc},
		{"### Donald's Way", heroDonaldSrc},
	} {
		i := strings.Index(readme, tc.heading)
		if i < 0 {
			t.Fatalf("README heading %q missing — hero tabs are pinned to the Quickstart sections", tc.heading)
		}
		section := readme[i+len(tc.heading):]
		if j := strings.Index(section, "\n### "); j >= 0 {
			section = section[:j]
		}
		if !strings.Contains(section, tc.src) {
			t.Errorf("hero tab %q drifted from the README program — edit both or neither", tc.heading)
		}
	}
}
