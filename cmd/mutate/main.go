// Command mutate breaks the conditional guards in one package, one at a time,
// both ways (never fires, always fires), and reports each mutant no test
// notices as SURVIVED and each that does not compile as BROKEN.
//
//	go run ./cmd/mutate ./sqlite/                       # whole package
//	go run ./cmd/mutate -file engine.go ./sqlite/       # one file
//	go run ./cmd/mutate -line 2520 -file engine.go ./sqlite/
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
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

	unlock, err := lockPackage(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer unlock()

	restoreAll := newRestoreSet()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-stop
		restoreAll.interrupt()
		unlock()
		fmt.Fprintf(os.Stderr, "\ninterrupted (%s): source files restored\n", sig)
		os.Exit(130)
	}()
	root, err := moduleDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

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
				fmt.Printf("  BROKEN    %s: mutant did not compile; nothing was proven\n", g)
			case survivedStatus:
				survived++
				fmt.Printf("  SURVIVED  %s\n", g)
			}
		}
		restore(path, original)
	}

	fmt.Printf("\n%d caught, %d survived, %d broken\n", caught, survived, broken)
	if (*fileFlag != "" || *lineFlag != 0) && caught+survived+broken == 0 {
		fmt.Fprintf(os.Stderr, "no guards matched -file=%q -line=%d\n", *fileFlag, *lineFlag)
		return 1
	}
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

func runMutant(path string, original []byte, g guardmut.Guard, pkg, run, moduleRoot string, timeout time.Duration, pending *restoreSet) (status, error) {
	mutated, err := guardmut.Apply(original, g)
	if err != nil {
		return 0, err
	}
	if err := pending.addAndWrite(path, original, mutated); err != nil {
		return 0, fmt.Errorf("%s: %w", g, err)
	}
	defer func() {
		restore(path, original)
		pending.remove(path)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), timeout+baselineBuildGrace)
	defer cancel()
	out, testErr := newGoTest(ctx, moduleRoot, testArgs(pkg, run, timeout)).CombinedOutput()
	if ctx.Err() != nil {
		st, err := expiredVerdict(string(out), timeout+baselineBuildGrace)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", g, err)
		}
		return st, nil
	}
	st, err := classify(testErr, string(out))
	if err != nil {
		return 0, fmt.Errorf("%s: %w", g, err)
	}
	return st, nil
}

func expiredVerdict(output string, limit time.Duration) (status, error) {
	if strings.Contains(output, "panic: test timed out") {
		return caughtStatus, nil
	}
	return 0, fmt.Errorf("go test ran past %s without the test binary timing out, so no test ran; "+
		"this is a harness problem, not a mutation result:\n%s", limit, truncate(output))
}

func writeVerified(path string, original, mutated []byte) error {
	if string(mutated) == string(original) {
		return errors.New("mutation produced identical source; it would read as an uncaught guard")
	}
	if err := os.WriteFile(path, mutated, 0o644); err != nil { //gofastr:allow(worldreadable) path is the module's own source file, read by the driver before the mutant is written (main.go runLoop); 0644 is the checkout's own mode and the file exists, so the mode never applies at create
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

// baselineBuildGrace is the head-room every go test run gets on top of
// -timeout for loading, compiling and vetting before the test binary exists.
const baselineBuildGrace = 5 * time.Minute

func testArgs(pkg, run string, timeout time.Duration) []string {
	args := []string{"test", pkg, "-count=1", fmt.Sprintf("-timeout=%s", timeout)}
	if run != "" {
		args = append(args, "-run", run)
	}
	return args
}

var newGoTest = goTest

func goTest(ctx context.Context, moduleRoot string, args []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = moduleRoot
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

func checkBaseline(pkg, run, moduleRoot string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout+baselineBuildGrace)
	defer cancel()
	out, err := newGoTest(ctx, moduleRoot, testArgs(pkg, run, timeout)).CombinedOutput()
	if ctx.Err() != nil {
		return fmt.Errorf("the baseline suite did not finish within %s (build plus -timeout=%s), so no verdict below would mean anything:\n%s",
			timeout+baselineBuildGrace, timeout, truncate(string(out)))
	}
	if err != nil {
		return fmt.Errorf("the suite is not green under -timeout=%s before any mutation, so no verdict below would mean anything:\n%s",
			timeout, truncate(string(out)))
	}
	if strings.Contains(string(out), "no tests to run") || strings.Contains(string(out), "[no test files]") {
		return fmt.Errorf("the baseline run executed no tests; every guard would be reported as uncovered:\n%s", truncate(string(out)))
	}
	return nil
}

var compilerDiagnostic = regexp.MustCompile(`(?m)^[^\s]+\.go:\d+:\d+: `)

func isBuildFailure(output string) bool {
	if strings.Contains(output, "[build failed]") || strings.Contains(output, "[setup failed]") {
		return true
	}
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

func classify(exitErr error, output string) (status, error) {
	if exitErr == nil {
		if strings.Contains(output, "warning: no tests to run") ||
			strings.Contains(output, "testing: warning: no tests to run") {
			return 0, fmt.Errorf("go test matched no tests; every mutant would read as survived:\n%s", truncate(output))
		}
		return survivedStatus, nil
	}
	if strings.Contains(output, "--- FAIL") {
		return caughtStatus, nil
	}
	if isBuildFailure(output) {
		return brokenStatus, nil
	}
	switch {
	case strings.Contains(output, "\nFAIL\t"),
		strings.HasPrefix(output, "FAIL\t"),
		strings.Contains(output, "panic: "):
		return caughtStatus, nil
	}
	return 0, fmt.Errorf("go test failed without reporting a test failure or a build error; "+
		"this is a harness problem, not a mutation result:\n%s", truncate(output))
}

type restoreSet struct {
	mu       sync.Mutex
	files    map[string][]byte
	stopping bool
}

func newRestoreSet() *restoreSet { return &restoreSet{files: map[string][]byte{}} }

func (r *restoreSet) addAndWrite(path string, original, mutated []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopping {
		return errors.New("run is stopping; refusing to write another mutant")
	}
	if err := writeVerified(path, original, mutated); err != nil {
		return err
	}
	r.files[path] = original
	return nil
}

func (r *restoreSet) stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopping = true
}

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

func (r *restoreSet) interrupt() {
	r.stop()
	r.runAll()
}

func (r *restoreSet) runAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for path, original := range r.files {
		restore(path, original)
		delete(r.files, path)
	}
}

func lockPackage(dir string) (func(), error) {
	path := filepath.Join(dir, ".mutate.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("another mutate run holds %s.\n"+
				"If a run is active, wait; concurrent runs read each other's mutants as their baseline.\n"+
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

func packageFiles(pkg string) (string, []string, error) {
	out, err := exec.Command("go", "list", "-json", pkg).Output()
	if err != nil {
		return "", nil, fmt.Errorf("go list %s: %w", pkg, err)
	}
	type listPkg struct {
		Dir     string
		GoFiles []string
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	var pkgs []listPkg
	for {
		var p listPkg
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			return "", nil, fmt.Errorf("go list %s: parse: %w", pkg, err)
		}
		pkgs = append(pkgs, p)
	}
	if len(pkgs) == 0 {
		return "", nil, fmt.Errorf("no Go packages matched %s", pkg)
	}
	if len(pkgs) > 1 {
		dirs := make([]string, 0, len(pkgs))
		for _, p := range pkgs {
			dirs = append(dirs, p.Dir)
		}
		return "", nil, fmt.Errorf("pattern %s matches %d packages (%s) — mutate rewrites source in place and takes one package at a time; name it directly",
			pkg, len(dirs), strings.Join(dirs, ", "))
	}
	if len(pkgs[0].GoFiles) == 0 {
		return "", nil, fmt.Errorf("no Go files in %s", pkg)
	}
	return pkgs[0].Dir, pkgs[0].GoFiles, nil
}
