package main

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

//go:embed graders
var graders embed.FS

//go:embed overlays
var overlays embed.FS

const (
	outcomeFixed    = "fixed"
	outcomeUnfixed  = "unfixed"
	outcomeHeld     = "held"
	outcomeBroke    = "broke"
	outcomeAnswered = "answered"
	outcomeError    = "error"
	outcomeSkipped  = "skipped"
)

type cell struct {
	Task      string `json:"task"`
	Condition string `json:"condition"`
	Run       int    `json:"run"`
	task      task

	Outcome       string   `json:"outcome"`
	Note          string   `json:"note,omitempty"`
	ChangedSource bool     `json:"changed_source"`
	ChangedTests  bool     `json:"changed_tests"`
	ChangedFiles  []string `json:"changed_files,omitempty"`
	TestsPassed   int      `json:"tests_passed"`
	TestsFailed   int      `json:"tests_failed"`
	TwinsFixed    int      `json:"twins_fixed"`
	TwinsTotal    int      `json:"twins_total"`
	RubricHit     []string `json:"rubric_hit,omitempty"`
	RubricMissed  []string `json:"rubric_missed,omitempty"`
	RegistryCalls int      `json:"registry_calls"` // tool calls that name primitives/ or cmd/primitives
	ToolCalls     int      `json:"tool_calls"`
	Turns         int      `json:"turns"`
	CostUSD       float64  `json:"cost_usd"`
	DurationMS    int64    `json:"duration_ms"`
	Answer        string   `json:"answer"`
}

type report struct {
	Model    string  `json:"model"`
	Base     string  `json:"base"`
	Head     string  `json:"head"`
	Started  string  `json:"started"`
	SpentUSD float64 `json:"spent_usd"`
	Cells    []*cell `json:"cells"`
}

func runCell(cfg config, c *cell) {
	name := fmt.Sprintf("%s-%s-%d", c.Task, c.Condition, c.Run)
	dir := filepath.Join(cfg.work, name)
	fail := func(err error) {
		c.Outcome, c.Note = outcomeError, err.Error()
	}
	if err := prepare(cfg, c.task, c.Condition, dir); err != nil {
		fail(err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	transcript, err := os.OpenFile(filepath.Join(cfg.out, name+".jsonl"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		fail(err)
		return
	}
	var stream bytes.Buffer
	prompt := c.task.Prompt
	if c.Condition == conditionNudge {
		prompt += nudge
	}
	agent := command(ctx, dir, cfg.claude,
		"-p", prompt,
		"--model", cfg.model,
		"--output-format", "stream-json", "--verbose",
		"--permission-mode", "acceptEdits",
		"--allowedTools", "Read", "Edit", "Write", "Grep", "Glob", "Bash",
		"--setting-sources", "project",
		"--strict-mcp-config",
		"--no-session-persistence",
		"--max-budget-usd", fmt.Sprintf("%.2f", cfg.cellBudget),
	)
	agent.Stdout = &stream
	agent.Stderr = &stream
	runErr := agent.Run()
	_, writeErr := transcript.Write(stream.Bytes())
	if err := errors.Join(writeErr, transcript.Close()); err != nil {
		fail(err)
		return
	}
	readTranscript(c, stream.Bytes())
	if runErr != nil && c.Answer == "" {
		fail(fmt.Errorf("agent: %w", runErr))
		return
	}

	status, err := gitOutput(dir, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		fail(err)
		return
	}
	for line := range strings.SplitSeq(status, "\n") {
		if len(line) < 4 {
			continue
		}
		file := strings.TrimSpace(line[3:])
		c.ChangedFiles = append(c.ChangedFiles, file)
		if strings.HasSuffix(file, "_test.go") {
			c.ChangedTests = true
		} else if strings.HasSuffix(file, ".go") {
			c.ChangedSource = true
		}
	}

	if len(c.task.Rubric) > 0 {
		c.Outcome = outcomeAnswered
		for _, point := range c.task.Rubric {
			if point.Pattern.MatchString(c.Answer) {
				c.RubricHit = append(c.RubricHit, point.Name)
			} else {
				c.RubricMissed = append(c.RubricMissed, point.Name)
			}
		}
		return
	}
	gradeTests(ctx, cfg, c, dir)
}

// prepare writes one condition's tree into dir as a fresh one-commit
// repository, so a cell cannot read this checkout's history.
func prepare(cfg config, t task, condition, dir string) error {
	ref := cfg.head
	switch {
	case t.Ref != "":
		ref = t.Ref
	case condition == conditionComments:
		ref = cfg.base
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	ctx := context.Background()
	archive := command(ctx, cfg.repo, "git", "archive", "--format=tar", ref)
	extract := command(ctx, dir, "tar", "-x")
	pipe, err := archive.StdoutPipe()
	if err != nil {
		return err
	}
	extract.Stdin = pipe
	if err := extract.Start(); err != nil {
		return err
	}
	if err := errors.Join(archive.Run(), extract.Wait()); err != nil {
		return fmt.Errorf("extract %s: %w", ref, err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	// The harness holds the prompts and the grading: no cell may read it.
	if err := root.RemoveAll("evals/primitives"); err != nil {
		return err
	}
	if condition == conditionBare {
		if err := stripRegistry(root); err != nil {
			return err
		}
	}
	if t.Ref != "" && condition == conditionRegistry {
		if err := overlayRegistry(ctx, cfg, t, dir, root); err != nil {
			return err
		}
	}
	// A grader file is held out: every cell starts from the -base copy and
	// meets the -head copy only when it is graded.
	for _, file := range t.GraderFiles {
		src, err := command(ctx, cfg.repo, "git", "show", cfg.base+":"+file).Output()
		if err != nil {
			return fmt.Errorf("grader file %s at %s: %w", file, cfg.base, err)
		}
		if err := root.WriteFile(file, src, 0o600); err != nil {
			return err
		}
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "-A"},
		{"-c", "user.name=eval", "-c", "user.email=eval@example.invalid", "commit", "-q", "-m", "start"},
	} {
		if _, err := gitOutput(dir, args...); err != nil {
			return err
		}
	}
	return nil
}

// overlayRegistry gives a replay cell, cut from a commit older than the
// registry, the registry's tooling from -head, the task's own entries and
// the pointer in CLAUDE.md.
func overlayRegistry(ctx context.Context, cfg config, t task, dir string, root *os.Root) error {
	archive := command(ctx, cfg.repo, "git", "archive", "--format=tar", cfg.head, "cmd/primitives", "internal/primitives", "primitives/README.md")
	extract := command(ctx, dir, "tar", "-x")
	pipe, err := archive.StdoutPipe()
	if err != nil {
		return err
	}
	extract.Stdin = pipe
	if err := extract.Start(); err != nil {
		return err
	}
	if err := errors.Join(archive.Run(), extract.Wait()); err != nil {
		return fmt.Errorf("overlay the registry tooling: %w", err)
	}
	base := path.Join("overlays", t.ID)
	err = fs.WalkDir(overlays, base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(p, base+"/")
		if d.IsDir() {
			if p == base {
				return nil
			}
			return root.MkdirAll(rel, 0o700)
		}
		src, err := overlays.ReadFile(p)
		if err != nil {
			return err
		}
		return root.WriteFile(rel, src, 0o600)
	})
	if err != nil {
		return fmt.Errorf("overlay %s: %w", t.ID, err)
	}
	guide, err := root.ReadFile("CLAUDE.md")
	if err != nil {
		return err
	}
	const anchor = "## TL;DR"
	i := bytes.Index(guide, []byte(anchor))
	if i < 0 {
		return fmt.Errorf("CLAUDE.md at %s has no %q heading to put the registry pointer before", t.Ref, anchor)
	}
	return root.WriteFile("CLAUDE.md", slices.Concat(guide[:i], []byte(registryPointer), guide[i:]), 0o600)
}

var registryParagraph = regexp.MustCompile(`(?s)\*\*Before changing a file, run .go run \./cmd/primitives for.*?\n\n`)

// stripRegistry removes the registry, its command and every pointer to
// it, leaving the swept code and the tests that enforce it.
func stripRegistry(root *os.Root) error {
	for _, p := range []string{"primitives", "cmd/primitives", "internal/primitives", ".claude/skills/primitives-sweep"} {
		if err := root.RemoveAll(p); err != nil {
			return err
		}
	}
	guide, err := root.ReadFile("CLAUDE.md")
	if err != nil {
		return err
	}
	loc := registryParagraph.FindIndex(guide)
	if loc == nil {
		return fmt.Errorf("CLAUDE.md holds no registry paragraph to strip: the bare condition would not be bare")
	}
	stripped := slices.Concat(guide[:loc[0]], guide[loc[1]:])
	if registryParagraph.Match(stripped) {
		return fmt.Errorf("CLAUDE.md still names the registry after one paragraph was removed")
	}
	return root.WriteFile("CLAUDE.md", stripped, 0o600)
}

type streamEvent struct {
	Type    string `json:"type"`
	Message struct {
		Content []struct {
			Type  string          `json:"type"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
	} `json:"message"`
	Result     string  `json:"result"`
	NumTurns   int     `json:"num_turns"`
	TotalCost  float64 `json:"total_cost_usd"`
	DurationMS int64   `json:"duration_ms"`
}

func readTranscript(c *cell, stream []byte) {
	scanner := bufio.NewScanner(bytes.NewReader(stream))
	scanner.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for scanner.Scan() {
		var ev streamEvent
		if json.Unmarshal(scanner.Bytes(), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "assistant":
			for _, block := range ev.Message.Content {
				if block.Type != "tool_use" {
					continue
				}
				c.ToolCalls++
				if bytes.Contains(block.Input, []byte("primitives/")) || bytes.Contains(block.Input, []byte("cmd/primitives")) {
					c.RegistryCalls++
				}
			}
		case "result":
			c.Answer, c.Turns, c.CostUSD, c.DurationMS = ev.Result, ev.NumTurns, ev.TotalCost, ev.DurationMS
		}
	}
}

var (
	testPass = regexp.MustCompile(`(?m)^--- PASS: `)
	testFail = regexp.MustCompile(`(?m)^--- FAIL: `)
)

// gradeTests puts the test files back as the condition shipped them and
// runs the task's enforcing tests against what the agent left.
func gradeTests(ctx context.Context, cfg config, c *cell, dir string) {
	fail := func(err error) { c.Outcome, c.Note = outcomeError, err.Error() }
	for _, args := range [][]string{
		{"checkout", "-q", "HEAD", "--", ":(glob)**/*_test.go"},
		{"clean", "-q", "-f", "--", ":(glob)**/*_test.go"},
	} {
		if _, err := gitOutput(dir, args...); err != nil {
			fail(err)
			return
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		fail(err)
		return
	}
	defer root.Close()
	for _, file := range c.task.GraderFiles {
		show := command(ctx, cfg.repo, "git", "show", cfg.head+":"+file)
		src, err := show.Output()
		if err == nil {
			err = root.WriteFile(file, src, 0o600)
		}
		if err != nil {
			fail(fmt.Errorf("grader file %s: %w", file, err))
			return
		}
	}
	if c.task.Graders != "" {
		gradeReplay(ctx, c, dir, root)
		return
	}
	args := append([]string{"test", "-count=1", "-v", "-run", c.task.Tests}, c.task.Packages...)
	out, err := command(ctx, dir, "go", args...).CombinedOutput()
	c.TestsPassed = len(testPass.FindAll(out, -1))
	c.TestsFailed = len(testFail.FindAll(out, -1))
	switch {
	case err == nil && c.TestsPassed > 0 && c.TestsFailed == 0:
		c.Outcome = outcomeHeld
	case c.TestsFailed > 0:
		c.Outcome = outcomeBroke
	case err == nil:
		c.Outcome, c.Note = outcomeError, "no enforcing test ran"
	default:
		c.Outcome, c.Note = outcomeBroke, "the packages no longer build or test: "+lastLine(out)
	}
}

var replayResult = regexp.MustCompile(`(?m)^--- (PASS|FAIL): TestReplay(Reported|Twin)\w*`)

// gradeReplay copies a replay task's held-out tests into the cell and
// scores the reported path and each twin path separately.
func gradeReplay(ctx context.Context, c *cell, dir string, root *os.Root) {
	fail := func(err error) { c.Outcome, c.Note = outcomeError, err.Error() }
	base := path.Join("graders", c.task.Graders)
	err := fs.WalkDir(graders, base, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		src, err := graders.ReadFile(p)
		if err != nil {
			return err
		}
		return root.WriteFile(strings.TrimSuffix(strings.TrimPrefix(p, base+"/"), ".txt"), src, 0o600)
	})
	if err != nil {
		fail(err)
		return
	}
	args := append([]string{"test", "-count=1", "-v", "-run", "^TestReplay(Reported|Twin)"}, c.task.Packages...)
	out, runErr := command(ctx, dir, "go", args...).CombinedOutput()
	reported, reportedPassed := 0, 0
	for _, m := range replayResult.FindAllSubmatch(out, -1) {
		passed := string(m[1]) == "PASS"
		if string(m[2]) == "Reported" {
			reported++
			if passed {
				reportedPassed++
			}
			continue
		}
		c.TwinsTotal++
		if passed {
			c.TwinsFixed++
		}
	}
	switch {
	case reported == 0:
		c.Outcome, c.Note = outcomeBroke, fmt.Sprintf("the graders did not run (%v): %s", runErr, lastLine(out))
	case reportedPassed == reported:
		c.Outcome, c.Note = outcomeFixed, fmt.Sprintf("twins %d/%d", c.TwinsFixed, c.TwinsTotal)
	default:
		c.Outcome, c.Note = outcomeUnfixed, fmt.Sprintf("twins %d/%d", c.TwinsFixed, c.TwinsTotal)
	}
}

func lastLine(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return lines[len(lines)-1]
}

func renderMarkdown(r report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Primitives eval\n\nModel `%s`, comments condition at `%s`, registry and bare at `%s`. Spent $%.2f.\n\n", r.Model, r.Base, r.Head, r.SpentUSD)
	b.WriteString("| Task | Condition | Run | Outcome | Tests pass/fail | Twins fixed | Rubric | Changed source | Changed tests | Registry calls | Tool calls | Cost |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, c := range r.Cells {
		rubric := ""
		if total := len(c.RubricHit) + len(c.RubricMissed); total > 0 {
			rubric = fmt.Sprintf("%d/%d", len(c.RubricHit), total)
		}
		twins := ""
		if c.TwinsTotal > 0 {
			twins = fmt.Sprintf("%d/%d", c.TwinsFixed, c.TwinsTotal)
		}
		fmt.Fprintf(&b, "| %s | %s | %d | %s | %d/%d | %s | %s | %t | %t | %d | %d | $%.2f |\n",
			c.Task, c.Condition, c.Run, c.Outcome, c.TestsPassed, c.TestsFailed, twins, rubric, c.ChangedSource, c.ChangedTests, c.RegistryCalls, c.ToolCalls, c.CostUSD)
	}
	return b.String()
}
