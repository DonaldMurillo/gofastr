package axecov

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestRecordMergesPathsAndSchemes(t *testing.T) {
	dir := t.TempDir()
	if err := Record(dir, "/docs", "dark"); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := Record(dir, "/docs", "light"); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := Record(dir, "/docs", "dark"); err != nil { // duplicate stays deduped
		t.Fatalf("record: %v", err)
	}
	if err := Record(dir, "/", "dark"); err != nil {
		t.Fatalf("record: %v", err)
	}

	m, err := Read(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if m.Version != 1 {
		t.Fatalf("version = %d, want 1", m.Version)
	}
	if got := m.Pages["/docs"]; len(got) != 2 {
		t.Fatalf("/docs schemes = %v, want [dark light]", got)
	}
	if _, ok := m.Pages["/"]; !ok {
		t.Fatalf("missing / entry: %v", m.Pages)
	}
}

func TestReadMissingManifestReturnsNotExist(t *testing.T) {
	_, err := Read(t.TempDir())
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestRecordNormalizesPath(t *testing.T) {
	dir := t.TempDir()
	// Query strings and fragments identify the same screen; empty paths
	// are the root.
	if err := Record(dir, "/pricing?tab=teams", "dark"); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := Record(dir, "", "dark"); err != nil {
		t.Fatalf("record: %v", err)
	}
	m, err := Read(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, ok := m.Pages["/pricing"]; !ok {
		t.Fatalf("query string not stripped: %v", m.Pages)
	}
	if _, ok := m.Pages["/"]; !ok {
		t.Fatalf("empty path not normalized to /: %v", m.Pages)
	}
}

func TestRecordConcurrentLosesNothing(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	paths := []string{"/a", "/b", "/c", "/d", "/e", "/f", "/g", "/h"}
	for _, p := range paths {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			if err := Record(dir, p, "dark"); err != nil {
				t.Errorf("record %s: %v", p, err)
			}
		}(p)
	}
	wg.Wait()
	m, err := Read(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, p := range paths {
		if _, ok := m.Pages[p]; !ok {
			t.Fatalf("lost %s under concurrency: %v", p, m.Pages)
		}
	}
}

func TestManifestLandsAtDocumentedLocation(t *testing.T) {
	dir := t.TempDir()
	if err := Record(dir, "/", "dark"); err != nil {
		t.Fatalf("record: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
		t.Fatalf("manifest not at %s: %v", FileName, err)
	}
}

func TestNormalizePathDropsBareHost(t *testing.T) {
	dir := t.TempDir()
	// A scan of the bare origin (no trailing slash) is a scan of "/".
	if err := Record(dir, "http://localhost:8080", "dark"); err != nil {
		t.Fatalf("record: %v", err)
	}
	m, err := Read(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, ok := m.Pages["/"]; !ok {
		t.Fatalf("bare-host URL not normalized to /: %v", m.Pages)
	}
}

func TestReadCorruptManifestIsNotNotExist(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".gofastr"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("{corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Read(dir)
	if err == nil {
		t.Fatal("corrupt manifest read did not error")
	}
	if errors.Is(err, fs.ErrNotExist) {
		t.Fatal("corrupt manifest must not read as absent — absence relaxes enforcement, corruption must not")
	}
}

func TestDefaultDirPrefersEnvThenModuleRoot(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "cmd", "app")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	// macOS tempdirs live behind /private symlinks; compare resolved paths.
	wantRoot, _ := filepath.EvalSymlinks(root)
	if got, _ := filepath.EvalSymlinks(DefaultDir()); got != wantRoot {
		t.Fatalf("DefaultDir from %s = %q, want module root %q", sub, got, wantRoot)
	}
	override := t.TempDir()
	t.Setenv("GOFASTR_AXE_COVERAGE_DIR", override)
	if got := DefaultDir(); got != override {
		t.Fatalf("env override ignored: got %q want %q", got, override)
	}
}

func TestDefaultDirFallsBackToCwdWithoutModule(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	got := DefaultDir()
	// macOS tempdirs resolve through /private symlinks — compare resolved.
	wd, _ := os.Getwd()
	if got != wd && got != dir {
		t.Fatalf("DefaultDir without go.mod = %q, want cwd %q", got, wd)
	}
}

func TestDefaultDirResolvesOutermostWorkspaceRoot(t *testing.T) {
	// A workspace root (go.work) above a nested module (go.mod): the
	// test binary in the nested module and a dev server at the
	// workspace root must resolve the SAME directory — the outermost.
	root := t.TempDir()
	nested := filepath.Join(root, "apps", "shop", "cmd", "app")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.work"), []byte("go 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "apps", "shop", "go.mod"), []byte("module example.com/shop\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantRoot, _ := filepath.EvalSymlinks(root)
	t.Chdir(nested)
	if got, _ := filepath.EvalSymlinks(DefaultDir()); got != wantRoot {
		t.Fatalf("from nested module: DefaultDir = %q, want workspace root %q", got, wantRoot)
	}
	t.Chdir(root)
	if got, _ := filepath.EvalSymlinks(DefaultDir()); got != wantRoot {
		t.Fatalf("from workspace root: DefaultDir = %q, want %q", got, wantRoot)
	}
}
