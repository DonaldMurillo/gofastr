package docs

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func contractDocs(t *testing.T) []struct{ name, text string } {
	t.Helper()
	return []struct{ name, text string }{
		{"architecture", readArchitecture(t)},
		{"runtime-contract", readRuntimeContract(t)},
	}
}

func attributeRow(t *testing.T, doc, attr string) string {
	t.Helper()
	for line := range strings.SplitSeq(doc, "\n") {
		if !strings.HasPrefix(line, "| `data-") {
			continue
		}
		cells := strings.SplitN(line, "|", 3)
		if len(cells) == 3 && mentions(cells[1], attr) {
			return line
		}
	}
	t.Fatalf("missing attribute row for %s", attr)
	return ""
}

// The TOC target is the observer's region, not its heading selector.
// Read the default from the shared observer so future heading levels
// cannot silently drift in either shipped reference.
//
//gofastr:test-reads core-ui/ARCHITECTURE.md
//gofastr:test-reads framework/headless/rail.js
func TestRailDocsDescribeObservedRegions(t *testing.T) {
	src, err := os.ReadFile("../headless/rail.js")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`targetSel = targetSel \|\| '([^']+)'`).FindSubmatch(src)
	if m == nil {
		t.Fatal("shared rail default selector not found")
	}
	for _, doc := range contractDocs(t) {
		t.Run(doc.name, func(t *testing.T) {
			for _, attr := range []string{"data-hui-rail-target", "data-hui-toc-target"} {
				row := attributeRow(t, doc.text, attr)
				if !strings.Contains(row, string(m[1])) {
					t.Errorf("%s must document heading default %q", attr, m[1])
				}
				if !strings.Contains(row, "content region") {
					t.Errorf("%s must identify the observed content region", attr)
				}
			}
		})
	}
}

//gofastr:test-reads core-ui/ARCHITECTURE.md
//gofastr:test-reads core-ui/runtime/src/popover.js
func TestPopoverDocsNameFallbackOrder(t *testing.T) {
	src, err := os.ReadFile("../../core-ui/runtime/src/popover.js")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`: \[pref, ([^\]]+)\]`).FindSubmatch(src)
	if m == nil {
		t.Fatal("popover fallback order not found")
	}
	var sides []string
	for _, side := range regexp.MustCompile(`'([^']+)'`).FindAllSubmatch(m[1], -1) {
		sides = append(sides, string(side[1]))
	}
	if len(sides) != 4 {
		t.Fatalf("expected four fallback sides, got %v", sides)
	}
	for _, doc := range contractDocs(t) {
		row := attributeRow(t, doc.text, "data-cui-popover-anchor")
		if !strings.Contains(row, "preferred side first") || !strings.Contains(row, strings.Join(sides, " → ")) {
			t.Errorf("%s must describe the preferred side first, then %s", doc.name, strings.Join(sides, " → "))
		}
		if strings.Contains(row, "auto-flips to the opposite") {
			t.Errorf("%s promises an opposite-side fallback the runtime does not implement", doc.name)
		}
	}
}
