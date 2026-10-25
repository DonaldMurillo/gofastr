package embedcheck

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/packages"
)

// fixtureDir is the fixture module, cmd/check-embed/testdata. It sits outside
// this package's directory on purpose: cmd/affected marks a package changed
// when any file under its directory changes, and a changed package selects
// every importer, so a fixture edit used to select cmd/gofastr and everything
// that runs it. From here, the directive below selects this package alone,
// the only one a fixture can break.
//
//gofastr:test-reads cmd/check-embed/testdata/
const fixtureDir = "../testdata"

// fixturePkgPrefix is the import path every fixture package's name follows.
const fixturePkgPrefix = "checkembedtestdata/src/"

// testdataDir is the absolute path to the fixture module. go test runs the
// tests with the package directory as the working directory.
func testdataDir(t *testing.T) string {
	t.Helper()
	td, err := filepath.Abs(fixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	return td
}

// loadFixtures type-checks the fixture packages matching patterns in module
// mode (the fixture go.mod has a local replace onto the real framework, so the
// fixtures import the real embed.Surface / app.Screen / component types) and
// indexes the root packages by fixture name. It loads with CheckAll's own
// loadMode, so the analyzer sees exactly what the CLI and the `gofastr build`
// gate give it. overlay, when set, replaces files' content without writing
// them.
//
// One load serves many tests: every fixture imports the same framework
// packages, and parsing and type-checking those dominates a load. Loading
// each fixture in each test repeated that work about 80 times per run.
func loadFixtures(overlay map[string][]byte, patterns ...string) (map[string]*packages.Package, error) {
	dir, err := filepath.Abs(fixtureDir)
	if err != nil {
		return nil, err
	}
	cfg := &packages.Config{
		Mode:  loadMode,
		Dir:   dir,
		Tests: false,
		// The fixture module is its own module with a local replace; module
		// mode + GOPROXY=off resolves everything from the working tree and
		// the module cache.
		Env:     append(os.Environ(), "GO111MODULE=on", "GOPROXY=off", "GOWORK=off"),
		Overlay: overlay,
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]*packages.Package, len(pkgs))
	for _, p := range pkgs {
		name, ok := strings.CutPrefix(p.PkgPath, fixturePkgPrefix)
		if !ok {
			return nil, fmt.Errorf("loaded %q, which is not a fixture package", p.PkgPath)
		}
		byName[name] = p
	}
	return byName, nil
}

// fixtures is every fixture package, loaded once per test binary.
var fixtures = sync.OnceValues(func() (map[string]*packages.Package, error) {
	return loadFixtures(nil, "./src/...")
})

// fixture returns one package from the shared load.
func fixture(t *testing.T, pkgName string) *packages.Package {
	t.Helper()
	pkgs, err := fixtures()
	if err != nil {
		t.Fatalf("loading fixtures: %v", err)
	}
	p := pkgs[pkgName]
	if p == nil {
		t.Fatalf("no fixture package %s under %s/src", pkgName, fixtureDir)
	}
	return p
}

// loadFindings is the analyzer's findings for one fixture. It deliberately
// bypasses analysistest's `// want` checker so the mutation suite can assert
// before/after counts without fighting static expectations.
func loadFindings(t *testing.T, pkgName string) []Finding {
	t.Helper()
	findings, _ := loadAll(t, pkgName)
	return findings
}

// loadAll is loadFindings plus the walk's give-up notes.
func loadAll(t *testing.T, pkgName string) ([]Finding, []Unresolved) {
	t.Helper()
	return analyzeFixture(t, fixture(t, pkgName))
}

// analyzeFixture runs the analyzer over a loaded fixture, failing the test
// when the fixture did not type-check.
func analyzeFixture(t *testing.T, p *packages.Package) ([]Finding, []Unresolved) {
	t.Helper()
	if p.Types == nil || p.TypesInfo == nil {
		for _, e := range p.Errors {
			t.Logf("package %s error: %v", p.ID, e)
		}
		t.Fatalf("package %s did not type-check (see logged errors above)", p.PkgPath)
	}
	return analyze(p.Types, p.TypesInfo, p.Syntax)
}

// --- mutation checks ----------------------------------------------------
//
// Each proves its static test is actually exercising the trigger: the source
// is rewritten in the loader's overlay, the mutation is verified to have
// applied (occurrence count before/after), and the finding count must flip. A
// mutation that silently did not apply looks exactly like a test that cannot
// fail.

// replacement is one (from→to) edit applied to a fixture file.
type replacement struct{ from, to string }

// mutations maps a fixture name to the edits its mutation check applies to
// src/<name>/<name>.go. Each check registers its edits through mutation, in a
// package-level var beside the test, so all of them are known before the one
// mutated load runs.
var mutations = map[string][]replacement{}

// mutation registers the edits for one fixture and returns its name. One
// fixture takes one mutation: they share an overlay.
func mutation(pkgName string, reps ...replacement) string {
	if _, dup := mutations[pkgName]; dup {
		panic("embedcheck tests: second mutation registered for fixture " + pkgName)
	}
	mutations[pkgName] = reps
	return pkgName
}

// mutatedLoad is the result of loading every mutated fixture at once.
type mutatedLoad struct {
	pkgs    map[string]*packages.Package
	applied map[string][]string // per fixture: the log of each edit that landed
	failed  map[string]error    // per fixture: why its mutation did not apply
}

// mutatedFixtures applies every registered mutation in one overlay and loads
// the mutated fixtures in one packages.Load. Each edit must apply exactly
// once, in order, and must change the content: an edit whose `from` is absent
// is a silent no-op, the failure mode that makes a mutated test
// indistinguishable from an inert one, which has bitten this repo before. A
// fixture whose edit fails is left out of the overlay and its test fails.
//
// The file on disk is never written: a fixture rewritten in place, even
// restored, carries a fresh mtime, and every package that walks the repo then
// misses Go's test cache.
var mutatedFixtures = sync.OnceValues(func() (*mutatedLoad, error) {
	m := &mutatedLoad{applied: map[string][]string{}, failed: map[string]error{}}
	overlay := map[string][]byte{}
	var patterns []string
	for _, name := range slices.Sorted(maps.Keys(mutations)) {
		rel := filepath.Join(fixtureDir, "src", name, name+".go")
		orig, err := os.ReadFile(rel)
		if err != nil {
			m.failed[name] = fmt.Errorf("read %s: %w", rel, err)
			continue
		}
		// Git may check fixtures out with CRLF on Windows, while the mutation
		// patterns intentionally use source-style LF line endings. Normalize
		// only the overlay copy; the file on disk keeps its bytes.
		cur := strings.ReplaceAll(string(orig), "\r\n", "\n")
		for _, r := range mutations[name] {
			if !strings.Contains(cur, r.from) {
				m.failed[name] = fmt.Errorf("mutation did not apply: from-pattern not found in %s\n%q", rel, r.from)
				break
			}
			before := strings.Count(cur, r.to)
			cur = strings.Replace(cur, r.from, r.to, 1)
			after := strings.Count(cur, r.to)
			m.applied[name] = append(m.applied[name],
				fmt.Sprintf("applied in %s: %q removed; %q occurrences %d→%d", rel, r.from, r.to, before, after))
		}
		if m.failed[name] != nil {
			continue
		}
		abs, err := filepath.Abs(rel)
		if err != nil {
			return nil, err
		}
		overlay[abs] = []byte(cur)
		patterns = append(patterns, "./src/"+name)
	}
	if len(patterns) == 0 {
		m.pkgs = map[string]*packages.Package{}
		return m, nil
	}
	pkgs, err := loadFixtures(overlay, patterns...)
	if err != nil {
		return nil, err
	}
	m.pkgs = pkgs
	return m, nil
})

// mutatedFindings is loadFindings over the mutated copy of one fixture.
//
// Sharing one overlay is sound only while no mutated fixture imports another:
// then each package's result is the one its own mutation alone produces. The
// check below fails the test the day a fixture change breaks that.
func mutatedFindings(t *testing.T, pkgName string) []Finding {
	t.Helper()
	m, err := mutatedFixtures()
	if err != nil {
		t.Fatalf("loading mutated fixtures: %v", err)
	}
	if err := m.failed[pkgName]; err != nil {
		t.Fatal(err)
	}
	for _, line := range m.applied[pkgName] {
		t.Log(line)
	}
	p := m.pkgs[pkgName]
	if p == nil {
		t.Fatalf("mutated fixture %s was not loaded", pkgName)
	}
	seen := map[*packages.Package]bool{}
	var walk func(*packages.Package)
	walk = func(q *packages.Package) {
		for _, dep := range q.Imports {
			if seen[dep] {
				continue
			}
			seen[dep] = true
			if name, ok := strings.CutPrefix(dep.PkgPath, fixturePkgPrefix); ok {
				if _, mutated := mutations[name]; mutated {
					t.Fatalf("mutated fixture %s imports mutated fixture %s, so the shared overlay "+
						"lets one mutation change the other's result; give one of them its own load", pkgName, name)
				}
			}
			walk(dep)
		}
	}
	walk(p)
	findings, _ := analyzeFixture(t, p)
	return findings
}
