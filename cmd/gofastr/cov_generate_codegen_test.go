package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/codegen"
)

func TestEnterCodegenProjectDir(t *testing.T) {
	// "." project dir → no-op restore.
	restore, err := enterCodegenProjectDir(codegen.Discovery{ProjectDir: "."})
	if err != nil {
		t.Fatal(err)
	}
	restore()
	// Real subdir.
	dir := t.TempDir()
	sub := filepath.Join(dir, "proj")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	restore, err = enterCodegenProjectDir(codegen.Discovery{ProjectDir: sub})
	if err != nil {
		t.Fatalf("enter: %v", err)
	}
	restore()
}
