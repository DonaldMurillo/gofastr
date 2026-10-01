package analyzers_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// The program grouping of the owned-style rules (see
// ownedstyleprograms.go): a program is a main package plus the
// transitive closure of its imports that resolve inside the pass, and
// the program-wide rules (1816, 1822) judge only sheets one program
// can link. Sheets no program reaches form one group together.

// mainImporting is a package main importing the given packages, so
// the grouping links their directories into one program.
func mainImporting(imports ...string) string {
	var b strings.Builder
	b.WriteString("package main\n\nimport (\n")
	for _, ip := range imports {
		fmt.Fprintf(&b, "\t%q\n", ip)
	}
	b.WriteString(")\n\nfunc main() {}\n")
	return b.String()
}

// renderDiagnostics renders a run's findings to comparable strings.
func renderDiagnostics(ds []contracts.Diagnostic) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, fmt.Sprintf("%s %s:%d:%d %s", d.RuleID, d.File, d.Line, d.Column, d.Message))
	}
	slices.Sort(out)
	return out
}

// twoProgramsFixture is two binaries, each importing its own
// siteheader package with the same owner name and the same repeated
// literal: exactly the layout `gofastr generate package siteheader`
// produces for a second app. No binary links both sheets.
func twoProgramsFixture(t *testing.T) map[string]string {
	t.Helper()
	const css = ".brand { color: #ab34cd; }\n"
	return merge(
		map[string]string{
			"cmd/alpha/main.go": mainImporting("example.com/app/alpha/siteheader"),
			"cmd/beta/main.go":  mainImporting("example.com/app/beta/siteheader"),
		},
		ownedPair(t, "alpha/siteheader", "siteheader", "siteheader", css),
		ownedPair(t, "beta/siteheader", "siteheader", "siteheader", css),
	)
}

func TestDuplicateStyleQuietAcrossPrograms(t *testing.T) {
	ds := fixture(t, twoProgramsFixture(t))
	// Both sheets must be in the run at all, or the asserts below
	// pass vacuously: 1815 lists every owned sheet.
	if got := len(countRule(t, ds, contracts.RuleUpstreamCandidate)); got != 2 {
		t.Fatalf("want both siteheader sheets discovered, got %d", got)
	}
	assertNot(t, ds, contracts.RuleDuplicateStyleName, "two binaries never link both siteheaders")
	assertNot(t, ds, contracts.RuleRepeatedLiteral, "the literal repeats only across binaries")
}

func TestDuplicateStyleNameWithinProgram(t *testing.T) {
	ds := fixture(t, merge(
		map[string]string{
			"cmd/app/main.go": mainImporting("example.com/app/one", "example.com/app/two"),
		},
		ownedPair(t, "one", "one", "card", ".a { order: 1; }\n"),
		ownedPair(t, "two", "two", "card", ".b { order: 1; }\n"),
	))
	found := countRule(t, ds, contracts.RuleDuplicateStyleName)
	if len(found) != 2 {
		t.Fatalf("want both files of the duplicate reported, got %v", found)
	}
	for _, d := range found {
		other := "two/card.style.css"
		if d.File == other {
			other = "one/card.style.css"
		}
		if !strings.Contains(d.Message, other) {
			t.Errorf("%s: message %q does not name %s", d.File, d.Message, other)
		}
	}
}

func TestRepeatedLiteralWithinProgram(t *testing.T) {
	ds := fixture(t, merge(
		map[string]string{
			"cmd/app/main.go": mainImporting("example.com/app/one", "example.com/app/two"),
		},
		ownedPair(t, "one", "one", "card", ".x { color: #ab34cd; }\n"),
		ownedPair(t, "two", "two", "lane", ".y { color: #ab34cd; }\n"),
	))
	found := countRule(t, ds, contracts.RuleRepeatedLiteral)
	if len(found) != 2 {
		t.Fatalf("want the literal reported at both sheets, got %v", found)
	}
}

// Unreached library sheets form one group even when programs exist:
// the canonical packages a repo ships beside its binaries are checked
// against each other, never against a binary's own sheets.
func TestUnreachedSheetsGroupTogether(t *testing.T) {
	ds := fixture(t, merge(
		map[string]string{
			"cmd/app/main.go": mainImporting("example.com/app/live"),
		},
		ownedPair(t, "live", "live", "shell", ".s { order: 1; }\n"),
		ownedPair(t, "a", "a", "card", ".a { order: 1; }\n"),
		ownedPair(t, "b", "b", "card", ".b { order: 1; }\n"),
	))
	found := countRule(t, ds, contracts.RuleDuplicateStyleName)
	if len(found) != 2 {
		t.Fatalf("want the two unreached sheets reported, got %v", found)
	}
	for _, d := range found {
		if !strings.HasPrefix(d.File, "a/") && !strings.HasPrefix(d.File, "b/") {
			t.Errorf("reported %s; the reached sheet must stay quiet", d.File)
		}
	}
}

// A sheet two programs reach is reported once, with the colliding
// sheets of both programs merged into one list; a sheet of one program
// names only its own program's collision.
func TestDuplicateStyleMergedAcrossPrograms(t *testing.T) {
	ds := fixture(t, merge(
		map[string]string{
			"cmd/alpha/main.go": mainImporting("example.com/app/shared", "example.com/app/one"),
			"cmd/beta/main.go":  mainImporting("example.com/app/shared", "example.com/app/two"),
		},
		ownedPair(t, "shared", "shared", "card", ".s { color: #ab34cd; }\n"),
		ownedPair(t, "one", "one", "card", ".o { color: #111111; }\n"),
		ownedPair(t, "two", "two", "card", ".t { color: #222222; }\n"),
	))
	found := countRule(t, ds, contracts.RuleDuplicateStyleName)
	if len(found) != 3 {
		t.Fatalf("want one finding per sheet (shared merged), got %v", found)
	}
	for _, d := range found {
		switch d.File {
		case "shared/card.style.css":
			for _, other := range []string{"one/card.style.css", "two/card.style.css"} {
				if !strings.Contains(d.Message, other) {
					t.Errorf("shared: message %q does not name %s", d.Message, other)
				}
			}
		case "one/card.style.css":
			if !strings.Contains(d.Message, "shared/card.style.css") {
				t.Errorf("one: message %q does not name shared/card.style.css", d.Message)
			}
			if strings.Contains(d.Message, "two/card.style.css") {
				t.Errorf("one: message %q names a sheet no program links with it", d.Message)
			}
		case "two/card.style.css":
			if !strings.Contains(d.Message, "shared/card.style.css") {
				t.Errorf("two: message %q does not name shared/card.style.css", d.Message)
			}
			if strings.Contains(d.Message, "one/card.style.css") {
				t.Errorf("two: message %q names a sheet no program links with it", d.Message)
			}
		}
	}
}

// The 1822 twin of the merge test: the shared sheet's literal is
// reported once, naming both programs' partner sheets.
func TestRepeatedLiteralMergedAcrossPrograms(t *testing.T) {
	ds := fixture(t, merge(
		map[string]string{
			"cmd/alpha/main.go": mainImporting("example.com/app/shared", "example.com/app/one"),
			"cmd/beta/main.go":  mainImporting("example.com/app/shared", "example.com/app/two"),
		},
		ownedPair(t, "shared", "shared", "brand", ".s { color: #ab34cd; }\n"),
		ownedPair(t, "one", "one", "card", ".o { color: #ab34cd; }\n"),
		ownedPair(t, "two", "two", "lane", ".t { color: #ab34cd; }\n"),
	))
	found := countRule(t, ds, contracts.RuleRepeatedLiteral)
	if len(found) != 3 {
		t.Fatalf("want one finding per sheet (shared merged), got %v", found)
	}
	for _, d := range found {
		if d.File != "shared/brand.style.css" {
			continue
		}
		for _, other := range []string{"one/card.style.css", "two/lane.style.css"} {
			if !strings.Contains(d.Message, other) {
				t.Errorf("shared: message %q does not name %s", d.Message, other)
			}
		}
	}
}

func TestOwnedStyleProgramsDeterministic(t *testing.T) {
	files := twoProgramsFixture(t)
	first := renderDiagnostics(fixture(t, files))
	second := renderDiagnostics(fixture(t, files))
	if !slices.Equal(first, second) {
		t.Fatalf("two runs disagree:\n%v\n%v", first, second)
	}
}
