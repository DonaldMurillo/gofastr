package testcmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestRunTestPassingModule(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module testmod\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte("package x\n\nfunc Add(a, b int) int { return a + b }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// TestAdd fails unless --short and GOFASTR_TEST=1 reach go test; --run must skip TestOther.
	src := "package x\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 || !testing.Short() || os.Getenv(\"GOFASTR_TEST\") != \"1\" {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n\nfunc TestOther(t *testing.T) {}\n"
	if err := os.WriteFile(filepath.Join(dir, "x_test.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	var out string
	code := clitest.CovT_capExit(t, func() {
		out = clitest.CovT_capStdout(t, func() { RunTest([]string{"--run=TestAdd", "--cover", "--short"}) })
	})
	if code != -1 {
		t.Fatalf("passing tests should not exit, got %d: %s", code, out)
	}
	if !regexp.MustCompile(`✓ TestAdd`).MatchString(out) {
		t.Fatalf("TestAdd not reported as passed: %s", out)
	}
	for _, want := range []string{"coverage:", "All tests passed! (1 passed, 0 failed)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q: %s", want, out)
		}
	}
	if strings.Contains(out, "TestOther") {
		t.Fatalf("--run=TestAdd did not filter: %s", out)
	}
}

func TestRunTestFailingModuleExits(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module failmod\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x_test.go"), []byte("package x\n\nimport \"testing\"\n\nfunc TestFail(t *testing.T) {\n\tt.Fatal(\"boom\")\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { RunTest(nil) })
	})
	if code != 1 {
		t.Fatalf("failing tests should exit 1, got %d", code)
	}
}
