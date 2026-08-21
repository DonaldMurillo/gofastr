package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// classify is the part of this tool that was wrong first: the original read
// every non-zero `go test` exit as "the mutant was caught", so a bad package
// pattern made the tool report a package as fully covered without running a
// single test. These cases are real `go test` output shapes.
func TestClassifyDistinguishesFailureFromNotRunning(t *testing.T) {
	exit := errors.New("exit status 1")

	cases := []struct {
		name   string
		err    error
		output string
		want   status
	}{
		{
			name:   "tests passed with the guard broken",
			err:    nil,
			output: "ok  \tgithub.com/x/y\t0.21s\n",
			want:   survivedStatus,
		},
		{
			name:   "a test failed",
			err:    exit,
			output: "--- FAIL: TestThing (0.00s)\n    x_test.go:12: boom\nFAIL\ngithub.com/x/y\t0.2s\n",
			want:   caughtStatus,
		},
		{
			name:   "package-level FAIL line only",
			err:    exit,
			output: "FAIL\tgithub.com/x/y\t0.21s\n",
			want:   caughtStatus,
		},
		{
			name:   "the mutant made the code panic",
			err:    exit,
			output: "panic: runtime error: index out of range [3] with length 2\n\ngoroutine 1 [running]:\n",
			want:   caughtStatus,
		},
		{
			name:   "the mutant did not compile",
			err:    exit,
			output: "# github.com/x/y\n./a.go:9:2: undefined: thing\nFAIL\tgithub.com/x/y [build failed]\n",
			want:   brokenStatus,
		},
		{
			name:   "the mutation orphaned a local",
			err:    exit,
			output: "# github.com/x/y\n./a.go:9:6: declared and not used: cond\n",
			want:   brokenStatus,
		},
	}
	for _, c := range cases {
		got, err := classify(c.err, c.output)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: classify = %v, want %v", c.name, got, c.want)
		}
	}
}

// The failure that started this: `go test ./sqlite/` run from inside ./sqlite
// matches no package, exits non-zero, and reports neither a test failure nor a
// build error. Scoring it either way invents a result.
func TestClassifyRefusesToScoreAHarnessProblem(t *testing.T) {
	for _, output := range []string{
		"go: cannot find main module\n",
		"stat ./sqlite/: directory not found\n",
		"go: warning: \"./nope/...\" matched no packages\n",
		"",
	} {
		if _, err := classify(errors.New("exit status 1"), output); err == nil {
			t.Errorf("classify scored a harness problem instead of refusing: %q", output)
		} else if !strings.Contains(err.Error(), "harness problem") {
			t.Errorf("error should name this as a harness problem, got: %v", err)
		}
	}
}

// A -run pattern matching nothing makes go test exit ZERO. Read naively, every
// mutant then reads as survived and the whole run is a false alarm.
func TestClassifyRefusesAZeroExitThatRanNoTests(t *testing.T) {
	out := "testing: warning: no tests to run\nPASS\nok  \tgithub.com/x/y\t0.10s [no tests to run]\n"
	if _, err := classify(nil, out); err == nil {
		t.Error("classify accepted a run that executed no tests — every mutant would read as survived")
	}
}

// writeVerified guards the three ways a mutation run can silently prove
// nothing. Each check is invisible when it fails — the tests simply pass — so
// each needs its own case.
func TestWriteVerifiedCatchesEveryWayAMutationCanBeAFiction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	original := []byte("package p\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("identical source is refused", func(t *testing.T) {
		err := writeVerified(path, original, original)
		if err == nil {
			t.Fatal("accepted a mutation that changed nothing — it would read as an uncaught guard")
		}
		if !strings.Contains(err.Error(), "identical") {
			t.Errorf("error should name the cause, got: %v", err)
		}
	})

	t.Run("a real mutation is written and confirmed", func(t *testing.T) {
		mutated := []byte("package p // changed\n")
		if err := writeVerified(path, original, mutated); err != nil {
			t.Fatalf("rejected a valid mutation: %v", err)
		}
		back, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(back) != string(mutated) {
			t.Errorf("file on disk = %q, want the mutant", back)
		}
	})

	t.Run("an unwritable path is refused, not scored", func(t *testing.T) {
		// A mutation that cannot be written must be an error. Treating the
		// failure as "no test caught it" would report the guard as uncovered
		// while the source was never touched.
		err := writeVerified(filepath.Join(dir, "no-such-dir", "a.go"), original, []byte("package q\n"))
		if err == nil {
			t.Fatal("accepted a write that could not have happened")
		}
		if !strings.Contains(err.Error(), "write mutant") {
			t.Errorf("error should name the write failure, got: %v", err)
		}
	})
}

// restore must put the file back byte for byte — the tool writes mutants into
// the real source tree, so a restore that drifts corrupts the repository.
func TestRestoreReturnsTheFileExactly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	original := []byte("package p\n\nfunc f() bool { return true }\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeVerified(path, original, []byte("package p // mutant\n")); err != nil {
		t.Fatal(err)
	}
	restore(path, original)
	back, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != string(original) {
		t.Errorf("restore left the file changed:\ngot  %q\nwant %q", back, original)
	}
}

// The third verdict bug in this tool's history, and the subtlest: the BROKEN
// arm scanned the whole `go test` stream for compiler-ish words, so a package
// whose tests legitimately LOG the words "syntax error" — every package that
// exercises a database error path — had its genuinely caught mutants reported
// as BROKEN. It only showed up on full-package runs; -run narrowing hid it,
// which is how it survived a self-audit.
func TestClassifyIsNotFooledByApplicationLogOutput(t *testing.T) {
	exit := errors.New("exit status 1")

	cases := []struct {
		name   string
		output string
		want   status
	}{
		{
			name: "a test failure whose logs mention a syntax error",
			output: "crud: internal error: insert: SQL logic error: near \"(\": syntax error (1)\n" +
				"--- FAIL: TestEagerScopeFilters (0.01s)\n    gate_test.go:367: got filters\n" +
				"FAIL\ngithub.com/x/crud\t2.2s\n",
			want: caughtStatus,
		},
		{
			name:   "a test failure whose logs mention undefined and cannot use",
			output: "app: undefined: column\napp: cannot use value\n--- FAIL: TestThing (0.00s)\nFAIL\tgithub.com/x/y\t0.2s\n",
			want:   caughtStatus,
		},
		{
			name:   "a genuine compile error",
			output: "# github.com/x/y\n./a.go:9:2: undefined: thing\nFAIL\tgithub.com/x/y [build failed]\n",
			want:   brokenStatus,
		},
		{
			name:   "a compile error reported without the build-failed marker",
			output: "# github.com/x/y\n./a.go:12:6: declared and not used: cond\n",
			want:   brokenStatus,
		},
		{
			name:   "a package-level FAIL with no per-test line still counts as caught",
			output: "FAIL\tgithub.com/x/y\t0.21s\n",
			want:   caughtStatus,
		},
	}
	for _, c := range cases {
		got, err := classify(exit, c.output)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: classify = %v, want %v", c.name, got, c.want)
		}
	}
}

// isBuildFailure must key on structure, not vocabulary: a `# package` header
// followed by a compiler diagnostic, or go's own marker.
func TestIsBuildFailureKeysOnStructure(t *testing.T) {
	build := []string{
		"# github.com/x/y\n./a.go:9:2: undefined: thing\n",
		"FAIL\tgithub.com/x/y [build failed]\n",
		"FAIL\tgithub.com/x/y [setup failed]\n",
	}
	for _, out := range build {
		if !isBuildFailure(out) {
			t.Errorf("missed a build failure:\n%s", out)
		}
	}
	notBuild := []string{
		"app log: near \"(\": syntax error (1)\n--- FAIL: T (0.0s)\n",
		"# a heading that is not a package error\nsome prose\n",
		"ok  \tgithub.com/x/y\t0.2s\n",
		"--- FAIL: TestX\n    x_test.go:3: undefined: in a message\n",
	}
	for _, out := range notBuild {
		if isBuildFailure(out) {
			t.Errorf("called this a build failure:\n%s", out)
		}
	}
}

// The signal handler restores every mutated file and exits. Marking the run as
// stopping BEFORE restoring is what stops a write already in flight from
// landing afterwards — the mutex made the two data-race-free but did not order
// them against the filesystem, so a restore could be overwritten by the write
// it was racing and the process would exit with the mutant on disk.
func TestRestoreSetRefusesNewMutantsOnceStopping(t *testing.T) {
	rs := newRestoreSet()
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	original := []byte("package p\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := rs.addAndWrite(path, original, []byte("package p // mutated\n")); err != nil {
		t.Fatalf("add before stopping should succeed: %v", err)
	}
	rs.stop()
	if !rs.stopped() {
		t.Error("stop() did not mark the set as stopping")
	}
	if err := rs.addAndWrite(filepath.Join(dir, "b.go"), original, []byte("package p // mutated\n")); err == nil {
		t.Error("a mutant was registered after the run began stopping — it could land after the restore")
	}

	// runAll must put back everything registered before the stop.
	if err := os.WriteFile(path, []byte("package p // mutant\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rs.runAll()
	back, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != string(original) {
		t.Errorf("runAll left the file as %q, want the original", back)
	}
}

// restoreSet is touched from the signal goroutine and the main loop, so its
// invariants have to hold under -race.
func TestRestoreSetIsSafeUnderConcurrentUse(t *testing.T) {
	rs := newRestoreSet()
	dir := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			p := filepath.Join(dir, fmt.Sprintf("f%d.go", n))
			_ = os.WriteFile(p, []byte("package p\n"), 0o644)
			for j := 0; j < 50; j++ {
				if err := rs.addAndWrite(p, []byte("package p\n"), []byte("package p // m\n")); err == nil {
					rs.remove(p)
				}
			}
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 50; j++ {
			rs.runAll()
			_ = rs.stopped()
		}
	}()
	wg.Wait()
}

// A wildcard pattern makes `go list` print one record per package. The old
// parser took line 0 as the directory and every later line as a file name, so
// a second package's directory path became a "file" and the run died with
// `read <dir>/<abs path>` — an error that names neither the wildcard nor what
// it broke. This tool rewrites source files in place, so a mis-parsed file set
// is the one failure it must refuse before doing any work.
func TestPackageFilesRejectsAMultiPackagePattern(t *testing.T) {
	_, _, err := packageFiles("github.com/DonaldMurillo/gofastr/cmd/...")
	if err == nil {
		t.Fatal("a wildcard matching several packages must be refused, not parsed")
	}
	for _, want := range []string{"matches", "one package at a time"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must say why the pattern is refused, missing %q: %v", want, err)
		}
	}
}

// The single-package path still resolves, and the directory it returns is a
// directory rather than the first source file.
func TestPackageFilesResolvesOnePackage(t *testing.T) {
	dir, files, err := packageFiles("github.com/DonaldMurillo/gofastr/cmd/mutate")
	if err != nil {
		t.Fatal(err)
	}
	if info, statErr := os.Stat(dir); statErr != nil || !info.IsDir() {
		t.Fatalf("dir = %q, want an existing directory (err %v)", dir, statErr)
	}
	if len(files) == 0 {
		t.Fatal("no Go files returned")
	}
	for _, f := range files {
		if filepath.IsAbs(f) {
			t.Errorf("file %q is absolute — that is a package directory read as a file name", f)
		}
		if !strings.HasSuffix(f, ".go") {
			t.Errorf("file %q is not a Go file", f)
		}
	}
}
