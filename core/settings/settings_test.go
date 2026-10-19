package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFindWalksUpToProjectRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n")
	writeFile(t, filepath.Join(root, FileName), "codegen:\n  output: gen\n")
	f, ok, err := Find(filepath.Join(root, "a", "b"), "codegen")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if f.Section("codegen") == nil || f.Section("contracts") != nil {
		t.Fatalf("sections: codegen=%v contracts=%v", f.Section("codegen"), f.Section("contracts"))
	}
	if f.Dir != root {
		t.Fatalf("dir = %s, want %s", f.Dir, root)
	}
}

// A settings file above the project root is someone else's: never read.
func TestFindStopsAtProjectRoot(t *testing.T) {
	outer := t.TempDir()
	writeFile(t, filepath.Join(outer, FileName), "codegen:\n  output: gen\n")
	inner := filepath.Join(outer, "app")
	writeFile(t, filepath.Join(inner, "go.mod"), "module example.com/app\n")
	if _, ok, err := Find(inner, "codegen"); err != nil || ok {
		t.Fatalf("found the parent's settings: ok=%v err=%v", ok, err)
	}
}

// With no root marker anywhere, only the start directory counts.
func TestFindUnmarkedTreeSearchesStartOnly(t *testing.T) {
	start := t.TempDir()
	for d := start; filepath.Dir(d) != d; d = filepath.Dir(d) {
		if IsProjectRoot(d) {
			t.Skipf("the temp dir sits under the project root %s", d)
		}
	}
	dirs, err := SearchDirs(start)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 1 || dirs[0] != start {
		t.Fatalf("dirs = %v, want [%s]", dirs, start)
	}
}

func TestParseRefusesUnknownSection(t *testing.T) {
	_, err := Parse("s.yml", []byte("codegen:\n  output: gen\nentities:\n  - name: x\n"))
	if err == nil || !strings.Contains(err.Error(), `unknown section "entities"`) ||
		!strings.Contains(err.Error(), "codegen, contracts, isolation") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseVersion(t *testing.T) {
	if _, err := Parse("s.yml", []byte("version: 1\n")); err != nil {
		t.Fatalf("version 1: %v", err)
	}
	for _, v := range []string{"2", "\"1\"", "one"} {
		if _, err := Parse("s.yml", []byte("version: "+v+"\n")); err == nil {
			t.Errorf("version %s accepted", v)
		}
	}
}

func TestParseEmptyAndNonMap(t *testing.T) {
	f, err := Parse("s.yml", nil)
	if err != nil || f.Section("codegen") != nil {
		t.Fatalf("empty: %v", err)
	}
	if _, err := Parse("s.yml", []byte("- a\n")); err == nil {
		t.Fatal("a list accepted as settings")
	}
}

// A file the settings file replaced is refused by its own reader, never
// dropped in silence; another reader is not stopped by it.
func TestFindRefusesRetiredFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n")
	writeFile(t, filepath.Join(root, "gofastr.codegen.yml"), "version: 1\ncodegen:\n  output: gen\n")
	_, _, err := Find(root, "codegen")
	if err == nil || !strings.Contains(err.Error(), "gofastr.codegen.yml is no longer read") ||
		!strings.Contains(err.Error(), `"codegen:"`) || !strings.Contains(err.Error(), FileName) {
		t.Fatalf("err = %v", err)
	}
	if _, ok, err := Find(root, "isolation"); err != nil || ok {
		t.Fatalf("isolation reader: ok=%v err=%v", ok, err)
	}
}
