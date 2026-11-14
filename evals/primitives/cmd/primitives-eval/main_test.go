package main

import (
	"io/fs"
	"os"
	"path/filepath"
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
		case candidate.Tests == "" || len(candidate.Packages) == 0:
			t.Errorf("%s has nothing to grade it", candidate.ID)
		}
	}
}
