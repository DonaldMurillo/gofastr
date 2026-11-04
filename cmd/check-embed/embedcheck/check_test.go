package embedcheck

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestCheckAllFindingsPrecedeUnmatchedPatterns(t *testing.T) {
	t.Chdir(testdataDir(t))
	for _, patterns := range [][]string{
		{"./src/bad", "./empty/..."},
		{"./empty/...", "./src/bad"},
	} {
		findings, _, _, err := CheckAll(patterns...)
		if err != nil || len(findings) != 1 {
			t.Errorf("patterns %v: findings=%v error=%v; want one actionable finding", patterns, findings, err)
		}
	}
	if _, _, _, err := CheckAll("./src/clean", "./empty/..."); err == nil || !strings.Contains(err.Error(), "matched no packages") {
		t.Fatalf("unmatched pattern did not fail a finding-free run: %v", err)
	}
}

func TestCheckAllPreservesPackageAndPatternErrors(t *testing.T) {
	t.Chdir(testdataDir(t))
	for _, patterns := range [][]string{{"./src/loaderror", "./empty/..."}, {"./empty/...", "./src/loaderror"}} {
		_, _, _, err := CheckAll(patterns...)
		if err == nil || !strings.Contains(err.Error(), "doesNotExist") || !strings.Contains(err.Error(), "matched no packages") {
			t.Errorf("patterns=%v: missing package or pattern failure: %v", patterns, err)
		}
	}
}

// checkFixture applies Check's analysis and error precedence (checkLoaded) to
// one fixture from the shared load.
func checkFixture(t *testing.T, pkgName string) ([]Finding, error) {
	t.Helper()
	findings, _, err := checkLoaded([]*packages.Package{fixture(t, pkgName)})
	return findings, err
}

// TestCheckReturnsFindings drives the exported Check end to end, its own
// loader included, from inside the fixture module the way the CLI runs from a
// project root. The precedence tests below reuse the shared load.
func TestCheckReturnsFindings(t *testing.T) {
	t.Chdir(testdataDir(t))
	findings, _, err := Check("./src/bad")
	if err != nil {
		t.Fatalf("Check returned an error with a finding: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("Check returned %d findings, want 1: %+v", len(findings), findings)
	}
}

func TestCheckReturnsLoadErrorWhenNoFindingsExist(t *testing.T) {
	findings, err := checkFixture(t, "loaderror")
	if len(findings) != 0 {
		t.Fatalf("Check returned findings for loaderror: %+v", findings)
	}
	if err == nil {
		t.Fatal("Check hid a package type error behind a clean result")
	}
	if !strings.Contains(err.Error(), "doesNotExist") {
		t.Fatalf("Check error does not include the type failure: %v", err)
	}
}

func TestCheckFindingsPrecedeLoadErrors(t *testing.T) {
	findings, err := checkFixture(t, "findinganderror")
	if err != nil {
		t.Fatalf("Check returned load error instead of actionable finding: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("Check returned %d findings, want 1: %+v", len(findings), findings)
	}
}
