package docs

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
		if mentions(attrCell(line), attr) {
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
		if !strings.Contains(row, strings.Join(sides, " → ")) {
			t.Errorf("%s must name the fallback order %s", doc.name, strings.Join(sides, " → "))
		}
		if strings.Contains(row, "auto-flips to the opposite") {
			t.Errorf("%s promises an opposite-side fallback the runtime does not implement", doc.name)
		}
	}
}

// hookDocumented accepts whole names and explicitly documented families.
// The prefix-ownership table is not an attribute reference and must not
// make every possible headless hook count as documented.
func hookDocumented(doc, hook string) bool {
	for line := range strings.SplitSeq(doc, "\n") {
		if !strings.HasPrefix(line, "| `data-") || strings.HasPrefix(line, "| `data-hui-*`") {
			continue
		}
		if mentions(line, hook) {
			return true
		}
		for _, family := range rowAttrs(line) {
			if strings.HasSuffix(family, "*") && strings.HasPrefix(hook, strings.TrimSuffix(family, "*")) {
				return true
			}
		}
	}
	return false
}

func TestHookDocsMatchNamesAndFamilies(t *testing.T) {
	for _, tc := range []struct {
		row, hook string
		want      bool
	}{
		{"| `data-hui-*` | ownership only |", "data-hui-json", false},
		{"| `data-hui-json-extra` | text |", "data-hui-json", false},
		{"| `data-hui-json` | text |", "data-hui-json", true},
		{"| `data-hui-combobox*` | text |", "data-hui-combobox-listbox", true},
		{"| `data-hui-sortable-s-*` | text |", "data-hui-sortable-live", false},
		{"| `data-hui-sortable-s-*` | text |", "data-hui-sortable-s-saved", true},
	} {
		if got := hookDocumented(tc.row, tc.hook); got != tc.want {
			t.Errorf("hookDocumented(%q, %q) = %v; want %v", tc.row, tc.hook, got, tc.want)
		}
	}
}

// Both literal attributes and dataset properties are runtime contracts.
// Like headless's binding gate, strip comments before collecting hooks;
// prose mentioning a removed hook is not an implementation of it.
//
//gofastr:test-reads core-ui/ARCHITECTURE.md
//gofastr:test-reads framework/headless/*.js
func TestHeadlessHooksHaveDocRows(t *testing.T) {
	paths, err := filepath.Glob("../headless/*.js")
	if err != nil || len(paths) == 0 {
		t.Fatalf("headless modules not found: %v", err)
	}
	block := regexp.MustCompile(`(?s)/\*.*?\*/`)
	line := regexp.MustCompile(`//[^\n]*`)
	literal := regexp.MustCompile(`data-hui-[a-z0-9-]+`)
	dataset := regexp.MustCompile(`dataset\.(hui[A-Za-z0-9]+)`)
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		js := line.ReplaceAllString(block.ReplaceAllString(string(src), " "), " ")
		hooks := literal.FindAllString(js, -1)
		for _, m := range dataset.FindAllStringSubmatch(js, -1) {
			var b strings.Builder
			b.WriteString("data-")
			for _, r := range m[1] {
				if r >= 'A' && r <= 'Z' {
					b.WriteByte('-')
					r += 'a' - 'A'
				}
				b.WriteRune(r)
			}
			hooks = append(hooks, b.String())
		}
		slices.Sort(hooks)
		hooks = slices.Compact(hooks)
		for _, doc := range contractDocs(t) {
			for _, hook := range hooks {
				// A concatenated prefix is not a complete attribute.
				if strings.HasSuffix(hook, "-") {
					continue
				}
				if !hookDocumented(doc.text, hook) {
					t.Errorf("%s lacks a row for %s (%s)", doc.name, hook, filepath.Base(path))
				}
			}
		}
	}
	for _, doc := range contractDocs(t) {
		if mentions(doc.text, "data-wizard-steps") {
			t.Errorf("%s documents an unimplemented wizard hook", doc.name)
		}
	}
}

// Input-triggered RPCs also bind non-form carriers such as comboboxes.
// Keep the source-of-truth row as broad as the embedded reference.
//
//gofastr:test-reads core-ui/ARCHITECTURE.md
func TestRPCCarrierDocsMatch(t *testing.T) {
	arch := attributeRow(t, readArchitecture(t), "data-cui-rpc-trigger")
	extract := attributeRow(t, readRuntimeContract(t), "data-cui-rpc-trigger")
	if arch != extract {
		t.Error("RPC input carrier rows disagree between the references")
	}
}

// Explicit rows must survive prefix collisions and every attribute family.
func TestAttrSyncMatchesWholeNames(t *testing.T) {
	for _, attr := range []string{"data-cui-compute", "data-hui-combobox*", "data-search-all-label", "data-custom-ready"} {
		src := "| `" + attr + "` | contract |"
		if got := missingFrom(src, "`"+attr+"d`"); len(got) != 1 || got[0] != attr {
			t.Errorf("%s incorrectly matched a longer name: %v", attr, got)
		}
		if got := missingFrom(src, "`"+attr+"` "); len(got) != 0 {
			t.Errorf("%s failed an exact match: %v", attr, got)
		}
	}
}

func TestAttrSyncKeepsEscapedPipeCells(t *testing.T) {
	src := "| `data-hui-step-wizard-action=\"next\\|back\"` / `data-hui-step-wizard-status` | No `data-body-mention` row. |"
	want := []string{"data-hui-step-wizard-action", "data-hui-step-wizard-status"}
	if got := rowAttrs(src); !slices.Equal(got, want) {
		t.Errorf("row attributes = %v; want %v", got, want)
	}
	if got := missingFrom(src, "`data-hui-step-wizard-action`"); !slices.Equal(got, want[1:]) {
		t.Errorf("missing attributes = %v; want %v", got, want[1:])
	}
}

//gofastr:test-reads core-ui/ARCHITECTURE.md
func TestSortableDocsMatch(t *testing.T) {
	arch := attributeRow(t, readArchitecture(t), "data-hui-sortable")
	extract := attributeRow(t, readRuntimeContract(t), "data-hui-sortable")
	if arch != extract {
		t.Error("sortable rows disagree between the references")
	}
}
