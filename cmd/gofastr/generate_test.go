package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestGenerateTypeScriptCommandShowsMigrationError(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	// Keep the package path relative to cmd.Dir so symlinked worktree roots
	// (for example /tmp versus /private/tmp on macOS) cannot split module identity.
	cmd := exec.Command("go", "run", "./cmd/gofastr", "generate", "ts")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected generate ts to fail after removal\n%s", output)
	}
	if !strings.Contains(string(output), "TypeScript codegen has been removed") || !strings.Contains(string(output), "codegen.md") {
		t.Fatalf("unexpected generate ts output:\n%s", output)
	}
}

func TestGenerateProjectDryRunJSONValidatesOutputBeforeExtensions(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "extension-ran")
	extCommand := clitest.WriteReportExtension(t, dir, marker)
	clitest.WriteTestFile(t, filepath.Join(dir, "reports.codegen.json"), `{"name":"reports"}`)
	clitest.WriteTestFile(t, filepath.Join(dir, "gofastr.settings.yml"), `
version: 1
codegen:
  output: generated
  generators:
    - name: custom/reports
      extension: report-generator
      source:
        type: json_file
        path: reports.codegen.json
  extensions:
    - name: report-generator
      command: `+extCommand+`
`)
	cmd := exec.Command("go", "run", "./cmd/gofastr", "generate", "--config="+filepath.Join(dir, "gofastr.settings.yml"), "--out=..", "--dry-run", "--json")
	cmd.Dir = repoRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err == nil {
		t.Fatalf("expected non-zero exit for unsafe output path\n%s", stdout.String())
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("extension executed before output validation: %v", statErr)
	}
	var got struct {
		Files  []any `json:"files"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if jsonErr := json.Unmarshal(stdout.Bytes(), &got); jsonErr != nil {
		t.Fatalf("dry-run JSON errors did not parse: %v\nstdout:\n%s\nstderr:\n%s", jsonErr, stdout.String(), stderr.String())
	}
	if len(got.Files) != 0 || len(got.Errors) != 1 || !strings.Contains(got.Errors[0].Message, "would target the working directory") {
		t.Fatalf("unexpected dry-run JSON error payload: %#v", got)
	}
}
