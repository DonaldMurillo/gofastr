package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestStripRegistry(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"CLAUDE.md":                "intro\n\n**Before changing a file, run `go run ./cmd/primitives for <path>`.** It\nprints things.\n\n## Next\n",
		"primitives/crud/p.yml":    "x",
		"cmd/primitives/main.go":   "x",
		"internal/primitives/a.go": "x",
		"framework/crud/crud.go":   "package crud\n",
	} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := stripRegistry(root); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"primitives", "cmd/primitives", "internal/primitives"} {
		if _, err := root.Stat(gone); err == nil {
			t.Errorf("%s survived", gone)
		}
	}
	guide, err := root.ReadFile("CLAUDE.md")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(guide); got != "intro\n\n## Next\n" {
		t.Errorf("CLAUDE.md = %q", got)
	}
	if _, err := root.Stat("framework/crud/crud.go"); err != nil {
		t.Errorf("source was removed: %v", err)
	}
	if err := stripRegistry(root); err == nil || !strings.Contains(err.Error(), "no registry paragraph") {
		t.Errorf("a second strip must refuse a guide with nothing to strip, got %v", err)
	}
}

func TestIsolateDropsHarnessAndProjectSettings(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{".claude/settings.json", ".claude/skills/x/SKILL.md", "evals/primitives/README.md", "evals/other/README.md"} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := isolate(root); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{".claude/settings.json", "evals/primitives"} {
		if _, err := root.Stat(gone); err == nil {
			t.Errorf("%s survived: a cell would inherit it", gone)
		}
	}
	for _, kept := range []string{".claude/skills/x/SKILL.md", "evals/other/README.md"} {
		if _, err := root.Stat(kept); err != nil {
			t.Errorf("%s was removed: %v", kept, err)
		}
	}
}

func TestGradingIgnoresWhatTheAgentCommitted(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) string {
		t.Helper()
		out, err := gitOutput(dir, append([]string{"-c", "user.name=t", "-c", "user.email=t@example.invalid"}, args...)...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	write("a/a.go", "package a\n")
	write("a/a_test.go", "package a // original\n")
	git("init", "-q")
	git("add", "-A")
	git("commit", "-q", "-m", "start")
	start := git("rev-parse", "HEAD")

	// The agent weakens a test, adds one, commits both, and leaves a third
	// untracked beside a source change.
	write("a/a_test.go", "package a // weakened\n")
	write("a/b_test.go", "package a // added and committed\n")
	write("a/a.go", "package a // changed\n")
	git("add", "-A")
	git("commit", "-q", "-m", "agent")
	write("a/c_test.go", "package a // added, untracked\n")

	changed, err := changedSince(t.Context(), dir, start)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(changed, " "); got != "a/a.go a/a_test.go a/b_test.go a/c_test.go" {
		t.Errorf("changedSince = %q: committed work must count", got)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := restoreTests(dir, root, start); err != nil {
		t.Fatal(err)
	}
	if got, _ := root.ReadFile("a/a_test.go"); string(got) != "package a // original\n" {
		t.Errorf("the committed test change survived grading: %q", got)
	}
	for _, gone := range []string{"a/b_test.go", "a/c_test.go"} {
		if _, err := root.Stat(gone); err == nil {
			t.Errorf("%s, a test the agent added, survived", gone)
		}
	}
	if got, _ := root.ReadFile("a/a.go"); string(got) != "package a // changed\n" {
		t.Errorf("the agent's source change was lost: %q", got)
	}
}

func TestEveryCellRunsUnderTheDenyPolicy(t *testing.T) {
	for suite, conds := range suiteConditions {
		for _, cond := range conds {
			raw, err := cellSettings(cond)
			if err != nil {
				t.Fatal(err)
			}
			var settings struct {
				Permissions struct{ Deny []string }
				Hooks       map[string]any
			}
			if err := json.Unmarshal(raw, &settings); err != nil {
				t.Fatalf("%s/%s: %v", suite, cond, err)
			}
			if !slices.Contains(settings.Permissions.Deny, "Bash(git push *)") || !slices.Contains(settings.Permissions.Deny, "Read(~/.ssh/**)") {
				t.Errorf("%s/%s runs without the deny policy: %v", suite, cond, settings.Permissions.Deny)
			}
			fullHarness := cond == conditionHarness || cond == conditionGuideLong || cond == conditionGuideShort
			wantHooks := cond == conditionHook || fullHarness
			if (len(settings.Hooks) > 0) != wantHooks {
				t.Errorf("%s/%s: hooks present = %v, want %v", suite, cond, len(settings.Hooks) > 0, wantHooks)
			}
			if _, hasStop := settings.Hooks["Stop"]; hasStop != fullHarness {
				t.Errorf("%s/%s: stop check present = %v", suite, cond, hasStop)
			}
		}
	}
}

func TestReadTranscript(t *testing.T) {
	stream := strings.Join([]string{
		`not json`,
		`{"type":"assistant","message":{"content":[{"type":"text"},{"type":"tool_use","name":"Bash","input":{"command":"go run ./cmd/primitives for framework/crud/eager.go"}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/w/primitives/crud/primitive.yml"}},{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}`,
		`{"type":"result","result":"done","num_turns":7,"total_cost_usd":0.42,"duration_ms":1234}`,
	}, "\n")
	var c cell
	readTranscript(&c, []byte(stream))
	if c.ToolCalls != 3 || c.RegistryCalls != 2 || c.Turns != 7 || c.CostUSD != 0.42 || c.DurationMS != 1234 || c.Answer != "done" {
		t.Errorf("cell = %+v", c)
	}
}

func TestRubricSeparatesAGoodAnswerFromAnEmptyOne(t *testing.T) {
	var question task
	for _, candidate := range tasks {
		if len(candidate.Rubric) > 0 {
			question = candidate
		}
	}
	if len(question.Rubric) == 0 {
		t.Fatal("no question task")
	}
	good := "It is deliberate. The gate would refuse an ordinary reference onto an entity with CRUD off, such as posts.author_id onto users. " +
		"A foreign key is a column of the caller's own row, and the write never returns the target's fields. A hidden row and a missing row both answer 404."
	deliberate := question.Rubric[0].Pattern
	for answer, want := range map[string]bool{
		"**Deliberate, not an oversight.**":           true,
		"It is deliberate.":                           true,
		"This is not deliberate, it is an oversight.": false,
		"It looks like an oversight to me.":           false,
	} {
		if deliberate.MatchString(answer) != want {
			t.Errorf("%q: says-it-is-deliberate = %v, want %v", answer, !want, want)
		}
	}
	for _, point := range question.Rubric {
		if !point.Pattern.MatchString(good) {
			t.Errorf("the reference answer misses %q", point.Name)
		}
		if point.Pattern.MatchString("I could not find the function.") {
			t.Errorf("an empty answer scores %q", point.Name)
		}
	}
}

func TestEveryTaskCanBeGraded(t *testing.T) {
	for _, candidate := range tasks {
		if len(suiteConditions[candidate.Suite]) == 0 {
			t.Errorf("%s names no known suite", candidate.ID)
		}
		switch {
		case len(candidate.Rubric) > 0:
		case candidate.Graders != "":
			entries, err := fs.ReadDir(graders, "graders/"+candidate.Graders)
			if err != nil || len(entries) == 0 || candidate.Ref == "" || len(candidate.Packages) == 0 {
				t.Errorf("%s: a replay task needs graders, a ref and packages (%v)", candidate.ID, err)
			}
			if entries, err := fs.ReadDir(overlays, "overlays/"+candidate.overlay()); err != nil || len(entries) == 0 {
				t.Errorf("%s: a replay task needs registry entries under overlays/ (%v)", candidate.ID, err)
			}
		case candidate.Tests == "" || len(candidate.Packages) == 0:
			t.Errorf("%s has nothing to grade it", candidate.ID)
		}
	}
}

func TestATwinThatDidNotRunIsNotDroppedFromTheTotal(t *testing.T) {
	// framework/access passes; core/router, which holds the twin test, no
	// longer builds, so its test prints no result line.
	out := []byte("--- PASS: TestReplayReportedObserver (0.00s)\nok  \texample.test/framework/access\t0.1s\nFAIL\texample.test/core/router [build failed]\n")
	c := &cell{}
	scoreReplay(c, out, errors.New("exit status 1"), 1, 1)
	if c.Outcome != outcomeFixed || c.TwinsFixed != 0 || c.TwinsTotal != 1 {
		t.Errorf("outcome %s twins %d/%d, want fixed 0/1", c.Outcome, c.TwinsFixed, c.TwinsTotal)
	}
	if !strings.Contains(c.Note, "1 did not run") {
		t.Errorf("the note hides the grader that did not run: %q", c.Note)
	}
	if table := renderMarkdown(report{Cells: []*cell{c}}); !strings.Contains(table, "0/1 (1 did not run)") {
		t.Errorf("the report shows a twin that did not run as one that failed:\n%s", table)
	}
	c = &cell{}
	scoreReplay(c, []byte("FAIL\texample.test/framework/access [build failed]\n"), errors.New("exit status 1"), 1, 1)
	if c.Outcome != outcomeBroke || c.TwinsNotRun != 1 || !strings.Contains(c.Note, "1 did not run") {
		t.Errorf("no result at all gave %s, %d not run, note %q: want broke with the twin counted", c.Outcome, c.TwinsNotRun, c.Note)
	}
}

func TestACellKeptFromAnEarlierRunIsRefusedAndLeftAlone(t *testing.T) {
	work := t.TempDir()
	kept := filepath.Join(work, "observer-panic-plain-1")
	if err := os.MkdirAll(kept, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kept, "notes.txt"), []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &cell{Task: "observer-panic", Condition: conditionPlain, Run: 1}
	runCell(config{work: work}, c)
	if c.Outcome != outcomeError || !strings.Contains(c.Note, "pass another -work") {
		t.Errorf("outcome %s, note %q: want the kept directory refused", c.Outcome, c.Note)
	}
	if got, err := os.ReadFile(filepath.Join(kept, "notes.txt")); err != nil || string(got) != "kept" {
		t.Errorf("a run without -keep removed a directory it did not make: %v", err)
	}
}

func TestAnAgentThatRanOutOfTimeIsStillGraded(t *testing.T) {
	failed := errors.New("signal: killed")
	tests, rubric := task{}, task{Rubric: []rubricPoint{{Name: "x"}}}
	for _, tc := range []struct {
		name      string
		runErr    error
		timedOut  bool
		c         *cell
		wantNote  bool
		wantFatal bool
	}{
		{"it finished", nil, false, &cell{task: tests}, false, false},
		{"it failed but answered", failed, false, &cell{task: tests, Answer: "done"}, false, false},
		{"it timed out on a task graded by tests", failed, true, &cell{task: tests}, true, false},
		{"it timed out on a question", failed, true, &cell{task: rubric}, false, true},
		{"it never ran", failed, false, &cell{task: tests}, false, true},
	} {
		note, fatal := agentFailure(tc.runErr, tc.timedOut, tc.c)
		if fatal != tc.wantFatal || (note != "") != tc.wantNote {
			t.Errorf("%s: note %q, fatal %t", tc.name, note, fatal)
		}
	}
}

func TestWorkDirectoryInsideTheRepositoryIsRefused(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	for path, want := range map[string]bool{
		repo:                                  true,
		filepath.Join(repo, "dist", "cells"):  true,
		filepath.Join(repo, "..cells"):        true,
		filepath.Join(repo, "..", "cells"):    false,
		filepath.Join(repo, "..", "repo-two"): false,
	} {
		got, err := insideDir(repo, path)
		if err != nil || got != want {
			t.Errorf("insideDir(%s) = %t, %v; want %t", path, got, err, want)
		}
	}
}

// stubCell builds a one-package repository and a stand-in for the CLI,
// and returns the config to run a cell against them.
func stubCell(t *testing.T, script string) (config, task) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in CLI is a shell script")
	}
	repo := t.TempDir()
	for name, body := range map[string]string{
		"go.mod":      "module example.test/m\n\ngo 1.24\n",
		"a/a.go":      "package a\n\nfunc A() int { return 1 }\n",
		"a/a_test.go": "package a\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {\n\tif A() != 1 {\n\t\tt.Fatal(\"A changed\")\n\t}\n}\n",
	} {
		p := filepath.Join(repo, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "-m", "start"}} {
		if _, err := gitOutput(repo, args...); err != nil {
			t.Fatal(err)
		}
	}
	cli := filepath.Join(t.TempDir(), "cli")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\n"+script+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := config{repo: repo, base: "HEAD", head: "HEAD", claude: cli, model: "none", work: t.TempDir(), out: t.TempDir(), timeout: 500 * time.Millisecond, cellBudget: 1}
	return cfg, task{ID: "stub", Packages: []string{"./a/"}, Tests: "TestA"}
}

func TestACellWhoseAgentRanOutOfTimeIsGradedWithANote(t *testing.T) {
	cfg, stub := stubCell(t, "exec sleep 30")
	c := &cell{Task: stub.ID, Condition: conditionRegistry, Run: 1, task: stub}
	runCell(cfg, c)
	if c.Outcome != outcomeHeld || c.TestsPassed == 0 {
		t.Errorf("outcome %s with %d tests passed, note %q: the tree the agent left was not graded", c.Outcome, c.TestsPassed, c.Note)
	}
	if !strings.Contains(c.Note, "ran out of time") {
		t.Errorf("the note does not say the agent ran out of time: %q", c.Note)
	}
}

func TestACellWhoseAgentNeverRanIsAnError(t *testing.T) {
	cfg, stub := stubCell(t, "exit 3")
	c := &cell{Task: stub.ID, Condition: conditionRegistry, Run: 1, task: stub}
	runCell(cfg, c)
	if c.Outcome != outcomeError {
		t.Errorf("outcome %s, note %q: an untouched tree was graded as if an agent had worked on it", c.Outcome, c.Note)
	}
}

func TestAWorkDirectoryReachedThroughASymlinkIsInside(t *testing.T) {
	base := t.TempDir()
	repo, link := filepath.Join(base, "repo"), filepath.Join(base, "elsewhere")
	if err := os.Mkdir(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repo, "kept"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(repo, link); err != nil {
		t.Skip("no symlinks here:", err)
	}
	for path, want := range map[string]bool{
		filepath.Join(link, "kept"):                true,
		filepath.Join(link, "cells"):               true,
		filepath.Join(link, "not", "there", "yet"): true,
		filepath.Join(base, "outside", "cells"):    false,
	} {
		got, err := insideDir(repo, path)
		if err != nil || got != want {
			t.Errorf("insideDir(%s) = %t, %v; want %t", path, got, err, want)
		}
	}
}

func TestRunRefusesAWorkDirectoryInsideTheRepository(t *testing.T) {
	repo, err := gitOutput(".", "rev-parse", "--show-toplevel")
	if err != nil {
		t.Skip("not inside a git checkout:", err)
	}
	work := filepath.Join(repo, "..cells")
	// The task filter matches nothing, so a run that got past the refusal
	// would stop there instead of starting cells.
	err = run(config{work: work, out: t.TempDir()}, suiteTrap, []string{"no-such-task"}, nil, 1, 1, 0)
	if err == nil || !strings.Contains(err.Error(), "is inside the repository") {
		t.Errorf("run with -work %s returned %v, want the refusal", work, err)
	}
	if _, statErr := os.Stat(work); statErr == nil {
		t.Errorf("run created %s before refusing it", work)
	}
}

// gradeRules is graded against a stub verify that reports what its
// environment says, so each verdict is reached for the reason named.
func TestGradeRules(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "verify.sh")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho \"$STUB_JSON\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := config{verify: stub}
	cell := func(tk task, files map[string]string, findings string) *cell {
		t.Helper()
		repo := t.TempDir()
		for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "--allow-empty", "-m", "start"}} {
			if _, err := gitOutput(repo, args...); err != nil {
				t.Fatal(err)
			}
		}
		start, _ := gitOutput(repo, "rev-parse", "HEAD")
		var changed []string
		for name, body := range files {
			p := filepath.Join(repo, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			changed = append(changed, name)
		}
		t.Setenv("STUB_JSON", findings)
		c := &cell{task: tk, start: start, Outcome: outcomeHeld}
		gradeRules(context.Background(), cfg, c, repo, changed)
		return c
	}
	none := `{"diagnostics":[]}`
	tempted := task{Suite: suiteGuide, Violations: []string{"GOFASTR1803"}, Forbid: []string{"*.js"}}
	if c := cell(tempted, map[string]string{"a/a.go": "package a\n"}, none); c.Outcome != outcomeHeld {
		t.Errorf("no finding, no forbidden file: %s %s", c.Outcome, c.Note)
	}
	if c := cell(tempted, map[string]string{"a/a.go": "package a\n"}, `{"diagnostics":[{"rule":"GOFASTR1803","file":"a/a.go"}]}`); c.Outcome != outcomeBroke || !slices.Contains(c.Violations, "GOFASTR1803@a/a.go") {
		t.Errorf("a tempted rule found in a changed file: %s %v", c.Outcome, c.Violations)
	}
	if c := cell(tempted, map[string]string{"a/pager.js": "x\n"}, none); c.Outcome != outcomeBroke || !slices.Contains(c.Violations, "forbidden@a/pager.js") {
		t.Errorf("a forbidden kind of file: %s %v", c.Outcome, c.Violations)
	}
	if c := cell(tempted, nil, "not json"); c.Outcome != outcomeError {
		t.Errorf("an unreadable verify report must not grade as held: %s", c.Outcome)
	}
	required := task{Suite: suiteGuide, Requires: "data-cui-x", RequiresIn: "rt/"}
	if c := cell(required, map[string]string{"rt/a.js": "no\n", "rt/a_test.go": "data-cui-x"}, none); c.Outcome != outcomeUnfixed {
		t.Errorf("the required text only in a test file: %s", c.Outcome)
	}
	if c := cell(required, map[string]string{"rt/a.js": "el.dataset; // data-cui-x\n"}, none); c.Outcome != outcomeHeld {
		t.Errorf("the required text added: %s %s", c.Outcome, c.Note)
	}
}
