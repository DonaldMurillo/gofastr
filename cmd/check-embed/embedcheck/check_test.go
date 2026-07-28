package embedcheck

import (
	"os"
	"strings"
	"testing"
)

func checkFixture(t *testing.T, pkgName string) ([]Finding, error) {
	t.Helper()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(testdataDir(t)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	}()

	findings, _, err := Check("./src/" + pkgName)
	return findings, err
}

func TestCheckReturnsFindings(t *testing.T) {
	findings, err := checkFixture(t, "bad")
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
