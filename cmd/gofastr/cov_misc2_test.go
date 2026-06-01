package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDocsNoArgsLists(t *testing.T) {
	out := covT_capStdout(t, func() { runDocs(nil) })
	if !strings.Contains(out, "framework docs") {
		t.Fatalf("docs list: %s", out)
	}
}

func TestRunThemeInitRouting(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	covT_capStdout(t, func() { runTheme([]string{"init"}) })
	if _, err := os.Stat(filepath.Join(dir, "theme", "theme.go")); err != nil {
		t.Fatalf("theme init via runTheme failed: %v", err)
	}
}

func TestRunDocsGrepShortFlag(t *testing.T) {
	out := covT_capStdout(t, func() { runDocs([]string{"-g", "architecture"}) })
	if out == "" {
		t.Fatal("expected grep output")
	}
}
