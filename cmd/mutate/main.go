// Command mutate breaks the conditional guards in a package one at a time and
// reports which ones no test notices.
//
// A guard whose "never fires" mutant leaves the suite green is a guard nothing
// proves refuses anything. A guard whose "always fires" mutant leaves the
// suite green is a guard nothing proves permits anything — the shape that lets
// a too-tight gate ship. Both are reported as SURVIVED.
//
//	go run ./cmd/mutate ./sqlite/                       # whole package
//	go run ./cmd/mutate -file engine.go ./sqlite/       # one file
//	go run ./cmd/mutate -line 2520 -file engine.go ./sqlite/
//
// Before any mutation the unmutated suite is run once: an already-red package
// would otherwise report every mutant as caught and exit 0 claiming perfect
// coverage. A mutant that fails to compile is reported as BROKEN, never as
// caught — a build failure prints its own `FAIL` line, so that classification
// has to be tested for explicitly rather than assumed. A mutation that does
// not change the file, or does not reach disk, is a hard error rather than any
// verdict at all.
//
// It writes into real source files, so it holds a per-package lock (concurrent
// runs otherwise read each other's mutants as their baseline and can restore
// one as the original), bounds every test run with -timeout (an infinite-loop
// mutant would otherwise hang until killed), and restores every file on
// SIGINT/SIGTERM (a deferred restore does not run on a signal, and Ctrl-C on a
// slow run is the ordinary way this ends).
//
// This is a developer tool and a targeted CI gate, not a blanket one: every
// mutant costs a full test run of the package.
//
// Run on itself, this package reports survivors in main()'s own flag parsing,
// filtering, and output formatting. That is accurate and deliberate: those
// branches are exercised by using the tool, and covering them would mean
// spawning the binary from a test for no real gain. The logic that can lie —
// the verdict classification, the write-and-verify step, restore, and the
// mutation core in guardmut — is tested, and guardmut is mutation-clean. A
// clean run of `make mutate PKG=./cmd/mutate/guardmut/` is the claim being
// made; it is not a claim about the CLI wiring.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/mutate/guardmut"
)

func main() {
	// A thin wrapper so every defer below actually runs. os.Exit skips defers,
	// and the lock is released by one: a refused baseline used to exit while
	// still holding the lock, which then blocked the next run over that
	// package with a message about a run that was no longer active.
	os.Exit(run())
}

func run() int {
	var (
		fileFlag = flag.String("file", "", "only mutate guards in this file (base name or suffix match)")
		lineFlag = flag.Int("line", 0, "only mutate the guard on this line")
		runFlag  = flag.String("run", "", "pass -run to go test (narrows the suite per mutant)")
		errFlag  = flag.Bool("err", false, "also mutate `if err != nil` guards (noisy: error paths are rarely asserted)")
		timeout  = flag.Duration("timeout", 10*time.Minute, "per-mutant test timeout")
		verbose  = flag.Bool("v", false, "print every mutant, not just survivors")
	)
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: mutate [flags] <package>")
		flag.PrintDefaults()
		return 2
	}
	pkg := flag.Arg(0)

	dir, files, err := packageFiles(pkg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	// This tool writes mutants into real source files, so two things must hold
	// no matter how the process ends.
	//
	// A lock, because two concurrent runs over one package interleave: the
	// second reads its "original" while the first's mutant is on disk, then
	// restores THAT as the original — a reviewer reproduced it and the true
	// source survived only by finish order.
	unlock, err := lockPackage(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer unlock()

	// And a signal handler, because `defer restore` does not run on SIGINT —
	// and Ctrl-C on a slow run is the ordinary way this ends. Both signals
	// were shown to leave a mutant in the tree.
	restoreAll := newRestoreSet()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-stop
		// Order matters: mark the run as stopping FIRST, so a write already
		// past pending.add refuses rather than landing after the restore.
		// The mutex made this data-race-free but did not order the two
		// against the FILESYSTEM — a restore could be immediately overwritten
		// by the write it was racing, leaving the mutant on disk.
		restoreAll.stop()
		restoreAll.runAll()
		unlock()
		fmt.Fprintf(os.Stderr, "\ninterrupted (%s): source files restored\n", sig)
		// os.Exit is correct here: the cleanup the deferred calls would have
		// done has just been performed explicitly, and there is no stack to
		// return through from a signal goroutine.
		os.Exit(130)
	}()
	root, err := moduleDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	// Establish that the suite is GREEN before breaking anything. Without
	// this, an already-red package reports every mutant as caught and the run
	// exits 0 claiming perfect coverage — the tests were failing the whole
	// time and the guards were never exercised. A reviewer produced exactly
	// that with a package whose test file did not compile.
	if err := checkBaseline(pkg, *runFlag, root, *timeout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	var survived, broken, caught int
	for _, name := range files {
		if *fileFlag != "" && !strings.HasSuffix(name, *fileFlag) && filepath.Base(name) != *fileFlag {
			continue
		}
		path := filepath.Join(dir, name)
		original, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read %s: %v\n", path, err)
			return 1
		}
		guards, err := guardmut.Find(name, original, guardmut.Options{SkipErrNil: !*errFlag})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		for _, g := range guards {
			if *lineFlag != 0 && g.Line != *lineFlag {
				continue
			}
			status, err := runMutant(path, original, g, pkg, *runFlag, root, *timeout, restoreAll)
			if err != nil {
				restore(path, original)
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			switch status {
			case caughtStatus:
				caught++
				if *verbose {
					fmt.Printf("  caught    %s\n", g)
				}
			case brokenStatus:
				broken++
				fmt.Printf("  BROKEN    %s — mutant did not compile; nothing was proven\n", g)
			case survivedStatus:
				survived++
				fmt.Printf("  SURVIVED  %s\n", g)
			}
		}
		restore(path, original)
	}

	fmt.Printf("\n%d caught, %d survived, %d broken\n", caught, survived, broken)
	if survived > 0 || broken > 0 {
		fmt.Println("\nA SURVIVED guard is one no test distinguishes. Either add the case that\n" +
			"separates it from the guards around it, or delete the guard as redundant.")
		return 1
	}
	return 0
}

type status int

const (
	caughtStatus status = iota
	survivedStatus
	brokenStatus
)

// runMutant writes one mutant, runs the package tests, and restores the file.
func runMutant(path string, original []byte, g guardmut.Guard, pkg, run, moduleRoot string, timeout time.Duration, pending *restoreSet) (status, error) {
	mutated, err := guardmut.Apply(original, g)
	if err != nil {
		return 0, err
	}
	if err := pending.add(path, original); err != nil {
		return 0, err
	}
	if err := writeVerified(path, original, mutated); err != nil {
		pending.remove(path)
		return 0, fmt.Errorf("%s: %w", g, err)
	}
	defer func() {
		restore(path, original)
		pending.remove(path)
	}()

	args := []string{"test", pkg, "-count=1"}
	if run != "" {
		args = append(args, "-run", run)
	}
	// Run from the module root, NOT the package directory: `go test ./sqlite/`
	// from inside ./sqlite resolves to nothing, `go test` exits non-zero, and a
	// naive reading of that exit code calls every mutant "caught" — the tool
	// reporting perfect coverage precisely because it never ran a test. This
	// happened on the first draft.
	// -timeout was accepted and never applied until a reviewer walked the
	// consequence: a mutant that turns a loop-exit condition into an infinite
	// loop hangs the run, the user kills it, and the kill is what leaves a
	// mutant in the source tree. Bound both the child and go test itself.
	args = append(args, fmt.Sprintf("-timeout=%s", timeout))
	ctx, cancel := context.WithTimeout(context.Background(), timeout+30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = moduleRoot
	out, testErr := cmd.CombinedOutput()
	if ctx.Err() != nil {
		// A hung mutant is a result, not a harness fault: the guard mattered
		// enough that removing it stopped the tests terminating.
		return caughtStatus, nil
	}
	st, err := classify(testErr, string(out))
	if err != nil {
		return 0, fmt.Errorf("%s: %w", g, err)
	}
	return st, nil
}

// writeVerified writes a mutant and proves it landed, changed, and differs
// from the original before any test is allowed to run.
//
// All three checks exist because their absence is invisible. A mutation that
// produces identical source, or that never reaches disk, yields a green test
// run — which reads exactly like a guard no test covers. Two hand-run audits
// during the review that motivated this tool nearly recorded a guard as
// covered for precisely that reason.
func writeVerified(path string, original, mutated []byte) error {
	if string(mutated) == string(original) {
		return errors.New("mutation produced identical source; it would read as an uncaught guard")
	}
	if err := os.WriteFile(path, mutated, 0o644); err != nil {
		return fmt.Errorf("write mutant: %w", err)
	}
	back, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("re-read mutant: %w", err)
	}
	if string(back) != string(mutated) {
		return errors.New("mutant did not land on disk; the run would prove nothing")
	}
	return nil
}

// checkBaseline runs the unmutated suite once. Everything after it is only
// meaningful against a green starting point.
func checkBaseline(pkg, run, moduleRoot string, timeout time.Duration) error {
	args := []string{"test", pkg, "-count=1", fmt.Sprintf("-timeout=%s", timeout)}
	if run != "" {
		args = append(args, "-run", run)
	}
	cmd := exec.Command("go", args...)
	cmd.Dir = moduleRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Including the timeout case: a suite that cannot finish inside
		// -timeout must refuse here, not proceed. Without this, every mutant
		// dies at the deadline, go test's `panic: test timed out` scores as
		// caught, and the run prints perfect coverage and exits 0 — a report
		// manufactured entirely by a flag that was added for safety.
		return fmt.Errorf("the suite is not green under -timeout=%s before any mutation, so no verdict below would mean anything:\n%s",
			timeout, truncate(string(out)))
	}
	// A run that executed no tests is the other way to get a meaningless
	// green: every mutant would then be reported as survived.
	if strings.Contains(string(out), "no tests to run") || strings.Contains(string(out), "[no test files]") {
		return fmt.Errorf("the baseline run executed no tests; every guard would be reported as uncovered:\n%s", truncate(string(out)))
	}
	return nil
}

// compilerDiagnostic matches a Go compiler line: `path/file.go:12:34: message`.
var compilerDiagnostic = regexp.MustCompile(`(?m)^[^\s]+\.go:\d+:\d+: `)

// isBuildFailure reports whether output shows the package failing to compile,
// as opposed to failing its tests.
//
// Structural, not lexical. An earlier version searched the whole stream for
// "syntax error", "undefined:", and friends — words that appear routinely in
// the log output of tests that exercise error paths, so a package whose tests
// print a database's own `syntax error` message had every caught mutant
// reported as BROKEN.
func isBuildFailure(output string) bool {
	if strings.Contains(output, "[build failed]") || strings.Contains(output, "[setup failed]") {
		return true
	}
	// `# <package>` immediately followed by a compiler diagnostic. Test logs
	// are prefixed by the testing package and never take this shape.
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "# ") {
			continue
		}
		for _, next := range lines[i+1:] {
			trimmed := strings.TrimSpace(next)
			if trimmed == "" {
				continue
			}
			return compilerDiagnostic.MatchString(trimmed)
		}
	}
	return false
}

// classify turns one `go test` run into a verdict.
//
// A zero exit means the tests passed with the guard broken: SURVIVED. A
// non-zero exit has three causes that must not be conflated — a real test
// failure (the mutant was caught), a compile error (nothing ran), and a
// tooling problem (also nothing ran) — and only the first is evidence. The
// first draft of this tool read every non-zero exit as "caught" and reported
// a package as fully covered while running no tests at all, because the
// package pattern was wrong for its working directory.
//
// A non-zero exit that reports no test failure and no build error is NOT
// scored. Guessing there is how a harness invents coverage.
func classify(exitErr error, output string) (status, error) {
	if exitErr == nil {
		// One more way to get a green run without testing anything: -run that
		// matches no test. go test prints "no tests to run" and exits 0, which
		// would score every mutant as survived.
		if strings.Contains(output, "warning: no tests to run") ||
			strings.Contains(output, "testing: warning: no tests to run") {
			return 0, fmt.Errorf("go test matched no tests; every mutant would read as survived:\n%s", truncate(output))
		}
		return survivedStatus, nil
	}
	// A reported test failure settles it. This is checked FIRST because the
	// alternative — scanning the whole stream for compiler-ish words — reads
	// an application's own log output as a build error. Packages under test
	// here log strings like `SQL logic error: near "(": syntax error`, which
	// made genuinely caught mutants report BROKEN on every full-package run
	// while the same mutants read correctly under -run narrowing. Two verdict
	// bugs before this one came from ordering these arms by guesswork.
	if strings.Contains(output, "--- FAIL") {
		return caughtStatus, nil
	}
	// No test failure line: a build error is the other reason for a non-zero
	// exit. Detect it structurally — go prints `FAIL\tpkg [build failed]`, or
	// a `# pkg` header followed by `file.go:line:col: message` — rather than
	// by hunting for words that legitimately appear in test output.
	if isBuildFailure(output) {
		return brokenStatus, nil
	}
	switch {
	case strings.Contains(output, "\nFAIL\t"),
		strings.HasPrefix(output, "FAIL\t"),
		strings.Contains(output, "panic: "):
		// A panic counts as caught: the guard was load-bearing enough that
		// removing it crashed the code under test.
		return caughtStatus, nil
	}
	return 0, fmt.Errorf("go test failed without reporting a test failure or a build error; "+
		"this is a harness problem, not a mutation result:\n%s", truncate(output))
}

// restoreSet tracks files currently holding a mutant so a signal handler can
// put them back. Guarded by a mutex because the handler runs on its own
// goroutine.
type restoreSet struct {
	mu       sync.Mutex
	files    map[string][]byte
	stopping bool
}

func newRestoreSet() *restoreSet { return &restoreSet{files: map[string][]byte{}} }

// add registers a file about to be mutated. It refuses once the run is
// stopping, which is what keeps a write from landing after the signal
// handler has already restored everything.
func (r *restoreSet) add(path string, original []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopping {
		return errors.New("run is stopping; refusing to write another mutant")
	}
	r.files[path] = original
	return nil
}

// stop marks the run as stopping. Callers already past add() are unaffected —
// the window it closes is the common one, where the signal arrives before the
// next mutant is written.
func (r *restoreSet) stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopping = true
}

// stopped reports whether the run is stopping.
func (r *restoreSet) stopped() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopping
}

func (r *restoreSet) remove(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.files, path)
}

func (r *restoreSet) runAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for path, original := range r.files {
		restore(path, original)
		delete(r.files, path)
	}
}

// lockPackage serialises runs over one package directory. Concurrent runs do
// not merely produce garbage verdicts — each reads the other's mutant as its
// baseline, and whichever finishes last writes that baseline back as the
// "original".
func lockPackage(dir string) (func(), error) {
	path := filepath.Join(dir, ".mutate.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("another mutate run holds %s.\n"+
				"If a run is active, wait — concurrent runs read each other's mutants as their baseline.\n"+
				"If none is active, the previous run was killed and MAY HAVE LEFT A MUTANT IN THE SOURCE.\n"+
				"Check `git status` / `git diff` for the package and restore it before removing the lock:\n"+
				"a fresh run would otherwise adopt that mutant as the original and write it back permanently.", path)
		}
		return nil, fmt.Errorf("create lock: %w", err)
	}
	_ = f.Close()
	return func() { _ = os.Remove(path) }, nil
}

func restore(path string, original []byte) {
	if err := os.WriteFile(path, original, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "CRITICAL: could not restore %s: %v\n", path, err)
	}
}

// moduleDir returns the module root, which is where package patterns like
// ./sqlite/ are meaningful.
func moduleDir() (string, error) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	if err != nil {
		return "", fmt.Errorf("locate module root: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func truncate(s string) string {
	const max = 600
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

// packageFiles resolves a package pattern to its directory and non-test Go
// files. Test files are excluded: mutating a test proves nothing about it.
func packageFiles(pkg string) (string, []string, error) {
	out, err := exec.Command("go", "list", "-f", "{{.Dir}}\n{{range .GoFiles}}{{.}}\n{{end}}", pkg).Output()
	if err != nil {
		return "", nil, fmt.Errorf("go list %s: %w", pkg, err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return "", nil, fmt.Errorf("no Go files in %s", pkg)
	}
	return lines[0], lines[1:], nil
}
