package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func authCopy() Unmounted {
	return Unmounted{
		Source:  "github.com/DonaldMurillo/gofastr/battery/auth",
		Version: "v0.91.0",
		Path:    "unmounted/battery/auth",
		Date:    "2026-10-19",
	}
}

// The section is rewritten in place: the rest of the file keeps its
// bytes, comments included, and the entries read back as written.
func TestSetUnmountedKeepsTheRestOfTheFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), FileName)
	head := "# project settings\nversion: 1\n\ncontracts:\n  # perf is fine here\n  performance: warn\n"
	tail := "\nisolation:\n  enabled: true\n"
	writeFile(t, p, head+"\nunmounted:\n  - source: \"old\"\n"+tail)
	if err := SetUnmounted(p, []Unmounted{authCopy()}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	got := string(data)
	if !strings.HasPrefix(got, head) || !strings.Contains(got, "isolation:\n  enabled: true\n") {
		t.Fatalf("other sections changed:\n%s", got)
	}
	f, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := f.Unmounted()
	if err != nil || len(entries) != 1 || entries[0] != authCopy() {
		t.Fatalf("entries = %+v, err = %v", entries, err)
	}

	// An empty list removes the section and the line gofastr wrote above it.
	if err := SetUnmounted(p, nil); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(p)
	if strings.Contains(string(data), "unmounted") || !strings.Contains(string(data), "performance: warn") {
		t.Fatalf("after removal:\n%s", data)
	}
}

func TestSetUnmountedCreatesTheFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), FileName)
	if err := SetUnmounted(p, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("an empty registry wrote a file: %v", err)
	}
	if err := SetUnmounted(p, []Unmounted{authCopy()}); err != nil {
		t.Fatal(err)
	}
	f, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if entries, err := f.Unmounted(); err != nil || len(entries) != 1 {
		t.Fatalf("entries = %+v, err = %v", entries, err)
	}
	if data, _ := os.ReadFile(p); !strings.HasPrefix(string(data), "# Written by gofastr unmount") {
		t.Errorf("a new file does not start with the section:\n%q", data)
	}
	if err := SetUnmounted(p, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("removing the last entry left an empty file: %v", err)
	}
}

// Every field lands in YAML and in generated Go: a value outside its
// shape is refused on write and on read.
func TestUnmountedRefusesBadEntries(t *testing.T) {
	for name, mutate := range map[string]func(*Unmounted){
		"source with a quote":  func(u *Unmounted) { u.Source = `github.com/x/"y` },
		"version with a space": func(u *Unmounted) { u.Version = "v1 x" },
		"absolute path":        func(u *Unmounted) { u.Path = "/etc/auth" },
		"escaping path":        func(u *Unmounted) { u.Path = "../auth" },
		"unclean path":         func(u *Unmounted) { u.Path = "a//b" },
		"vendored path":        func(u *Unmounted) { u.Path = "lib/vendor/auth" },
		"bad date":             func(u *Unmounted) { u.Date = "yesterday" },
	} {
		u := authCopy()
		mutate(&u)
		if err := SetUnmounted(filepath.Join(t.TempDir(), FileName), []Unmounted{u}); err == nil {
			t.Errorf("%s: written", name)
		}
	}
	for name, body := range map[string]string{
		"not a list":    "unmounted:\n  source: x\n",
		"unknown key":   "unmounted:\n  - source: \"github.com/a/b\"\n    version: \"dev\"\n    path: \"u/b\"\n    date: \"2026-10-19\"\n    note: \"x\"\n",
		"missing field": "unmounted:\n  - source: \"github.com/a/b\"\n    version: \"dev\"\n    path: \"u/b\"\n",
		"listed twice": "unmounted:\n  - source: \"github.com/a/b\"\n    version: \"dev\"\n    path: \"u/b\"\n    date: \"2026-10-19\"\n" +
			"  - source: \"github.com/a/c\"\n    version: \"dev\"\n    path: \"u/b\"\n    date: \"2026-10-19\"\n",
	} {
		f, err := Parse("s.yml", []byte(body))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := f.Unmounted(); err == nil {
			t.Errorf("%s: read", name)
		}
	}
}

func TestSetUnmountedRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "elsewhere.yml")
	writeFile(t, target, "version: 1\n")
	p := filepath.Join(dir, FileName)
	if err := os.Symlink(target, p); err != nil {
		t.Skip(err)
	}
	if err := SetUnmounted(p, []Unmounted{authCopy()}); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("err = %v", err)
	}
	if data, _ := os.ReadFile(target); string(data) != "version: 1\n" {
		t.Errorf("the link's target changed:\n%s", data)
	}
}

// A section key the rewrite does not recognise must not let a removal
// report success while the entry stays.
func TestSetUnmountedQuotedKeyRefused(t *testing.T) {
	p := filepath.Join(t.TempDir(), FileName)
	body := "\"unmounted\":\n  - source: \"github.com/DonaldMurillo/gofastr/battery/auth\"\n    version: \"v0.91.0\"\n    path: \"unmounted/battery/auth\"\n    date: \"2026-10-19\"\n"
	writeFile(t, p, body)
	if err := SetUnmounted(p, nil); err == nil {
		t.Fatal("removal reported success with the entry still there")
	}
	if data, _ := os.ReadFile(p); string(data) != body {
		t.Errorf("file changed:\n%s", data)
	}
}

// The rewrite goes through a temporary file renamed over the old one, so
// a failed write cannot leave a truncated settings file. A hard link to
// the old file shows which happened: a rename leaves it holding the old
// bytes, a truncate-and-write changes it too.
func TestSetUnmountedReplacesFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, FileName)
	body := "contracts:\n  performance: warn\n"
	writeFile(t, p, body)
	alias := filepath.Join(dir, "alias.yml")
	if err := os.Link(p, alias); err != nil {
		t.Skip(err)
	}
	if err := SetUnmounted(p, []Unmounted{authCopy()}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(alias); string(data) != body {
		t.Errorf("the old file was rewritten in place:\n%s", data)
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, ".*tmp-*")); len(matches) > 0 {
		t.Errorf("temporary files left: %v", matches)
	}
}

// A chrome package copied once per binary is one source at two paths.
func TestUnmountedSameSourceTwoPaths(t *testing.T) {
	p := filepath.Join(t.TempDir(), FileName)
	alpha, beta := authCopy(), authCopy()
	alpha.Path, beta.Path = "alpha/auth", "beta/auth"
	if err := SetUnmounted(p, []Unmounted{beta, alpha}); err != nil {
		t.Fatal(err)
	}
	f, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := f.Unmounted(); err != nil || len(got) != 2 || got[0] != alpha {
		t.Fatalf("entries = %+v, err = %v", got, err)
	}
}

// A column-0 comment between two entries is part of the section, not
// the end of it; a comment above the next section stays with that one.
func TestSetUnmountedCommentBetweenEntries(t *testing.T) {
	p := filepath.Join(t.TempDir(), FileName)
	writeFile(t, p, "unmounted:\n  - source: \"github.com/a/b\"\n    version: \"dev\"\n    path: \"u/b\"\n    date: \"2026-10-19\"\n"+
		"# the alpha copy\n  - source: \"github.com/a/c\"\n    version: \"dev\"\n    path: \"u/c\"\n    date: \"2026-10-19\"\n"+
		"\n# isolation settings\nisolation:\n  enabled: true\n")
	keep := Unmounted{Source: "github.com/a/c", Version: "dev", Path: "u/c", Date: "2026-10-19"}
	if err := SetUnmounted(p, []Unmounted{keep}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	if strings.Contains(string(data), "u/b") || !strings.Contains(string(data), "# isolation settings\nisolation:") {
		t.Errorf("rewrite:\n%s", data)
	}
}
