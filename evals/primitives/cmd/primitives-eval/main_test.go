package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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
			wantHooks := cond == conditionHook || cond == conditionHarness
			if (len(settings.Hooks) > 0) != wantHooks {
				t.Errorf("%s/%s: hooks present = %v, want %v", suite, cond, len(settings.Hooks) > 0, wantHooks)
			}
			if _, hasStop := settings.Hooks["Stop"]; hasStop != (cond == conditionHarness) {
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
