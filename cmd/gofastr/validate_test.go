package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// --- Bug 4: gofastr validate <gofastr.yml>.

func TestValidateCmdValidExitsZero(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteFileAll(t, path, `
app:
  name: Demo
entities:
  - name: users
    fields:
      - name: username
        type: string
  - name: posts
    fields:
      - name: title
        type: string
      - name: author_id
        type: relation
        to: users
`)
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { dispatch([]string{"validate", path}) })
	})
	if code != -1 {
		t.Fatalf("valid app spec should not exit, got %d\n%s", code, out)
	}
	if !strings.Contains(out, "valid") {
		t.Fatalf("expected success message, got:\n%s", out)
	}
}

func TestValidateCmdBadRelationExits1(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteFileAll(t, path, `
entities:
  - name: posts
    fields:
      - name: author_id
        type: relation
        to: users
`)
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { dispatch([]string{"validate", path}) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d\n%s", code, out)
	}
	// Agent-facing: the error names the file, the field, and the remedy.
	for _, want := range []string{path, "author_id", `"users"`, "declare"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output should contain %q:\n%s", want, out)
		}
	}
}
