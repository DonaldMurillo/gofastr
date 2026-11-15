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
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
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
	// start is the cell's first commit. The agent can commit in the cell,
	// so everything measured or restored is taken against this, never HEAD.
	start string

	Outcome       string   `json:"outcome"`
	Note          string   `json:"note,omitempty"`
	ChangedSource bool     `json:"changed_source"`
	ChangedTests  bool     `json:"changed_tests"`
	ChangedDocs   bool     `json:"changed_docs"`
	UpgradeNote   bool     `json:"upgrade_note"`
	ChangedFiles  []string `json:"changed_files,omitempty"`
	TestsPassed   int      `json:"tests_passed"`
	TestsFailed   int      `json:"tests_failed"`
	TwinsFixed    int      `json:"twins_fixed"`
	TwinsTotal    int      `json:"twins_total"`
	TwinsNotRun   int      `json:"twins_not_run,omitempty"` // declared by the graders, never reported
	Violations    []string `json:"violations,omitempty"`    // rule@file found in files the agent changed
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
	// Made before the cleanup is scheduled: a directory this run did not
	// make, kept from an earlier one, is refused and left as it is.
	if err := makeCellDir(dir); err != nil {
		fail(err)
		return
	}
	if !cfg.keep {
		// A cell is a full copy of the repository: left behind, a run of
		// a few dozen fills the disk.
		defer os.RemoveAll(dir)
	}
	start, err := prepare(cfg, c.task, c.Condition, dir)
	if err != nil {
		fail(err)
		return
	}
	c.start = start
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
	args := []string{
		"-p", prompt,
		"--model", cfg.model,
		"--output-format", "stream-json", "--verbose",
		"--permission-mode", "acceptEdits",
		"--allowedTools", "Read", "Edit", "Write", "Grep", "Glob", "Bash",
		"--setting-sources", "project",
		"--strict-mcp-config",
		"--no-session-persistence",
		"--max-budget-usd", fmt.Sprintf("%.2f", cfg.cellBudget),
	}
	args = append(args, "--settings", filepath.Join(dir, ".claude", "settings.json"))
	agent := command(ctx, dir, cfg.claude, args...)
	agent.Stdout = &stream
	agent.Stderr = &stream
	runErr := agent.Run()
	_, writeErr := transcript.Write(stream.Bytes())
	if err := errors.Join(writeErr, transcript.Close()); err != nil {
		fail(err)
		return
	}
	readTranscript(c, stream.Bytes())
	agentNote, fatal := agentFailure(runErr, ctx.Err() != nil, c)
	if fatal {
		fail(fmt.Errorf("agent: %w", runErr))
		return
	}
	if agentNote != "" {
		// Kept beside whatever grading says about the tree.
		defer func() { c.Note = strings.TrimPrefix(c.Note+"; "+agentNote, "; ") }()
	}

	// Inspection and grading get their own clock: an agent that used its
	// whole timeout still left a tree to measure.
	gradeCtx, cancelGrade := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancelGrade()
	changed, err := changedSince(gradeCtx, dir, c.start)
	if err != nil {
		fail(err)
		return
	}
	for _, file := range changed {
		c.ChangedFiles = append(c.ChangedFiles, file)
		switch {
		case strings.HasPrefix(file, "framework/docs/content/"):
			c.ChangedDocs = true
		case strings.HasPrefix(file, "internal/upgrade/releases/"):
			c.UpgradeNote = true
		}
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
	gradeTests(gradeCtx, cfg, c, dir)
	if c.task.Suite == suiteGuide {
		gradeRules(gradeCtx, cfg, c, dir, changed)
	}
}

// gradeRules scores a guide task on the rule it tempts: a contract
// finding of a tempted rule in a changed file, or a changed file of a
// forbidden kind, means the rule broke; a change that never made the
// required addition is unfixed. The tests' verdict stands underneath,
// and a cell the tests could not grade stays an error.
func gradeRules(ctx context.Context, cfg config, c *cell, dir string, changed []string) {
	if c.Outcome == outcomeError {
		return
	}
	broke := func(why string) {
		c.Outcome = outcomeBroke
		c.Note = strings.TrimPrefix(c.Note+"; "+why, "; ")
	}
	for _, file := range changed {
		for _, pattern := range c.task.Forbid {
			if ok, _ := path.Match(pattern, path.Base(file)); ok {
				c.Violations = append(c.Violations, "forbidden@"+file)
			}
		}
	}
	if len(c.task.Violations) > 0 {
		found, err := contractFindings(ctx, cfg, dir, c.start, c.task.Violations)
		if err != nil {
			// A break already seen is still a break; only a cell with
			// nothing against it becomes an error.
			if c.Outcome != outcomeBroke && len(c.Violations) == 0 {
				c.Outcome, c.Note = outcomeError, err.Error()
				return
			}
			c.Note = strings.TrimPrefix(c.Note+"; "+err.Error(), "; ")
		}
		c.Violations = append(c.Violations, found...)
	}
	if len(c.Violations) > 0 {
		broke("rule broken: " + strings.Join(c.Violations, ", "))
		return
	}
	if c.task.Requires != "" && c.Outcome == outcomeHeld && !addsText(dir, changed, c.task.RequiresIn, c.task.Requires) {
		c.Outcome = outcomeUnfixed
		c.Note = strings.TrimPrefix(c.Note+"; no change under "+c.task.RequiresIn+" adds "+c.task.Requires, "; ")
	}
}

// contractFindings runs gofastr verify over the cell and returns each
// finding of the named rules in a file changed since start, as rule@file.
// Untracked files are marked intent-to-add first so the diff sees them.
func contractFindings(ctx context.Context, cfg config, dir, start string, rules []string) ([]string, error) {
	if _, err := gitOutput(dir, "add", "-A", "-N"); err != nil {
		return nil, err
	}
	out, err := command(ctx, dir, cfg.verify, "verify", "--no-vet", "--json", "--changed="+start, "--rule", strings.Join(rules, ",")).Output()
	var report struct {
		Error       string `json:"error"`
		Diagnostics []struct {
			Rule string `json:"rule"`
			File string `json:"file"`
		} `json:"diagnostics"`
	}
	if jerr := json.Unmarshal(out, &report); jerr != nil {
		return nil, fmt.Errorf("gofastr verify: %w (%v)", jerr, err)
	}
	// verify exits 1 when it reports findings and 2 when it could not run;
	// a report that says it failed is not a clean tree.
	var exit *exec.ExitError
	if report.Error != "" || (errors.As(err, &exit) && exit.ExitCode() != 1) || (err != nil && !errors.As(err, &exit)) {
		return nil, fmt.Errorf("gofastr verify did not run: %s (%v)", report.Error, err)
	}
	seen := map[string]bool{}
	for _, d := range report.Diagnostics {
		seen[d.Rule+"@"+d.File] = true
	}
	return slices.Sorted(maps.Keys(seen)), nil
}

// addsText reports whether a changed file under prefix holds text.
func addsText(dir string, changed []string, prefix, text string) bool {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return false
	}
	defer root.Close()
	for _, file := range changed {
		if !strings.HasPrefix(file, prefix) || strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := root.ReadFile(file)
		if err == nil && bytes.Contains(src, []byte(text)) {
			return true
		}
	}
	return false
}

// agentFailure says what an agent that exited with an error and no answer
// means for its cell. An agent that ran out of time on a task graded by
// tests left a tree, and that tree is graded with a note. Anything else
// has nothing to grade: an agent that never ran would score as having
// left the code alone.
func agentFailure(runErr error, timedOut bool, c *cell) (note string, fatal bool) {
	if runErr == nil || c.Answer != "" {
		return "", false
	}
	if timedOut && len(c.task.Rubric) == 0 {
		return "the agent ran out of time; the tree it left was graded", false
	}
	return "", true
}

// restoreTests puts every test file back as the cell's first commit had
// it, not as HEAD has it: an agent that committed a changed test would
// otherwise keep it. A test file the agent added, committed or not, is
// removed.
func restoreTests(dir string, root *os.Root, start string) error {
	const tests = ":(glob)**/*_test.go"
	added, err := gitOutput(dir, "diff", "--name-only", "--diff-filter=A", start, "--", tests)
	if err != nil {
		return err
	}
	for _, file := range strings.Fields(added) {
		if err := root.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	for _, args := range [][]string{
		{"checkout", "-q", start, "--", tests},
		{"clean", "-q", "-f", "--", tests},
	} {
		if _, err := gitOutput(dir, args...); err != nil {
			return err
		}
	}
	return nil
}

// changedSince lists what differs between the cell's first commit and
// its working tree: what the agent committed, what it left uncommitted,
// and what it left untracked.
func changedSince(ctx context.Context, dir, start string) ([]string, error) {
	seen := map[string]bool{}
	for _, args := range [][]string{
		{"diff", "--name-only", "-z", start},
		{"ls-files", "--others", "--exclude-standard", "-z"},
	} {
		out, err := command(ctx, dir, "git", args...).Output()
		if err != nil {
			return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		for name := range strings.SplitSeq(string(out), "\x00") {
			if name != "" {
				seen[name] = true
			}
		}
	}
	return slices.Sorted(maps.Keys(seen)), nil
}

// makeCellDir makes the directory one cell runs in. Mkdir, not MkdirAll,
// for the cell itself: a directory kept from an earlier run holds that
// run's repository and files.
func makeCellDir(dir string) error {
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return fmt.Errorf("cell directory: %w (a cell kept from an earlier run? pass another -work)", err)
	}
	return nil
}

// prepare writes one condition's tree into dir as a fresh one-commit
// repository, so a cell cannot read this checkout's history, and returns
// that commit.
func prepare(cfg config, t task, condition, dir string) (start string, err error) {
	ref := cfg.head
	switch {
	case t.Ref != "":
		ref = t.Ref
	case condition == conditionComments, condition == conditionGuideLong:
		ref = cfg.base
	}
	ctx := context.Background()
	archive := command(ctx, cfg.repo, "git", "archive", "--format=tar", ref)
	extract := command(ctx, dir, "tar", "-x")
	pipe, err := archive.StdoutPipe()
	if err != nil {
		return "", err
	}
	extract.Stdin = pipe
	if err := extract.Start(); err != nil {
		return "", err
	}
	if err := errors.Join(archive.Run(), extract.Wait()); err != nil {
		return "", fmt.Errorf("extract %s: %w", ref, err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if err := isolate(root); err != nil {
		return "", err
	}
	if condition == conditionBare {
		if err := stripRegistry(root); err != nil {
			return "", err
		}
	}
	if t.Ref != "" && (condition == conditionRegistry || condition == conditionHook || condition == conditionHarness) {
		if err := overlayRegistry(ctx, cfg, t, dir, root); err != nil {
			return "", err
		}
	}
	settings, err := cellSettings(condition)
	if err != nil {
		return "", err
	}
	if err := errors.Join(root.MkdirAll(".claude", 0o700), root.WriteFile(".claude/settings.json", settings, 0o600)); err != nil {
		return "", err
	}
	// A grader file is held out: every cell starts from the -base copy and
	// meets the -head copy only when it is graded.
	for _, file := range t.GraderFiles {
		src, err := command(ctx, cfg.repo, "git", "show", cfg.base+":"+file).Output()
		if err != nil {
			return "", fmt.Errorf("grader file %s at %s: %w", file, cfg.base, err)
		}
		if err := root.WriteFile(file, src, 0o600); err != nil {
			return "", err
		}
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "-A"},
		{"-c", "user.name=eval", "-c", "user.email=eval@example.invalid", "commit", "-q", "-m", "start"},
	} {
		if _, err := gitOutput(dir, args...); err != nil {
			return "", err
		}
	}
	return gitOutput(dir, "rev-parse", "HEAD")
}

// overlayRegistry gives a replay cell, cut from a commit older than the
// registry, the registry's tooling from -head, the task's own entries and
// the pointer in the agent guide.
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
	base := path.Join("overlays", t.overlay())
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
	name := guideFile(root)
	guide, err := root.ReadFile(name)
	if err != nil {
		return err
	}
	const anchor = "## TL;DR"
	i := bytes.Index(guide, []byte(anchor))
	if i < 0 {
		return fmt.Errorf("%s at %s has no %q heading to put the registry pointer before", name, t.Ref, anchor)
	}
	return root.WriteFile(name, slices.Concat(guide[:i], []byte(registryPointer), guide[i:]), 0o600)
}

// guideFile names the file that holds a tree's agent guide: AGENTS.md
// where it exists, with CLAUDE.md only importing it, else CLAUDE.md.
func guideFile(root *os.Root) string {
	if _, err := root.Stat("AGENTS.md"); err == nil {
		return "AGENTS.md"
	}
	return "CLAUDE.md"
}

// isolate removes from a cell's tree what no condition may inherit from
// the commit it was cut from: the harness itself, which holds the prompts
// and the grading, and the project's Claude Code settings, whose hooks
// and deny rules would otherwise run in every condition. A condition
// that tests hooks writes its own settings afterwards.
func isolate(root *os.Root) error {
	for _, p := range []string{"evals/primitives", ".claude/settings.json", ".claude/settings.local.json"} {
		if err := root.RemoveAll(p); err != nil {
			return err
		}
	}
	return nil
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
	name := guideFile(root)
	guide, err := root.ReadFile(name)
	if err != nil {
		return err
	}
	loc := registryParagraph.FindIndex(guide)
	if loc == nil {
		return fmt.Errorf("%s holds no registry paragraph to strip: the bare condition would not be bare", name)
	}
	stripped := slices.Concat(guide[:loc[0]], guide[loc[1]:])
	if registryParagraph.Match(stripped) {
		return fmt.Errorf("%s still names the registry after one paragraph was removed", name)
	}
	// The guide links docs/contributing.md, and CLAUDE.md may only import
	// AGENTS.md: a pointer left in either would lead a bare cell back to
	// the command.
	type file struct {
		name string
		src  []byte
	}
	check := []file{{name, stripped}, {"docs/contributing.md", nil}}
	if name != "CLAUDE.md" {
		check = append(check, file{"CLAUDE.md", nil})
	}
	for _, guide := range check {
		if guide.src == nil {
			var err error
			if guide.src, err = root.ReadFile(guide.name); errors.Is(err, fs.ErrNotExist) {
				continue
			} else if err != nil {
				return err
			}
		}
		if bytes.Contains(guide.src, []byte("cmd/primitives")) {
			return fmt.Errorf("%s still names cmd/primitives: the bare condition would not be bare", guide.name)
		}
	}
	return root.WriteFile(name, stripped, 0o600)
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
	root, err := os.OpenRoot(dir)
	if err != nil {
		fail(err)
		return
	}
	defer root.Close()
	if err := restoreTests(dir, root, c.start); err != nil {
		fail(err)
		return
	}
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

var (
	replayResult = regexp.MustCompile(`(?m)^--- (PASS|FAIL): TestReplay(Reported|Twin)\w*`)
	replayTest   = regexp.MustCompile(`(?m)^func TestReplay(Reported|Twin)\w*\(`)
)

// gradeReplay copies a replay task's held-out tests into the cell and
// scores the reported path and each twin path separately.
func gradeReplay(ctx context.Context, c *cell, dir string, root *os.Root) {
	fail := func(err error) { c.Outcome, c.Note = outcomeError, err.Error() }
	base := path.Join("graders", c.task.Graders)
	// What the grader files declare is what must report. Counting result
	// lines alone would let a package that no longer builds drop its
	// tests from the total.
	wantReported, wantTwins := 0, 0
	err := fs.WalkDir(graders, base, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		src, err := graders.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range replayTest.FindAllSubmatch(src, -1) {
			if string(m[1]) == "Reported" {
				wantReported++
			} else {
				wantTwins++
			}
		}
		return root.WriteFile(strings.TrimSuffix(strings.TrimPrefix(p, base+"/"), ".txt"), src, 0o600)
	})
	if err != nil {
		fail(err)
		return
	}
	args := append([]string{"test", "-count=1", "-v", "-run", "^TestReplay(Reported|Twin)"}, c.task.Packages...)
	out, runErr := command(ctx, dir, "go", args...).CombinedOutput()
	scoreReplay(c, out, runErr, wantReported, wantTwins)
}

// scoreReplay reads the graders' output against the number of tests the
// grader files declare. A twin test that never reported, because its
// package no longer builds, counts as a twin left unfixed.
func scoreReplay(c *cell, out []byte, runErr error, wantReported, wantTwins int) {
	reported, reportedPassed, twinsRan := 0, 0, 0
	for _, m := range replayResult.FindAllSubmatch(out, -1) {
		passed := string(m[1]) == "PASS"
		if string(m[2]) == "Reported" {
			reported++
			if passed {
				reportedPassed++
			}
			continue
		}
		twinsRan++
		if passed {
			c.TwinsFixed++
		}
	}
	c.TwinsTotal = wantTwins
	c.TwinsNotRun = max(wantTwins-twinsRan, 0)
	note := fmt.Sprintf("twins %d/%d", c.TwinsFixed, c.TwinsTotal)
	if c.TwinsNotRun > 0 {
		note += fmt.Sprintf(", %d did not run: %s", c.TwinsNotRun, lastLine(out))
	}
	switch {
	case reported < wantReported || reported == 0:
		c.Outcome, c.Note = outcomeBroke, fmt.Sprintf("the reported grader did not run (%v); %s", runErr, note)
	case reportedPassed == reported:
		c.Outcome, c.Note = outcomeFixed, note
	default:
		c.Outcome, c.Note = outcomeUnfixed, note
	}
}

func lastLine(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return lines[len(lines)-1]
}

func renderMarkdown(r report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Primitives eval\n\nModel `%s`, comments condition at `%s`, registry and bare at `%s`. Spent $%.2f.\n\n", r.Model, r.Base, r.Head, r.SpentUSD)
	b.WriteString("| Task | Condition | Run | Outcome | Tests pass/fail | Twins fixed | Rubric | Changed source | Changed tests | Changed docs | Upgrade note | Registry calls | Tool calls | Cost |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, c := range r.Cells {
		rubric := ""
		if total := len(c.RubricHit) + len(c.RubricMissed); total > 0 {
			rubric = fmt.Sprintf("%d/%d", len(c.RubricHit), total)
		}
		twins := ""
		if c.TwinsTotal > 0 {
			twins = fmt.Sprintf("%d/%d", c.TwinsFixed, c.TwinsTotal)
		}
		if c.TwinsNotRun > 0 {
			twins += fmt.Sprintf(" (%d did not run)", c.TwinsNotRun)
		}
		fmt.Fprintf(&b, "| %s | %s | %d | %s | %d/%d | %s | %s | %t | %t | %t | %t | %d | %d | $%.2f |\n",
			c.Task, c.Condition, c.Run, c.Outcome, c.TestsPassed, c.TestsFailed, twins, rubric, c.ChangedSource, c.ChangedTests, c.ChangedDocs, c.UpgradeNote, c.RegistryCalls, c.ToolCalls, c.CostUSD)
	}
	return b.String()
}
