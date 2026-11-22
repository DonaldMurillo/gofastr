package settings

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSweepSettingsIsSectionLeavesOutVersion(t *testing.T) {
	for name, want := range map[string]bool{"version": false, "codegen": true, "endpoints": true, "plugins": true, "unmounted": true, "nope": false} {
		if got := IsSection(name); got != want {
			t.Errorf("IsSection(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestSweepSettingsWriteIsOwnerOnly(t *testing.T) {
	p := filepath.Join(t.TempDir(), FileName)
	if err := SetUnmounted(p, []Unmounted{{Source: "github.com/DonaldMurillo/gofastr/battery/auth", Version: "dev", Path: "unmounted/auth", Date: "2026-11-19"}}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows reports no Unix permission bits")
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("settings file mode = %o, want 600", perm)
	}
}

func TestSweepSettingsUnmountedWrittenSorted(t *testing.T) {
	p := filepath.Join(t.TempDir(), FileName)
	in := []Unmounted{
		{Source: "github.com/x/b", Version: "dev", Path: "a", Date: "2026-11-19"},
		{Source: "github.com/x/a", Version: "dev", Path: "z", Date: "2026-11-19"},
		{Source: "github.com/x/a", Version: "dev", Path: "y", Date: "2026-11-19"},
	}
	if err := SetUnmounted(p, in); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	s := string(data)
	y, z, a := strings.Index(s, `"y"`), strings.Index(s, `"z"`), strings.Index(s, `"a"`)
	if y < 0 || z < 0 || a < 0 {
		t.Fatalf("an entry is missing:\n%s", s)
	}
	if !(y < z && z < a) {
		t.Fatalf("entries not sorted by source then path:\n%s", s)
	}
	if in[0].Path != "a" {
		t.Fatal("SetUnmounted reordered the caller's slice")
	}
}

func TestSweepSettingsEmptySectionRemovesFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), FileName)
	if err := SetUnmounted(p, []Unmounted{{Source: "github.com/x/a", Version: "dev", Path: "a", Date: "2026-11-19"}}); err != nil {
		t.Fatal(err)
	}
	if err := SetUnmounted(p, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("a settings file left with no sections still exists: %v", err)
	}
	if err := SetUnmounted(p, nil); err != nil {
		t.Fatalf("an empty write with no file: %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("an empty write created the file")
	}
}

func TestSweepSettingsProjectRootMarkers(t *testing.T) {
	for _, marker := range []string{"go.work", "go.mod", ".git"} {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, marker), 0o700); err != nil {
			t.Fatal(err)
		}
		if !IsProjectRoot(dir) {
			t.Errorf("a directory holding %s is not a project root", marker)
		}
	}
	if IsProjectRoot(t.TempDir()) {
		t.Error("an empty directory is a project root")
	}
}

func TestSweepSettingsEndpointsQuotedKeyRefused(t *testing.T) {
	p := filepath.Join(t.TempDir(), FileName)
	body := "\"endpoints\":\n  - name: \"ping\"\n    entity: \"orders\"\n    method: GET\n    path: \"ping\"\n    handler: \"entities/refunds.go#Ping\"\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetEndpoints(p, nil); err == nil {
		t.Fatal("removal reported success with the entry still there")
	}
	if data, _ := os.ReadFile(p); string(data) != body {
		t.Errorf("file changed:\n%s", data)
	}
}
