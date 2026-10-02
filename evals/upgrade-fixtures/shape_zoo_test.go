package upgradefixtures

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"github.com/DonaldMurillo/gofastr/internal/upgrade/scan"
)

// The shape zoo is one app, pinned to gofastr v0.63.0, holding every code
// shape a reviewer found the scanner misreading: a method implementing an
// interface from a package that never imports it, a type alias, a map key
// in another file and another case, an unsafe URL behind a constant,
// markup spelled the way a browser forgives, CSS escapes, a testing
// message, a build-constrained file, a nested module, a package-local
// alias in a compile error. Each line the scan must report carries a
// `zoo:hit <tag>` marker; every other line must stay silent. A line only a
// compile error lands on (an interface assertion whose implementation
// changed shape elsewhere) carries `zoo:err <tag>` instead: it must stay
// silent while the app type-checks. The historical
// fixtures prove an upgrade works end to end; the zoo proves the scanner
// reads each shape, in both directions, and fails naming the line.

// zooNotes maps a marker tag to the start of the note's change text.
var zooNotes = map[string]string{
	"finish":       "IdempotencyStore.Finish takes the request fingerprint",
	"gomod":        "the module requires Go 1.27",
	"disabled":     "disabled belongs to ButtonConfig.Disabled",
	"button-class": "the button classes are fui-button*",
	"action":       "an unsafe FormConfig.Action panics",
	"sorthref":     "ui.DataTableConfig loses SortHrefPattern",
	"tokens":       "the ten legacy token aliases",
	"siteheader":   "ui.SiteHeader, ui.SiteFooter and ui.DocLayout are deleted",
}

var zooFixture = fixture{"shape-zoo", "v0.63.0"}

func TestShapeZoo(t *testing.T) {
	if os.Getenv("GOFASTR_UPGRADE_FIXTURES") != "1" {
		t.Skip("set GOFASTR_UPGRADE_FIXTURES=1 to run the shape zoo (needs the module proxy)")
	}
	t.Setenv("CGO_ENABLED", "0")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOWORK", "off")
	root := repoRoot(t)
	appDir := filepath.Join(t.TempDir(), "zoo")
	mustCopyDir(t, filepath.Join(fixturesRoot(t), "fixtures", zooFixture.name), appDir)
	reg, notes := loadInRangeNotes(t, appDir)
	want := zooMarkers(t, appDir, "zoo:hit ")
	wantErr := zooMarkers(t, appDir, "zoo:err ")

	// At its own version: the app type-checks, every file is reached,
	// and the hits are exactly the marked lines.
	res, err := scan.Run(appDir, notes, reg.MarkerSinks)
	if err != nil {
		t.Fatalf("own-version scan: %v", err)
	}
	if !res.TypeChecked {
		t.Fatalf("fixture problem: the zoo does not type-check at v0.63.0 (broken: %s; first unexplained: %s)",
			strings.Join(res.Broken, ", "), firstUnexplained(res))
	}
	for _, u := range res.Unscanned {
		t.Errorf("own-version scan left a file unscanned: %s", u)
	}
	compareZooHits(t, "own-version", want, zooHits(res, false))

	// Pointed at this tree, unpatched: nothing type-checks, every compile
	// error is explained, and a hit read from an error sits on a line
	// marked zoo:hit or zoo:err for its note.
	rewriteGomodReplace(t, appDir, root)
	rewriteGomodReplace(t, filepath.Join(appDir, "tools"), root)
	run(t, appDir, "go mod download all", "go", "mod", "download", "all")
	run(t, filepath.Join(appDir, "tools"), "go mod download all (tools)", "go", "mod", "download", "all")
	res, err = scan.Run(appDir, notes, reg.MarkerSinks)
	if err != nil {
		t.Fatalf("pre-patch scan at HEAD: %v", err)
	}
	if res.TypeChecked {
		t.Fatal("the unpatched zoo type-checks against the current tree: it no longer exercises a migration")
	}
	for _, u := range res.Unexplained {
		t.Errorf("pre-patch scan: compile error no in-range note explains: %s:%d: %s", u.File, u.Line, u.Why)
	}
	errHits := zooHits(res, true)
	for _, key := range slices.Sorted(maps.Keys(errHits)) {
		for _, tag := range errHits[key] {
			if !slices.Contains(want[key], tag) && !slices.Contains(wantErr[key], tag) {
				t.Errorf("pre-patch scan: note %s read from a compile error at %s, a line not marked for it", tag, key)
			}
		}
	}

	// Migrated: the overlay replaces every marked file, the app
	// type-checks again and no retired spelling survives.
	mustCopyDir(t, filepath.Join(fixturesRoot(t), "fixtures", "_"+zooFixture.name+".after"), appDir)
	gatePatchedScanAtHead(t, zooFixture, appDir)
}

// zooMarkers reads every `<marker>tag[,tag]` comment in the app, keyed by
// "file:line".
func zooMarkers(t *testing.T, appDir, marker string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	err := filepath.WalkDir(appDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(appDir, path)
		for i, line := range strings.Split(string(b), "\n") {
			_, after, ok := strings.Cut(line, marker)
			if !ok {
				continue
			}
			spec := strings.Fields(strings.TrimSuffix(strings.TrimSpace(after), "*/"))
			if len(spec) == 0 {
				return fmt.Errorf("%s:%d: %snames no tag", rel, i+1, marker)
			}
			for _, tag := range strings.Split(spec[0], ",") {
				if _, known := zooNotes[tag]; !known {
					return fmt.Errorf("%s:%d: unknown zoo tag %q", rel, i+1, tag)
				}
				key := fmt.Sprintf("%s:%d", filepath.ToSlash(rel), i+1)
				out[key] = append(out[key], tag)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read zoo markers: %v", err)
	}
	if len(out) == 0 {
		t.Fatalf("the zoo carries no %smarkers", marker)
	}
	return out
}

// zooHits reduces a scan to "file:line" -> tags. A note with no tag keeps
// its change text, so an unexpected note shows up by name. fromErr keeps
// only hits read from compile errors.
func zooHits(res *scan.Result, fromErr bool) map[string][]string {
	out := map[string][]string{}
	for n, hits := range res.Hits {
		tag := zooTag(n)
		for _, h := range hits {
			if fromErr && h.Err == "" {
				continue
			}
			key := fmt.Sprintf("%s:%d", h.File, h.Line)
			if !slices.Contains(out[key], tag) {
				out[key] = append(out[key], tag)
			}
		}
	}
	return out
}

func zooTag(n *upgrade.Note) string {
	for tag, change := range zooNotes {
		if strings.HasPrefix(n.Change, change) {
			return tag
		}
	}
	return n.Change
}

// compareZooHits fails for each marked line the scan missed and each hit
// on a line not marked for that note.
func compareZooHits(t *testing.T, mode string, want, got map[string][]string) {
	t.Helper()
	for _, key := range slices.Sorted(maps.Keys(want)) {
		for _, tag := range want[key] {
			if !slices.Contains(got[key], tag) {
				t.Errorf("%s: %s is marked zoo:hit %s, but the scan missed it", mode, key, tag)
			}
		}
	}
	for _, key := range slices.Sorted(maps.Keys(got)) {
		for _, tag := range got[key] {
			if !slices.Contains(want[key], tag) {
				t.Errorf("%s: unexpected hit at %s for note %q", mode, key, tag)
			}
		}
	}
}
