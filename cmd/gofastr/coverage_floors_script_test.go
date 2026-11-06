package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

//gofastr:test-reads scripts/coverage-floors.sh
func TestCoverageReportsMissingAndContinues(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	source, err := os.ReadFile(filepath.Join("..", "..", "scripts", "coverage-floors.sh"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(dir, "scripts", "coverage-floors.sh"), string(source))
	fakeGo := filepath.Join(dir, "go")
	body := `#!/bin/bash
if [[ "$*" == *'./battery/admin/'* || "$*" == *'./battery/cache/'* ]]; then
  echo '? package [no test files]'
  exit 0
fi
for arg in "$@"; do
  case "$arg" in
    -coverprofile=*) printf 'mode: set\nexample/file.go:1.1,2.2 1 1\n' > "${arg#-coverprofile=}";;
  esac
done
echo 'ok package coverage: 100.0% of statements'
`
	if err := os.WriteFile(fakeGo, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash", "scripts/coverage-floors.sh")
	command.Dir = dir
	command.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "GOFASTR_TEST_ALL=1", "COVERPROFILE=")
	output, err := command.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("exit=%v stdout=%s", err, output)
	}
	for _, pkg := range []string{"./battery/admin/", "./battery/cache/"} {
		if !strings.Contains(string(output), "FAIL  "+pkg+" — could not compute a coverage percentage") {
			t.Errorf("missing diagnostic for %s: %s", pkg, output)
		}
	}
	if !strings.Contains(string(output), "ok    ./battery/auth/") || !strings.Contains(string(output), "ok    ./battery/log/") {
		t.Errorf("coverage checks stopped after missing measurement: %s", output)
	}
}
