package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
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
// own workspace, if any, cannot leak into the fixture's go list, and
// GOFASTR_TEST_ALL is cleared so a test-everything run (main's CI) does not
// widen what a subprocess of the built binary selects.
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
	t.Setenv("GOFASTR_TEST_ALL", "")
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

// A test that runs one of the module's commands (go run <module>/cmd/x)
// depends on that command's source and everything it imports, with no
// import edge for go list to report.
func TestGoRunOfACommandIsATestEdge(t *testing.T) {
	root := newRepo(t)
	extra := map[string]string{
		"g/main.go":   "package main\n\nimport \"example.com/m/c\"\n\nfunc main() { _ = c.C() }\n",
		"h/h.go":      "package h\n",
		"h/h_test.go": "package h\n\nimport (\n\t\"os/exec\"\n\t\"testing\"\n)\n\nfunc TestH(t *testing.T) { _ = exec.Command(\"go\", \"run\", \"example.com/m/g\") }\n",
	}
	for name, body := range extra {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want(t, run(t, options{root: root, files: []string{"g/main.go"}}), "g", "h")
	want(t, run(t, options{root: root, files: []string{"c/c.go"}}), "c", "d", "g", "h")
	// A non-main package named in a string is not a run: e names b only
	// through its import, and a library path in a string adds nothing.
	want(t, run(t, options{root: root, files: []string{"f/f.go"}}), "f")
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

// envValue is the last value of key in env, the one exec uses.
func envValue(env []string, key string) (string, bool) {
	val, found := "", false
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			val, found = v, true
		}
	}
	return val, found
}

func TestCoverageDirIsOutsideTheModule(t *testing.T) {
	t.Setenv(coverageDirEnv, "")
	root := t.TempDir()
	dir, ok := envValue(commandEnv(root), coverageDirEnv)
	if !ok || dir == "" {
		t.Fatalf("%s not set for the wrapped command", coverageDirEnv)
	}
	if rel, err := filepath.Rel(root, dir); err == nil && !strings.HasPrefix(rel, "..") {
		t.Fatalf("coverage dir %q is inside the module %q", dir, root)
	}
	// The value is a test cache input: it must not move between runs.
	if again, _ := envValue(commandEnv(root), coverageDirEnv); again != dir {
		t.Fatalf("coverage dir moved between runs: %q then %q", dir, again)
	}
	if other, _ := envValue(commandEnv(t.TempDir()), coverageDirEnv); other == dir {
		t.Fatalf("two checkouts share coverage dir %q", dir)
	}
}

func TestExplicitCoverageDirWins(t *testing.T) {
	t.Setenv(coverageDirEnv, "/elsewhere")
	if got, _ := envValue(commandEnv(t.TempDir()), coverageDirEnv); got != "/elsewhere" {
		t.Fatalf("explicit %s replaced: got %q", coverageDirEnv, got)
	}
}

// commandEnv is only half of it: the wrapped command must see the value.
func TestWrappedCommandGetsCoverageDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("prints the variable through sh")
	}
	t.Setenv(coverageDirEnv, "")
	bin := buildAffected(t)
	root := newRepo(t)
	if err := os.WriteFile(filepath.Join(root, "c", "c.go"), []byte("package c\n\nfunc C() int { return 7 }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "-base", "main", "--", "sh", "-c", `printf %s "$`+coverageDirEnv+`"`)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("affected -- sh: %v", err)
	}
	if want, _ := envValue(commandEnv(root), coverageDirEnv); string(out) != want {
		t.Fatalf("wrapped command saw %s=%q, want %q", coverageDirEnv, out, want)
	}
}

func buildAffected(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "affected")
	// -buildvcs=false: a stamped build runs git in this repo and moves
	// .git's mtime (see commandEnv).
	if out, err := exec.Command("go", "build", "-buildvcs=false", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build affected: %v\n%s", err, out)
	}
	return bin
}

// The hooks export what -coverage-dir prints before the sweep, so the
// sweep and the gofastr verify after it agree on where the manifests are.
func TestCoverageDirFlagPrintsTheWrappedValue(t *testing.T) {
	t.Setenv(coverageDirEnv, "")
	bin := buildAffected(t)
	root := newRepo(t)
	cmd := exec.Command(bin, "-coverage-dir")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("affected -coverage-dir: %v", err)
	}
	want, _ := envValue(commandEnv(root), coverageDirEnv)
	if got := strings.TrimSpace(string(out)); got != want || want == "" {
		t.Fatalf("-coverage-dir printed %q, want the wrapped value %q", got, want)
	}
}

// Without a user cache the manifests fall back into the module, where they
// break the test cache, so the fallback has to be visible.
func TestCoverageDirWarnsWithoutAUserCache(t *testing.T) {
	t.Setenv(coverageDirEnv, "")
	bin := buildAffected(t)
	cmd := exec.Command(bin, "-coverage-dir")
	cmd.Dir = newRepo(t)
	cmd.Env = append(os.Environ(), "HOME=", "XDG_CACHE_HOME=", "LocalAppData=")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("affected -coverage-dir: %v\n%s", err, stderr.String())
	}
	if got := strings.TrimSpace(string(out)); got != "" {
		t.Fatalf("-coverage-dir printed %q, want the module-root fallback \"\"", got)
	}
	if !strings.Contains(stderr.String(), "no user cache directory") {
		t.Fatalf("no warning on stderr: %q", stderr.String())
	}
}

func TestCommandEnvTurnsOffVCSStamping(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", "-buildvcs=false"},
		{"-mod=mod", "-mod=mod -buildvcs=false"},
		{"-buildvcs=true", "-buildvcs=true"},
		{"--buildvcs=auto -mod=mod", "--buildvcs=auto -mod=mod"},
	} {
		t.Setenv("GOFLAGS", tc.in)
		for _, cov := range []string{"", "/elsewhere"} {
			t.Setenv(coverageDirEnv, cov)
			if got, _ := envValue(commandEnv(t.TempDir()), "GOFLAGS"); got != tc.want {
				t.Errorf("GOFLAGS=%q, %s=%q: wrapped GOFLAGS=%q, want %q", tc.in, coverageDirEnv, cov, got, tc.want)
			}
		}
	}
}

// A stamped go build runs git, which moves .git's mtime; every test that
// lists the repo root then misses the cache. The wrapped command's builds
// must leave .git alone.
func TestWrappedBuildLeavesGitDirAlone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("runs the build through sh")
	}
	t.Setenv(coverageDirEnv, "")
	bin := buildAffected(t)
	root := newRepo(t)
	if err := os.MkdirAll(filepath.Join(root, "app"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app", "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", ".")
	git(t, root, "commit", "-q", "-m", "app")
	gitDir := filepath.Join(root, ".git")
	old := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	moved := func() bool {
		t.Helper()
		fi, err := os.Stat(gitDir)
		if err != nil {
			t.Fatal(err)
		}
		return !fi.ModTime().Equal(old)
	}
	build := func(cmd *exec.Cmd) {
		t.Helper()
		// A fresh mtime on a tracked file gives git's status an index
		// refresh to write, which is when it takes index.lock.
		now := time.Now()
		if err := os.Chtimes(filepath.Join(root, "app", "main.go"), now, now); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(gitDir, old, old); err != nil {
			t.Fatal(err)
		}
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", cmd.Args, err, out)
		}
	}
	out := filepath.Join(t.TempDir(), "app")

	// Control: a stamped build does move it, so the check below can fail.
	build(exec.Command("go", "build", "-o", out, "./app"))
	if !moved() {
		t.Skip("this git leaves .git alone on a stamped build; nothing to prove")
	}
	build(exec.Command(bin, "-all", "--", "sh", "-c", `go build -o "$0" ./app`, out))
	if moved() {
		t.Fatal("a go build under affected moved .git's mtime")
	}
}

// CI's replay gate greps this line to tell an empty selection, which runs
// no test, from a second run that tested nothing.
func TestEmptySelectionSkipsTheCommand(t *testing.T) {
	bin := buildAffected(t)
	cmd := exec.Command(bin, "-base", "HEAD", "--", "echo", "ran")
	cmd.Dir = newRepo(t)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("affected: %v\n%s", err, stderr.String())
	}
	if len(out) != 0 {
		t.Fatalf("the command ran on an empty selection: %q", out)
	}
	if !strings.HasPrefix(stderr.String(), "affected: no packages affected") {
		t.Fatalf("stderr %q does not start with the line CI's replay gate greps", stderr.String())
	}
}
