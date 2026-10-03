package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// fixture is a tiny module with the import shapes the tool must read:
//
//	a            leaf, embeds assets/data.txt
//	b  → a       plus b/testdata/fixture.txt
//	c            leaf
//	d            no imports; d_test.go (package d_test) imports c
//	e            no imports; e_test.go (package e) imports b
//	f            isolated, imports nothing, imported by nothing
var fixture = map[string]string{
	"go.mod":                 "module example.com/m\n\ngo 1.22\n",
	"README.md":              "docs\n",
	"scripts/run.sh":         "echo hi\n",
	"a/a.go":                 "package a\n\nimport _ \"embed\"\n\n//go:embed assets/data.txt\nvar Data string\n\nfunc A() int { return 1 }\n",
	"a/assets/data.txt":      "payload\n",
	"b/b.go":                 "package b\n\nimport \"example.com/m/a\"\n\nfunc B() int { return a.A() + 1 }\n",
	"b/testdata/fixture.txt": "fixture\n",
	"c/c.go":                 "package c\n\nfunc C() int { return 3 }\n",
	"d/d.go":                 "package d\n\nfunc D() int { return 4 }\n",
	"d/d_test.go":            "package d_test\n\nimport (\n\t\"testing\"\n\n\t\"example.com/m/c\"\n)\n\nfunc TestD(t *testing.T) { _ = c.C() }\n",
	"e/e.go":                 "package e\n\nfunc E() int { return 5 }\n",
	"e/e_test.go":            "package e\n\nimport (\n\t\"testing\"\n\n\t\"example.com/m/b\"\n)\n\nfunc TestE(t *testing.T) { _ = b.B() }\n",
	"f/f.go":                 "package f\n\nfunc F() int { return 6 }\n",
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// newRepo writes the fixture module into a fresh git repository with one
// commit on main and returns its root. GOWORK is disabled so the repo's
// own workspace, if any, cannot leak into the fixture's go list.
func newRepo(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range fixture {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "")
	git(t, root, "init", "-q", "-b", "main")
	git(t, root, "add", ".")
	git(t, root, "commit", "-q", "-m", "base")
	return root
}

func run(t *testing.T, o options) []string {
	t.Helper()
	if o.format == "" {
		o.format = "import"
	}
	if o.stderr == nil {
		o.stderr = &bytes.Buffer{}
	}
	got, err := compute(o)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	for i, p := range got {
		got[i] = strings.TrimPrefix(p, "example.com/m/")
	}
	return got
}

func want(t *testing.T, got []string, want ...string) {
	t.Helper()
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("affected = %v, want %v", got, want)
	}
}

func TestFilesMapToImportClosure(t *testing.T) {
	root := newRepo(t)
	cases := []struct {
		name  string
		files []string
		want  []string
	}{
		{"leaf package", []string{"c/c.go"}, []string{"c", "d"}},
		{"importer chain", []string{"a/a.go"}, []string{"a", "b", "e"}},
		{"embedded asset below the package", []string{"a/assets/data.txt"}, []string{"a", "b", "e"}},
		{"testdata below the package", []string{"b/testdata/fixture.txt"}, []string{"b", "e"}},
		{"external test import", []string{"d/d_test.go"}, []string{"d"}},
		{"isolated package", []string{"f/f.go"}, []string{"f"}},
		{"file under no package", []string{"README.md", "scripts/run.sh"}, nil},
		{"several files union", []string{"c/c.go", "f/f.go"}, []string{"c", "d", "f"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want(t, run(t, options{root: root, files: tc.files}), tc.want...)
		})
	}
}

func TestModuleFilesWidenToEverything(t *testing.T) {
	root := newRepo(t)
	all := []string{"a", "b", "c", "d", "e", "f"}
	for _, f := range []string{"go.mod", "go.sum", "cmd/affected/main.go"} {
		t.Run(f, func(t *testing.T) {
			var errb bytes.Buffer
			want(t, run(t, options{root: root, files: []string{f}, stderr: &errb}), all...)
			if !strings.Contains(errb.String(), "testing everything") {
				t.Fatalf("stderr should say why: %q", errb.String())
			}
		})
	}
	t.Run("caller trigger", func(t *testing.T) {
		o := options{root: root, files: []string{"scripts/run.sh"}, allIfChanged: []string{"scripts/"}}
		want(t, run(t, o), all...)
	})
	t.Run("caller trigger is a prefix, not a substring", func(t *testing.T) {
		o := options{root: root, files: []string{"b/testdata/fixture.txt"}, allIfChanged: []string{"testdata/"}}
		want(t, run(t, o), "b", "e")
	})
	t.Run("-all", func(t *testing.T) {
		want(t, run(t, options{root: root, files: []string{"README.md"}, all: true}), all...)
	})
}

func TestExcludeAndDirFormat(t *testing.T) {
	root := newRepo(t)
	o := options{root: root, files: []string{"a/a.go"}, exclude: regexp.MustCompile(`/e$`)}
	want(t, run(t, o), "a", "b")

	o = options{root: root, files: []string{"c/c.go"}, format: "dir"}
	want(t, run(t, o), "./c", "./d")

	o = options{root: root, files: []string{"a/a.go"}, match: regexp.MustCompile(`/(a|e)$`)}
	want(t, run(t, o), "a", "e")
}

// The module root is a package too; -format dir spells it "." the way
// scripts/red-tests.sh does, never "./.".
func TestDirFormatSpellsRootAsDot(t *testing.T) {
	root := newRepo(t)
	if err := os.WriteFile(filepath.Join(root, "m.go"), []byte("package m\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	want(t, run(t, options{root: root, files: []string{"m.go"}, format: "dir"}), ".")
}

// The git path: modified, staged, untracked and deleted files all count,
// and the base is the merge base with main, not main's tip.
func TestGitChangeSet(t *testing.T) {
	root := newRepo(t)
	git(t, root, "checkout", "-q", "-b", "feature")

	t.Run("clean tree on the base is empty", func(t *testing.T) {
		want(t, run(t, options{root: root, base: "main"}))
	})

	write := func(rel, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("c/c.go", "package c\n\nfunc C() int { return 30 }\n") // unstaged
	write("f/new.go", "package f\n")                             // untracked
	if err := os.Remove(filepath.Join(root, "a", "assets", "data.txt")); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "a") // staged deletion (a's embed now fails to compile; -e keeps it listed)
	want(t, run(t, options{root: root, base: "main"}), "a", "b", "c", "d", "e", "f")

	// reset --hard, not checkout: the deletion is staged, and checkout
	// would restore the working tree from that index.
	git(t, root, "reset", "-q", "--hard")
	if err := os.Remove(filepath.Join(root, "f", "new.go")); err != nil {
		t.Fatal(err)
	}

	t.Run("merge base, not the tip of main", func(t *testing.T) {
		// A commit on main after the branch point must not count.
		git(t, root, "checkout", "-q", "main")
		write("f/f.go", "package f\n\nfunc F() int { return 60 }\n")
		git(t, root, "commit", "-q", "-am", "main moves")
		git(t, root, "checkout", "-q", "feature")
		write("d/d.go", "package d\n\nfunc D() int { return 40 }\n")
		git(t, root, "commit", "-q", "-am", "feature work")
		want(t, run(t, options{root: root, base: "main"}), "d")
		// -diff-base compares with main's tip and so sees f too.
		want(t, run(t, options{root: root, base: "main", diffBase: true}), "d", "f")
	})

	t.Run("unresolvable base tests everything", func(t *testing.T) {
		var errb bytes.Buffer
		got := run(t, options{root: root, base: "no-such-ref", stderr: &errb})
		want(t, got, "a", "b", "c", "d", "e", "f")
		if !strings.Contains(errb.String(), "testing everything") {
			t.Fatalf("stderr should explain the widening: %q", errb.String())
		}
	})
}

func TestDefaultBaseFallsBackToMain(t *testing.T) {
	root := newRepo(t)
	git(t, root, "checkout", "-q", "-b", "feature")
	if err := os.WriteFile(filepath.Join(root, "c", "c.go"), []byte("package c\n\nfunc C() int { return 7 }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// No origin/main in this repository, so the default resolves to main.
	want(t, run(t, options{root: root}), "c", "d")
}
