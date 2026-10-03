package upgradefixtures

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"github.com/DonaldMurillo/gofastr/internal/upgrade/scan"
)

// ---------------------------------------------------------------------------
// Gate 2: the fixtures, end to end, through the typed scanner
// ---------------------------------------------------------------------------
//
// migration.patch is the oracle: every hunk that edits a Go, CSS, gofastr.yml
// or go.mod file must be findable by some in-range registry note (the note
// points at the code it forces you to change), and — in compile-error mode,
// where hits land exactly where the code broke — a breaking note's hits on Go
// or CSS files must sit inside a hunk the patch edits. Both directions are
// checked against scan.Run directly, in three modes:
//
//   - own version, pre-replace: the fixture as generated, type-checked
//     against the gofastr release it pins (needs the module proxy);
//   - pointed at this tree, pre-patch: nothing type-checks, every compile
//     error the upgrade causes must be explained by a note;
//   - pointed at this tree, post-patch: no breaking note's strings/css/
//     config/gomod matcher may still hit (no retired spelling survives) and
//     the app type-checks again.

// patchHunk is one hunk of a fixture's migration.patch, keyed to the OLD
// (pre-patch) file the scanner sees.
type patchHunk struct {
	file     string // root-relative, slash-separated ("entities/tags.go")
	oldStart int    // 0 for a new file
	oldCount int
	firstOld string // first non-blank old line, trimmed: the exemption key
}

// covers reports whether a hit at line lands inside the hunk's old range.
// A new-file hunk (oldStart 0) has no old lines; a zero-count hunk is a
// pure insertion with no old lines either — neither can be hit, so both
// only pass coverage through the exemption table.
func (h patchHunk) covers(line int) bool {
	if h.oldStart == 0 || h.oldCount == 0 {
		return false
	}
	return line >= h.oldStart && line < h.oldStart+h.oldCount
}

func (h patchHunk) exemptionKey() string {
	return h.file + "|" + h.firstOld
}

func (h patchHunk) describe() string {
	return fmt.Sprintf("%s:%d,%d (old lines start %q)", h.file, h.oldStart, h.oldStart+h.oldCount-1, h.firstOld)
}

var hunkHeaderRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+\d+(?:,\d+)? @@`)

// parseMigrationPatch extracts the hunks of a unified diff, old side only.
func parseMigrationPatch(src string) ([]patchHunk, error) {
	var hunks []patchHunk
	var oldSide, newSide string
	var cur *patchHunk
	for i, ln := range strings.Split(src, "\n") {
		switch {
		case strings.HasPrefix(ln, "--- "):
			oldSide = trimDiffPath(ln[4:])
		case strings.HasPrefix(ln, "+++ "):
			newSide = trimDiffPath(ln[4:])
		case strings.HasPrefix(ln, "@@ "):
			m := hunkHeaderRe.FindStringSubmatch(ln)
			if m == nil {
				return nil, fmt.Errorf("patch line %d: malformed hunk header %q", i+1, ln)
			}
			start, err := strconv.Atoi(m[1])
			if err != nil {
				return nil, fmt.Errorf("patch line %d: bad old start in %q", i+1, ln)
			}
			count := 1
			if m[2] != "" {
				if count, err = strconv.Atoi(m[2]); err != nil {
					return nil, fmt.Errorf("patch line %d: bad old count in %q", i+1, ln)
				}
			}
			file := oldSide
			if file == "" || file == "/dev/null" {
				file = newSide // a new file: old side is /dev/null
			}
			if file == "" || file == "/dev/null" {
				return nil, fmt.Errorf("patch line %d: hunk with neither an old nor a new file", i+1)
			}
			hunks = append(hunks, patchHunk{file: file, oldStart: start, oldCount: count})
			cur = &hunks[len(hunks)-1]
		case cur == nil:
			// Headers and prose between file sections.
		case strings.HasPrefix(ln, "diff "):
			cur = nil
		case strings.HasPrefix(ln, "+") || strings.HasPrefix(ln, "\\"):
			// Added or no-newline marker: not an old line.
		case strings.HasPrefix(ln, " ") || strings.HasPrefix(ln, "-"):
			if cur.oldStart == 0 {
				continue // new file: no old lines exist
			}
			if cur.firstOld == "" && hasLetter(ln[1:]) {
				// The exemption key: the first old line with an identifier
				// in it, so brace-only context lines ("}") do not make
				// every insertion hunk share one key.
				cur.firstOld = strings.TrimSpace(ln[1:])
			}
		default:
			// A bare line (no leading diff marker) ends the hunk body.
			cur = nil
		}
	}
	return hunks, nil
}

// hunkExemptions lists migration.patch hunks no note can be expected to
// find, one reason per entry. Keyed by file plus the hunk's first non-blank
// old line, so regenerating a patch (shifting line numbers) does not rot the
// table. Keep it SHORT: an entry here weakens the coverage gate, and every
// entry must be justified.
var hunkExemptions = map[string]string{
	// The typed pager refuses an out-of-range page, so the requested page
	// is clamped before fetching rows. Pure insertion: the surrounding old
	// lines are legal code, nothing retired sits on them.
	"resource.go|total, _ := c.Crud.CountAll(ctx, framework.ListOptions{Filters: filters})": "page clamp the typed pager demands; pure insertion over legal old lines",
	// formInput rewiring (decl + control branches): html.Select/TextArea/
	// Input keep their API and fields, so no registry spelling differs on
	// these old lines; the builder note's hit lands at the Form call site.
	"resource.go|// formInput builds the typed control for one field, prefilled with cur. Enums": "control builders keep their API; the builder note hits the Form call site instead",
	"resource.go|for _, v := range f.Values {":                                                   "same formInput rewiring: only ExtraAttrs wiring is new",
	// The layout build closure needs two new imports; the retired API
	// (NewLayout/WithSidebar) is matched in the hunk that rewrites the call.
	"app.go|\"github.com/DonaldMurillo/gofastr/battery/auth\"": "pure import additions for the new layout build closure",
}

// coverageTarget reports whether a patch file is one the registry's
// matchers can read: Go, CSS, go.mod, or a gofastr blueprint YAML.
func coverageTarget(file string) bool {
	switch {
	case strings.HasSuffix(file, ".go"), strings.HasSuffix(file, ".css"):
		return true
	case file == "go.mod", file == "gofastr.yml", strings.HasSuffix(file, ".gofastr.yml"):
		return true
	}
	return false
}
func trimDiffPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "a/")
	p = strings.TrimPrefix(p, "b/")
	return strings.TrimSuffix(p, "\t")
}

// hasLetter reports whether s carries an identifier-shaped character, so a
// brace-only context line does not become an exemption key.
func hasLetter(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return unicode.IsLetter(r) }) >= 0
}

// hitLine is one scan hit reduced to what the gates need.
type hitLine struct {
	file    string
	line    int
	why     string
	note    *upgrade.Note
	fromErr bool // read from a compile error, not a typed match
}

// findUncoveredHunks returns the coverage-target hunks no hit lands in and
// no exemption covers. hits are the in-range notes' hits in the same mode.
func findUncoveredHunks(hunks []patchHunk, hits []hitLine, exempt map[string]string) []patchHunk {
	var out []patchHunk
	for _, h := range hunks {
		if !coverageTarget(h.file) {
			continue
		}
		if _, ok := exempt[h.exemptionKey()]; ok {
			continue
		}
		covered := false
		for _, hit := range hits {
			if hit.file == h.file && h.covers(hit.line) {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, h)
		}
	}
	return out
}

// findOutOfHunkHits returns hits that land on a line no hunk of that file
// covers — including files the patch never touches.
func findOutOfHunkHits(hunks []patchHunk, hits []hitLine) []hitLine {
	var out []hitLine
	for _, hit := range hits {
		inside := false
		for _, h := range hunks {
			if h.file == hit.file && h.covers(hit.line) {
				inside = true
				break
			}
		}
		if !inside {
			out = append(out, hit)
		}
	}
	return out
}

// collectHits flattens a scan result. breakingGoCSS keeps only breaking
// notes' hits on .go/.css files (the precision gate's scope).
func collectHits(res *scan.Result, breakingGoCSS bool) []hitLine {
	var out []hitLine
	for n, hits := range res.Hits {
		for _, h := range hits {
			if breakingGoCSS {
				if !n.Breaking {
					continue
				}
				if !strings.HasSuffix(h.File, ".go") && !strings.HasSuffix(h.File, ".css") {
					continue
				}
			}
			out = append(out, hitLine{file: h.File, line: h.Line, why: h.Why, note: n, fromErr: h.Err != ""})
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// the three modes, wired into upgradeAndAssert
// ---------------------------------------------------------------------------

// gateOwnVersionScan runs mode 1: the fixture as generated, before the
// replace, type-checked against the gofastr release it pins (from the
// module proxy). It must type-check — a fixture that cannot compile at its
// own version is a fixture problem, not a registry one.
func gateOwnVersionScan(t *testing.T, fx fixture, appDir string) {
	t.Helper()
	reg, notes := loadInRangeNotes(t, appDir)
	hunks := loadFixtureHunks(t, fx)
	res, err := scan.Run(appDir, notes, reg.MarkerSinks)
	if err != nil {
		t.Fatalf("own-version scan: %v", err)
	}
	if !res.TypeChecked {
		t.Fatalf("fixture problem: %s does not type-check at its pinned gofastr %s (broken: %s; first unexplained: %s)",
			fx.name, fixtureGofastrVersion(t, appDir), strings.Join(res.Broken, ", "), firstUnexplained(res))
	}
	assertHunkCoverage(t, "own-version (typed)", hunks, res, notes)
	reportPrecision(t, "own-version (typed)", hunks, res)
}

// gateBrokenScanAtHead runs mode 2: after the replace points the fixture at
// this tree, before the patch. The app must NOT type-check, every compile
// error must be explained by an in-range note (Unexplained empty), every
// coverage hunk still matched, and breaking hits on Go/CSS landing inside
// the patch's old ranges.
func gateBrokenScanAtHead(t *testing.T, root string, fx fixture, appDir string) {
	t.Helper()
	reg, notes := loadInRangeNotes(t, appDir)
	hunks := loadFixtureHunks(t, fx)

	// Resolve the tree's module graph into go.sum so the load's errors are
	// the upgrade's real compile errors, not missing-sum noise.
	run(t, appDir, "go mod download all", "go", "mod", "download", "all")

	cliUpgradeSmokeRun(t, root, appDir)

	res, err := scan.Run(appDir, notes, reg.MarkerSinks)
	if err != nil {
		t.Fatalf("pre-patch scan at HEAD: %v", err)
	}
	if res.TypeChecked {
		t.Fatalf("the unpatched %s type-checks against the current tree — the fixture no longer exercises a real migration", fx.name)
	}
	for _, u := range res.Unexplained {
		t.Errorf("pre-patch scan: compile error no in-range note explains: %s:%d: %s", u.File, u.Line, u.Why)
	}
	assertHunkCoverage(t, "compile-error", hunks, res, notes)
	checkPrecision(t, "compile-error", hunks, res)
}

// gatePatchedScanAtHead runs mode 5: after the patch, still pointed at this
// tree. The migrated app must type-check, and no breaking note's strings,
// css, config or gomod matcher may still hit: the retired spellings are gone.
func gatePatchedScanAtHead(t *testing.T, fx fixture, appDir string) {
	t.Helper()
	reg, notes := loadInRangeNotes(t, appDir)
	silence := silencedMatchers(notes)
	res, err := scan.Run(appDir, silence, reg.MarkerSinks)
	if err != nil {
		t.Fatalf("post-patch scan: %v", err)
	}
	if !res.TypeChecked {
		t.Fatalf("the patched %s still does not type-check against the current tree (broken: %s; first unexplained: %s)",
			fx.name, strings.Join(res.Broken, ", "), firstUnexplained(res))
	}
	for n, hits := range res.Hits {
		for _, h := range hits {
			t.Errorf("retired spelling survived migration.patch: note %q (%s) still matches %s:%d: %s",
				n.Change, n.File, h.File, h.Line, h.Why)
		}
	}
}

// silencedMatchers copies each breaking in-range note once per non-Go
// matcher it carries, so one scan reports which matcher kind still hits.
func silencedMatchers(notes []*upgrade.Note) []*upgrade.Note {
	var out []*upgrade.Note
	for _, n := range notes {
		if !n.Breaking {
			continue
		}
		if !n.Find.Strings.Empty() {
			out = append(out, onlyMatcher(n, "strings"))
		}
		if !n.Find.CSS.Empty() {
			out = append(out, onlyMatcher(n, "css"))
		}
		if len(n.Find.Config) > 0 {
			out = append(out, onlyMatcher(n, "config"))
		}
		if n.Find.GoMod != nil {
			out = append(out, onlyMatcher(n, "gomod"))
		}
	}
	return out
}

func onlyMatcher(n *upgrade.Note, kind string) *upgrade.Note {
	c := *n
	f := upgrade.Find{}
	switch kind {
	case "strings":
		f.Strings = n.Find.Strings
	case "css":
		f.CSS = n.Find.CSS
	case "config":
		f.Config = n.Find.Config
	case "gomod":
		f.GoMod = n.Find.GoMod
	}
	c.Find = f
	return &c
}

// assertHunkCoverage fails for every coverage-target hunk no in-range note
// hit lands inside (outside the exemption table).
func assertHunkCoverage(t *testing.T, mode string, hunks []patchHunk, res *scan.Result, notes []*upgrade.Note) {
	t.Helper()
	uncovered := findUncoveredHunks(hunks, collectHits(res, false), hunkExemptions)
	for _, h := range uncovered {
		t.Errorf("%s mode: migration.patch hunk %s is matched by no in-range note (out of %d notes) — write the note that finds it, or add a justified exemption",
			mode, h.describe(), len(notes))
	}
}

// checkPrecision fails for every breaking-note hit read from a compile
// error on a Go/CSS file outside every hunk old range. Compile errors land
// exactly where the code broke, so such a hit means the patch misses a line
// the note flags (or the note overreaches). The typed matchers still run
// over what type-checks in a broken package, and their hits on symbols the
// target keeps are review pointers, reported as in mode 1.
func checkPrecision(t *testing.T, mode string, hunks []patchHunk, res *scan.Result) {
	t.Helper()
	for _, hit := range findOutOfHunkHits(hunks, collectHits(res, true)) {
		if !hit.fromErr {
			t.Logf("%s mode: note %q hits %s:%d (%s) outside the patch hunks (typed match, informational)",
				mode, hit.note.Change, hit.file, hit.line, hit.why)
			continue
		}
		t.Errorf("%s mode: breaking note %q hits %s:%d (%s) outside every migration.patch hunk of that file",
			mode, hit.note.Change, hit.file, hit.line, hit.why)
	}
}

// reportPrecision is mode 1's version of checkPrecision: a type-aware scan
// hits every call of a signature-changed symbol, including calls the patch
// did not need to touch, so out-of-hunk hits are reported, not failed.
func reportPrecision(t *testing.T, mode string, hunks []patchHunk, res *scan.Result) {
	t.Helper()
	for _, hit := range findOutOfHunkHits(hunks, collectHits(res, true)) {
		t.Logf("%s mode: note %q hits %s:%d (%s) outside the patch hunks (informational in type-aware mode)",
			mode, hit.note.Change, hit.file, hit.line, hit.why)
	}
}

func firstUnexplained(res *scan.Result) string {
	if len(res.Unexplained) == 0 {
		return "(none)"
	}
	u := res.Unexplained[0]
	return fmt.Sprintf("%s:%d: %s", u.File, u.Line, u.Why)
}

// loadInRangeNotes loads the registry and returns the notes `gofastr
// upgrade` would print for the app in appDir: everything from its go.mod's
// gofastr version to the registry's newest release.
func loadInRangeNotes(t *testing.T, appDir string) (*upgrade.Registry, []*upgrade.Note) {
	t.Helper()
	reg, err := upgrade.Load()
	if err != nil {
		t.Fatalf("load the migration registry: %v", err)
	}
	if len(reg.Releases) == 0 {
		t.Fatal("the migration registry has no releases")
	}
	target := reg.Releases[len(reg.Releases)-1].Version
	var notes []*upgrade.Note
	for _, rel := range upgrade.ReleasesInRange(reg, fixtureGofastrVersion(t, appDir), target) {
		notes = append(notes, rel.Notes...)
	}
	if len(notes) == 0 {
		t.Fatalf("no in-range notes for a fixture at gofastr %s", fixtureGofastrVersion(t, appDir))
	}
	return reg, notes
}

// fixtureGofastrVersion reads the gofastr require line out of the app's
// go.mod — the version the fixture builds with today.
func fixtureGofastrVersion(t *testing.T, appDir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(appDir, "go.mod"))
	if err != nil {
		t.Fatalf("read %s/go.mod: %v", appDir, err)
	}
	for _, m := range regexp.MustCompile(`github\.com/DonaldMurillo/gofastr (v[0-9.]+)`).FindAllStringSubmatch(string(b), -1) {
		if err := upgrade.ValidateSemver(m[1]); err == nil {
			return m[1]
		}
	}
	t.Fatalf("no gofastr require line in %s/go.mod", appDir)
	return ""
}

func loadFixtureHunks(t *testing.T, fx fixture) []patchHunk {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixturesRoot(t), "fixtures", fx.name, "migration.patch"))
	if err != nil {
		t.Fatalf("read %s migration.patch: %v", fx.name, err)
	}
	hunks, err := parseMigrationPatch(string(b))
	if err != nil {
		t.Fatalf("parse %s migration.patch: %v", fx.name, err)
	}
	return hunks
}

// ---------------------------------------------------------------------------
// unit tests of the gates themselves
// ---------------------------------------------------------------------------

func TestParseMigrationPatchHunks(t *testing.T) {
	const diff = `diff --git a/a.go b/a.go
index 111..222 100644
--- a/a.go
+++ b/a.go
@@ -1,3 +1,4 @@ func f() {
 line1
-line2
+line2 new
 line3
@@ -10 +11,2 @@ func g() {
 ctx
 
+added
diff --git a/new.go b/new.go
new file mode 100644
--- /dev/null
+++ b/new.go
@@ -0,0 +1,2 @@
+one
+two
diff --git a/old.css b/old.css
index 333..000 100644
--- a/old.css
+++ /dev/null
@@ -5,2 +0,0 @@
-.ui-button {}
-.ui-hero {}
`
	hunks, err := parseMigrationPatch(diff)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []patchHunk{
		{file: "a.go", oldStart: 1, oldCount: 3, firstOld: "line1"},
		{file: "a.go", oldStart: 10, oldCount: 1, firstOld: "ctx"},
		{file: "new.go", oldStart: 0, oldCount: 0, firstOld: ""},
		{file: "old.css", oldStart: 5, oldCount: 2, firstOld: ".ui-button {}"},
	}
	if len(hunks) != len(want) {
		t.Fatalf("got %d hunks, want %d: %+v", len(hunks), len(want), hunks)
	}
	for i, h := range hunks {
		if h != want[i] {
			t.Errorf("hunk %d = %+v, want %+v", i, h, want[i])
		}
	}
	if !hunks[0].covers(3) || hunks[0].covers(4) || hunks[0].covers(0) {
		t.Errorf("hunk 1..3 covers() wrong for boundary lines")
	}
	if !hunks[1].covers(10) || hunks[1].covers(11) {
		t.Errorf("single-count hunk must cover exactly its start line")
	}
	if hunks[2].covers(1) {
		t.Errorf("a new-file hunk covers no old line")
	}
}

func TestCoverageGateFlagsUncoveredHunk(t *testing.T) {
	hunks := []patchHunk{
		{file: "a.go", oldStart: 10, oldCount: 3, firstOld: "line ten"},
		{file: "b.go", oldStart: 1, oldCount: 1, firstOld: "exempt me"},
	}
	exempt := map[string]string{"b.go|exempt me": "justified"}
	covered := []hitLine{{file: "a.go", line: 12, why: "hit"}}
	if out := findUncoveredHunks(hunks, covered, exempt); len(out) != 0 {
		t.Errorf("uncovered = %+v, want none", out)
	}
	// Hit one line short of the range: still uncovered.
	short := []hitLine{{file: "a.go", line: 9, why: "hit"}}
	if out := findUncoveredHunks(hunks, short, exempt); len(out) != 1 || out[0].file != "a.go" {
		t.Errorf("uncovered = %+v, want the a.go hunk", out)
	}
	// Dropping the exemption must surface the exempted hunk again.
	if out := findUncoveredHunks(hunks, covered, nil); len(out) != 1 || out[0].file != "b.go" {
		t.Errorf("uncovered without exemptions = %+v, want the b.go hunk", out)
	}
	// Non-coverage files never count.
	css := []patchHunk{{file: "x.txt", oldStart: 1, oldCount: 1, firstOld: "t"}}
	if out := findUncoveredHunks(css, nil, nil); len(out) != 0 {
		t.Errorf("a .txt hunk is not a coverage target: %+v", out)
	}
}

func TestPrecisionFlagsOutOfHunkHit(t *testing.T) {
	hunks := []patchHunk{{file: "a.go", oldStart: 10, oldCount: 3, firstOld: "x"}}
	hits := []hitLine{
		{file: "a.go", line: 11, why: "in"},
		{file: "a.go", line: 99, why: "out"},
		{file: "untouched.go", line: 5, why: "out"},
	}
	out := findOutOfHunkHits(hunks, hits)
	if len(out) != 2 {
		t.Fatalf("out-of-hunk hits = %+v, want the two outside hits", out)
	}
	if out[0].line != 99 || out[1].file != "untouched.go" {
		t.Errorf("out-of-hunk hits = %+v, want a.go:99 then untouched.go:5", out)
	}
}
