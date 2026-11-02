package initcmd

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// init read its flags only after the name and only in the --flag=value
// form: `gofastr init app --module example.com/app` failed with
// `Unknown flag "--module"` (a flag --help lists), and
// `gofastr init --no-entity app` refused "--no-entity" as the project
// name. Flags now go anywhere, valued ones in either form.
func TestSplitInitArgs(t *testing.T) {
	for _, tc := range []struct {
		args      []string
		name      string
		flags     []string
		errSubstr string
	}{
		{args: []string{"app"}, name: "app"},
		{args: []string{"app", "--module=example.com/app"}, name: "app", flags: []string{"--module=example.com/app"}},
		{args: []string{"app", "--module", "example.com/app"}, name: "app", flags: []string{"--module=example.com/app"}},
		{args: []string{"--module", "example.com/app", "app"}, name: "app", flags: []string{"--module=example.com/app"}},
		{args: []string{"--no-entity", "app", "--db", "postgres"}, name: "app", flags: []string{"--no-entity", "--db=postgres"}},
		{args: []string{"--module=example.com/app"}, flags: []string{"--module=example.com/app"}},
		{args: []string{"app", "--module"}, errSubstr: "--module needs a value"},
		{args: []string{"app", "--db", "--no-entity"}, errSubstr: "--db needs a value"},
		{args: []string{"app", "other"}, errSubstr: `unexpected argument "other"`},
		{args: []string{"app", "--bogus"}, name: "app", flags: []string{"--bogus"}},
	} {
		name, flags, err := splitInitArgs(tc.args)
		if tc.errSubstr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.errSubstr) {
				t.Errorf("splitInitArgs(%q) err = %v, want %q", tc.args, err, tc.errSubstr)
			}
			continue
		}
		if err != nil {
			t.Errorf("splitInitArgs(%q) err = %v", tc.args, err)
			continue
		}
		if name != tc.name || !slices.Equal(flags, tc.flags) {
			t.Errorf("splitInitArgs(%q) = %q, %q; want %q, %q", tc.args, name, flags, tc.name, tc.flags)
		}
	}
}

// The space form reaches the scaffold: go.mod names the module given.
func TestInitModuleFlagSpaceForm(t *testing.T) {
	inPlaceDir(t, "work")
	if code := runInitCode(t, "--no-entity", "app", "--module", "example.com/spaced"); code != -1 {
		t.Fatalf("init exited %d", code)
	}
	data, err := os.ReadFile(filepath.Join("app", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "module example.com/spaced\n") {
		t.Fatalf("go.mod does not name the --module value:\n%s", data)
	}
}

// A --module go mod init refuses was caught only after every other file
// was written: init left a half-scaffolded directory and told the user to
// run go mod init by hand with the same bad path. It is refused before
// anything is written.
func TestInitRefusesMalformedModuleBeforeWriting(t *testing.T) {
	inPlaceDir(t, "work")
	for _, mod := range []string{"bad module", "example.com/a//b", "/abs/path", "example.com/a\x01"} {
		if code := runInitCode(t, "app", "--module="+mod); code != 1 {
			t.Fatalf("init --module=%q exited %d, want 1", mod, code)
		}
		if _, err := os.Stat("app"); err == nil {
			t.Fatalf("init --module=%q created the project before refusing it", mod)
		}
	}
}

// --reinit refreshes an existing project. Given a name with no directory
// (a typo of the project's name), it created that directory, filled it
// with onboarding files, and reported "Reinit complete".
func TestInitReinitRefusesMissingProject(t *testing.T) {
	inPlaceDir(t, "work")
	if code := runInitCode(t, "my-ap", "--reinit"); code != 1 {
		t.Fatalf("init my-ap --reinit exited %d, want 1", code)
	}
	if _, err := os.Stat("my-ap"); err == nil {
		t.Fatal("init --reinit created the directory it was asked to refresh")
	}
	if err := os.WriteFile("file", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runInitCode(t, "file", "--reinit"); code != 1 {
		t.Fatalf("init file --reinit exited %d, want 1", code)
	}
}

// A missing value is a usage error, exit 2, and writes nothing.
func TestInitModuleFlagMissingValue(t *testing.T) {
	inPlaceDir(t, "work")
	if code := runInitCode(t, "app", "--module"); code != 2 {
		t.Fatalf("init app --module exited %d, want 2", code)
	}
	if _, err := os.Stat("app"); err == nil {
		t.Error("init with a valueless --module created the project")
	}
}
