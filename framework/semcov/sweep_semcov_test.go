package semcov

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/axecov"
)

func TestSweepEnableTwiceKeepsUnflushedData(t *testing.T) {
	dir := fresh(t)
	RecordRoute("GET", "/before")
	Enable(dir)
	if err := Flush(); err != nil {
		t.Fatal(err)
	}
	m, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !m.CoveredRoute("GET", "/before") {
		t.Error("a second Enable dropped what was recorded before it")
	}
}

func TestSweepHooksAreCaseInsensitive(t *testing.T) {
	dir := fresh(t)
	RecordHook("Posts", "BeforeCreate")
	if err := Flush(); err != nil {
		t.Fatal(err)
	}
	m, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Hooks["posts"]; len(got) != 1 || got[0] != "beforecreate" {
		t.Errorf("hooks = %v, want posts: [beforecreate]", m.Hooks)
	}
}

func TestSweepNilManifestAnswersFalseEverywhere(t *testing.T) {
	var m *Manifest
	if m.CoveredRoute("GET", "/") || m.CoveredEntity("x") || m.CoveredHook("x", "y") ||
		m.CoveredEvent("x") || m.CoveredRole("x") || m.CoveredPermission("x:y") {
		t.Error("a nil manifest reported something covered")
	}
}

func TestSweepManifestIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	dir := fresh(t)
	RecordRoute("GET", "/x")
	if err := Flush(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("manifest mode = %o, want 600", mode)
	}
}

func TestSweepDefaultDirMatchesAxecov(t *testing.T) {
	t.Setenv("GOFASTR_SEMANTIC_COVERAGE_DIR", "")
	t.Setenv("GOFASTR_AXE_COVERAGE_DIR", "")
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)
	if s, a := DefaultDir(), axecov.DefaultDir(); s != a {
		t.Errorf("semcov.DefaultDir %q, axecov.DefaultDir %q; want one coverage root", s, a)
	}
	t.Setenv("GOFASTR_AXE_COVERAGE_DIR", "/axe")
	if s, a := DefaultDir(), axecov.DefaultDir(); s != a {
		t.Errorf("under the axe override: semcov %q, axecov %q", s, a)
	}
}
