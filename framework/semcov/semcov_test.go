package semcov

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func fresh(t *testing.T) string {
	t.Helper()
	Reset()
	t.Cleanup(Reset)
	dir := t.TempDir()
	Enable(dir)
	return dir
}

func TestRecordingIsOffUntilEnabled(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	if Enabled() {
		t.Fatal("recording is on before Enable")
	}
	// The production path: every hook is a no-op, and Flush writes nothing.
	RecordRoute("GET", "/x")
	RecordPermission("users:read")
	RecordEntityOp("users", "list")
	if err := Flush(); err != nil {
		t.Fatalf("Flush with recording off: %v", err)
	}
}

func TestRecordAndRead(t *testing.T) {
	dir := fresh(t)
	RecordRoute("get", "/users/{id}")
	RecordRoute("GET", "/users/{id}") // deduped, and case-normalised
	RecordRoute("POST", "/users")
	RecordPermission("users:write")
	RecordEntityOp("Users", "Create")

	if err := Flush(); err != nil {
		t.Fatal(err)
	}
	m, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Routes["/users/{id}"]; len(got) != 1 || got[0] != "GET" {
		t.Errorf("routes = %v, want one GET", got)
	}
	if !m.CoveredRoute("post", "/users") {
		t.Error("POST /users not recorded")
	}
	if !m.CoveredPermission("users:write") {
		t.Error("permission not recorded")
	}
	if !m.CoveredEntity("users") {
		t.Error("entity not recorded (name should be case-insensitive)")
	}
	if m.CoveredRoute("DELETE", "/users") {
		t.Error("a method that never ran is reported as covered")
	}
}

func TestNormalizePatternDropsQueryAndTrailingSlash(t *testing.T) {
	dir := fresh(t)
	RecordRoute("GET", "/docs/?tab=x")
	RecordRoute("GET", "/pricing/")
	RecordRoute("GET", "/")
	if err := Flush(); err != nil {
		t.Fatal(err)
	}
	m, _ := Read(dir)
	for _, want := range []string{"/docs", "/pricing", "/"} {
		if _, ok := m.Routes[want]; !ok {
			t.Errorf("missing %q in %v", want, m.Routes)
		}
	}
}

func TestFlushMergesWithWhatIsOnDisk(t *testing.T) {
	// `go test ./...` runs one process per package. Without the merge the
	// last package to finish would be the only one recorded, and every
	// other package's routes would report as unexercised.
	dir := t.TempDir()

	Reset()
	Enable(dir)
	RecordRoute("GET", "/from-package-a")
	if err := Flush(); err != nil {
		t.Fatal(err)
	}

	Reset()
	Enable(dir)
	RecordRoute("GET", "/from-package-b")
	if err := Flush(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(Reset)

	m, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/from-package-a", "/from-package-b"} {
		if _, ok := m.Routes[want]; !ok {
			t.Errorf("%q lost in the merge: %v", want, m.Routes)
		}
	}
}

func TestFlushIsANoOpWhenNothingChanged(t *testing.T) {
	dir := fresh(t)
	RecordRoute("GET", "/x")
	if err := Flush(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, FileName)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// A per-test Flush must be free when that test recorded nothing new;
	// a suite of thousands would otherwise re-serialise the manifest for
	// every one of them.
	if err := Flush(); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("a clean Flush rewrote the manifest")
	}
}

func TestReadMissingManifestIsNotExist(t *testing.T) {
	// The reader has to distinguish "never recorded" (fine on a fresh
	// clone) from "recorded and incomplete" (real drift).
	_, err := Read(t.TempDir())
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestReadRejectsFutureVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version": 99, "routes": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(dir); err == nil {
		t.Fatal("a newer manifest was parsed as if this build understood it")
	}
}

func TestReadRejectsCorruptManifest(t *testing.T) {
	// Corruption is not absence. Relaxing enforcement exactly when the
	// record is untrustworthy would invert the guarantee.
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Read(dir)
	if err == nil {
		t.Fatal("corrupt manifest read as valid")
	}
	if errors.Is(err, fs.ErrNotExist) {
		t.Fatal("corruption reported as absence")
	}
}

func TestConcurrentRecordingLosesNothing(t *testing.T) {
	dir := fresh(t)
	var wg sync.WaitGroup
	paths := []string{"/a", "/b", "/c", "/d", "/e", "/f", "/g", "/h"}
	for _, p := range paths {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			RecordRoute("GET", p)
			RecordEntityOp("things", "list")
			RecordPermission("things:read")
		}(p)
	}
	wg.Wait()
	if err := Flush(); err != nil {
		t.Fatal(err)
	}
	m, _ := Read(dir)
	for _, p := range paths {
		if !m.CoveredRoute("GET", p) {
			t.Errorf("%s lost under concurrency", p)
		}
	}
}

func TestDefaultDirPrefersExplicitOverride(t *testing.T) {
	t.Setenv("GOFASTR_SEMANTIC_COVERAGE_DIR", "/tmp/explicit")
	if got := DefaultDir(); got != "/tmp/explicit" {
		t.Errorf("DefaultDir = %q", got)
	}
}

func TestDefaultDirFallsBackToAxeOverride(t *testing.T) {
	// Sharing the override means an app that already pinned its axe
	// manifest per-app gets the same isolation here without configuring
	// it twice.
	t.Setenv("GOFASTR_SEMANTIC_COVERAGE_DIR", "")
	t.Setenv("GOFASTR_AXE_COVERAGE_DIR", "/tmp/axe")
	if got := DefaultDir(); got != "/tmp/axe" {
		t.Errorf("DefaultDir = %q", got)
	}
}

func TestNilManifestQueriesAreFalse(t *testing.T) {
	var m *Manifest
	if m.CoveredRoute("GET", "/x") || m.CoveredEntity("x") || m.CoveredPermission("x:y") {
		t.Error("a nil manifest claimed coverage")
	}
}

func TestRecordHookEventAndRole(t *testing.T) {
	dir := fresh(t)
	RecordHook("Posts", "BeforeCreate") // case-normalised on the way in
	RecordHook("posts", "beforecreate") // deduped
	RecordEvent("order.placed")
	RecordEvent("order.placed")
	RecordRole("editor")
	RecordRole("editor")

	if err := Flush(); err != nil {
		t.Fatal(err)
	}
	m, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Hooks["posts"]; len(got) != 1 || got[0] != "beforecreate" {
		t.Errorf("hooks = %v, want one beforecreate", got)
	}
	if len(m.Events) != 1 || len(m.Roles) != 1 {
		t.Errorf("events = %v, roles = %v — both should dedupe", m.Events, m.Roles)
	}
	if !m.CoveredHook("POSTS", "BEFORECREATE") {
		t.Error("CoveredHook is not case-insensitive")
	}
	if !m.CoveredEvent("order.placed") || !m.CoveredRole("editor") {
		t.Error("recorded values do not read back")
	}
	if m.CoveredHook("posts", "afterdelete") || m.CoveredEvent("nope") || m.CoveredRole("admin") {
		t.Error("unrecorded values reported as covered")
	}
}

func TestRecordersIgnoreBlankInput(t *testing.T) {
	dir := fresh(t)
	// A blank key would create an unnameable manifest entry that no rule
	// could ever match, so it is dropped at the door.
	RecordHook("", "beforecreate")
	RecordHook("posts", "")
	RecordEvent("   ")
	RecordRole("")
	RecordPermission("  ")
	RecordRoute("GET", "")
	if err := Flush(); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("blank input produced a manifest: %v", err)
	}
}

func TestDisableStopsRecordingButKeepsData(t *testing.T) {
	dir := fresh(t)
	RecordRoute("GET", "/before")
	Disable()
	RecordRoute("GET", "/after")
	if Enabled() {
		t.Fatal("Disable did not take effect")
	}
	// Data already accumulated survives, so a deferred Flush still writes
	// what the run proved before it was switched off.
	if err := Flush(); err != nil {
		t.Fatal(err)
	}
	m, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !m.CoveredRoute("GET", "/before") {
		t.Error("data recorded before Disable was lost")
	}
	if m.CoveredRoute("GET", "/after") {
		t.Error("recording continued after Disable")
	}
}

func TestDefaultDirWalksToTheModuleRoot(t *testing.T) {
	// Writer and reader must resolve the same directory even when their
	// working directories differ — a test binary in ./sub and a server at
	// the root both have to land on the root.
	t.Setenv("GOFASTR_SEMANTIC_COVERAGE_DIR", "")
	t.Setenv("GOFASTR_AXE_COVERAGE_DIR", "")
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "sub", "deeper")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)

	got, err := filepath.EvalSymlinks(DefaultDir())
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("DefaultDir() = %q, want the module root %q", got, want)
	}
}

func TestMergeKeepsEveryDimension(t *testing.T) {
	dir := t.TempDir()
	Reset()
	t.Cleanup(Reset)

	Enable(dir)
	RecordRoute("GET", "/a")
	RecordPermission("posts:read")
	RecordEntityOp("posts", "list")
	RecordHook("posts", "beforecreate")
	RecordEvent("a.happened")
	RecordRole("editor")
	if err := Flush(); err != nil {
		t.Fatal(err)
	}

	Reset()
	Enable(dir)
	RecordRoute("POST", "/b")
	RecordPermission("posts:write")
	RecordEntityOp("comments", "create")
	RecordHook("comments", "afterdelete")
	RecordEvent("b.happened")
	RecordRole("admin")
	if err := Flush(); err != nil {
		t.Fatal(err)
	}

	m, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	// One process per package means every dimension has to survive the
	// merge, not just the ones the last package touched.
	for _, check := range []struct {
		name string
		ok   bool
	}{
		{"route a", m.CoveredRoute("GET", "/a")},
		{"route b", m.CoveredRoute("POST", "/b")},
		{"permission a", m.CoveredPermission("posts:read")},
		{"permission b", m.CoveredPermission("posts:write")},
		{"entity a", m.CoveredEntity("posts")},
		{"entity b", m.CoveredEntity("comments")},
		{"hook a", m.CoveredHook("posts", "beforecreate")},
		{"hook b", m.CoveredHook("comments", "afterdelete")},
		{"event a", m.CoveredEvent("a.happened")},
		{"event b", m.CoveredEvent("b.happened")},
		{"role a", m.CoveredRole("editor")},
		{"role b", m.CoveredRole("admin")},
	} {
		if !check.ok {
			t.Errorf("%s was lost in the merge", check.name)
		}
	}
}
