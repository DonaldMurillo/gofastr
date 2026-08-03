package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

func devReport(t *testing.T, n int) *contracts.Report {
	t.Helper()
	rule, ok := contracts.LookupRule(contracts.RuleUnguardedMutation)
	if !ok {
		t.Fatal("rule missing from catalog")
	}
	r := &contracts.Report{FailOn: contracts.SeverityWarn}
	for i := 0; i < n; i++ {
		r.Diagnostics = append(r.Diagnostics, contracts.Diagnostic{
			RuleID: rule.ID, Slug: rule.Slug, Capability: rule.Capability,
			Severity: contracts.SeverityWarn,
			File:     "main.go", Line: i + 1,
			Message: "unguarded mutation",
		})
	}
	return r
}

func TestDevSummaryIsCompactAndActionable(t *testing.T) {
	w := newDevContractWatch(".")
	out := w.summarise(devReport(t, 2))

	// The dev loop gets the rule ID and location, not the full reasoning —
	// that is what `gofastr verify` is for, and repeating it every save
	// would bury the loop.
	if !strings.Contains(out, contracts.RuleUnguardedMutation) {
		t.Errorf("summary omits the rule ID:\n%s", out)
	}
	if !strings.Contains(out, "main.go:1") {
		t.Errorf("summary omits the location:\n%s", out)
	}
	if !strings.Contains(out, "--explain") {
		t.Errorf("summary does not say how to get the reasoning:\n%s", out)
	}
	// The long-form Why must NOT be inlined here.
	rule, _ := contracts.LookupRule(contracts.RuleUnguardedMutation)
	if strings.Contains(out, rule.Why) {
		t.Errorf("the dev summary inlined the full Why — that belongs in verify:\n%s", out)
	}
}

func TestDevSummaryCapsTheWall(t *testing.T) {
	// A wall of findings after a save is something you scroll past.
	w := newDevContractWatch(".")
	out := w.summarise(devReport(t, devContractMaxLines+5))

	lines := 0
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, contracts.RuleUnguardedMutation) {
			lines++
		}
	}
	if lines != devContractMaxLines {
		t.Errorf("printed %d findings, want the cap of %d", lines, devContractMaxLines)
	}
	if !strings.Contains(out, "5 more") {
		t.Errorf("the truncation is silent — a capped report must say what it hid:\n%s", out)
	}
}

func TestDevWatchSkipsAnInFlightRun(t *testing.T) {
	// A burst of saves must not stack analyses; the newer save supersedes
	// the older one anyway.
	w := newDevContractWatch(t.TempDir())
	w.mu.Lock()
	w.running = true
	w.mu.Unlock()

	w.Run() // must return without starting a second analysis
	w.mu.Lock()
	still := w.running
	w.mu.Unlock()
	if !still {
		t.Error("an in-flight run was cleared by a concurrent Run")
	}
}

func TestDevWatchIsConcurrencySafe(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module example.com/dev\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := newDevContractWatch(dir)

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); w.Run() }()
	}
	wg.Wait()
	// The analyses run in goroutines; this asserts Run() itself is safe to
	// call concurrently, which is the contract the reload loop relies on.
}

func TestDevBaselinePathJoins(t *testing.T) {
	if got := devBaselinePath("."); got != contracts.BaselineFileName {
		t.Errorf("devBaselinePath(.) = %q", got)
	}
	if got := devBaselinePath("/tmp/app/"); got != "/tmp/app/"+contracts.BaselineFileName {
		t.Errorf("a trailing slash produced %q", got)
	}
	if got := devBaselinePath("/tmp/app"); got != "/tmp/app/"+contracts.BaselineFileName {
		t.Errorf("devBaselinePath(/tmp/app) = %q", got)
	}
}

// captureWatch runs one synchronous analysis pass with stdout captured.
// analyse is called directly rather than through Run: the goroutine and
// the in-flight guard are covered above; here the reporting is under
// test.
func captureWatch(t *testing.T, w *devContractWatch) string {
	t.Helper()
	origStdout := os.Stdout
	r, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = pw
	defer func() { os.Stdout = origStdout }()

	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, readErr := r.Read(buf)
			b.Write(buf[:n])
			if readErr != nil {
				break
			}
		}
		done <- b.String()
	}()

	w.analyse()
	pw.Close()
	out := <-done
	r.Close()
	return out
}

// writeWatchTree lays out a tree with one GOFASTR1002 finding, plus any
// extra files, and returns its directory.
func writeWatchTree(t *testing.T, extra map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.26\n",
		"main.go": "package main\n\n" +
			"import \"github.com/DonaldMurillo/gofastr/framework\"\n\n" +
			"func main() {\n\tapp := framework.NewApp()\n\tapp.Router().Get(\"/users/:id\", nil)\n}\n",
	}
	for name, body := range extra {
		files[name] = body
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// The failure class under test below: the analysis itself failing. Every
// silent `return` in analyse leaves the loop printing nothing while the
// analyzers have not actually looked — which reads as "clean".

func TestWatcherReportsAnUnreadableTree(t *testing.T) {
	w := newDevContractWatch(filepath.Join(t.TempDir(), "does-not-exist"))
	out := captureWatch(t, w)
	if !strings.Contains(out, "contracts") || !strings.Contains(out, "does-not-exist") {
		t.Errorf("a tree that could not be scanned was swallowed silently; output:\n%s", out)
	}
	// Once, not on every save.
	if again := captureWatch(t, w); again != "" {
		t.Errorf("the same failure was reprinted on the next save:\n%s", again)
	}
}

func TestWatcherReportsACorruptBaseline(t *testing.T) {
	dir := writeWatchTree(t, map[string]string{
		".gofastr-contracts-baseline.json": "{ not json",
	})
	out := captureWatch(t, newDevContractWatch(dir))
	if !strings.Contains(out, "baseline") {
		t.Errorf("a corrupt baseline was skipped without a word — the loop would flood with the debt it records; output:\n%s", out)
	}
	// Matching `gofastr verify`: an unreadable baseline fails the pass
	// rather than reporting findings against a state nobody chose.
	if strings.Contains(out, "GOFASTR1002") {
		t.Errorf("findings were reported despite the unreadable baseline:\n%s", out)
	}
}

func TestWatcherSaysWhenTheChangedSetIsUnavailable(t *testing.T) {
	// A repository with no commits: `git diff HEAD` has nothing to
	// resolve, which is exactly the state of a fresh project after `git
	// init` and before the first commit. The loop must fall back to the
	// whole tree and say so, not silently widen.
	dir := writeWatchTree(t, nil)
	for _, args := range [][]string{{"init"}, {"add", "."}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if msg, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v unavailable: %v\n%s", args, err, msg)
		}
	}
	out := captureWatch(t, newDevContractWatch(dir))
	if !strings.Contains(out, "GOFASTR1002") {
		t.Fatalf("the finding disappeared with the changed-set:\n%s", out)
	}
	if !strings.Contains(out, "whole tree") {
		t.Errorf("the fallback to whole-tree reporting was silent:\n%s", out)
	}
}

// The fix hint is only useful when a fix exists. Offering it against a
// report of rules that have no autofix teaches people to ignore the line.
func TestDevSummaryOffersTheFixOnlyWhenOneExists(t *testing.T) {
	w := newDevContractWatch(".")

	unfixable := w.summarise(devReport(t, 1))
	if strings.Contains(unfixable, "--fix") {
		t.Errorf("summary offered a fix for a rule that has none:\n%s", unfixable)
	}

	r := devReport(t, 1)
	r.Diagnostics = append(r.Diagnostics, contracts.Diagnostic{
		RuleID:   contracts.RuleNonUppercaseVerb,
		Severity: contracts.SeverityError,
		File:     "main.go", Line: 9,
		Message: "lowercase method",
		Fix: &contracts.SuggestedFix{Edits: []contracts.TextEdit{
			{File: "main.go", Start: 0, End: 1, New: `"POST"`},
		}},
	})
	fixable := w.summarise(r)
	if !strings.Contains(fixable, "--rule "+contracts.RuleNonUppercaseVerb+" --fix") {
		t.Errorf("summary does not offer the rule-scoped fix:\n%s", fixable)
	}
}
