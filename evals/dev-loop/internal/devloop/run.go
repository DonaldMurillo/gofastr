// Package devloop measures whether a coding agent, dropped into a fresh
// GoFastr scaffold with an edit-and-check task, develops under
// `gofastr dev` (rebuild on save, browser reload) or falls back to
// `go run .` / a hand-launched binary and restarts after every edit.
//
// The prompt names no command. Whatever loop the agent picks, it found
// in the scaffold's own guidance, so a failing trial is a guidance gap.
package devloop

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/evals/internal/childenv"
	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

//go:embed task.md
var taskMarkdown string

// renderTask fills the task's browser URL with the trial's address.
func renderTask(addr string) string {
	return strings.ReplaceAll(taskMarkdown, "{{ADDR}}", addr)
}

const module = "eval.local/devloop"

// builderPrompt points the agent at the scaffold's guidance and the task.
// It deliberately names no command: discovering the dev loop is the
// thing under test.
const builderPrompt = `Read AGENTS.md, CLAUDE.md and EVAL_TASK.md in this directory, then do
the task in EVAL_TASK.md. Work only inside this directory.`

// Options configures a run.
type Options struct {
	RepoRoot  string
	RunDir    string
	Runs      int
	Model     string
	ClaudeBin string
	Timeout   time.Duration
}

// Trial is one agent run and its grade.
type Trial struct {
	Index    int      `json:"index"`
	Signals  Signals  `json:"signals"`
	Verdict  Verdict  `json:"verdict"`
	CostUSD  float64  `json:"cost_usd"`
	Duration float64  `json:"duration_seconds"`
	Issues   []string `json:"technical_issues,omitempty"`
	Final    string   `json:"final_message,omitempty"`
}

// Report is the aggregate written to results.json and RESULTS.md.
type Report struct {
	Model     string  `json:"model"`
	Trials    []Trial `json:"trials"`
	Passed    int     `json:"passed"`
	TasksDone int     `json:"tasks_done"`
}

// Run scaffolds, drives and grades opts.Runs independent trials.
func Run(ctx context.Context, opts Options) (Report, error) {
	if !supported {
		return Report{}, errors.New("the dev-loop eval needs a unix host (its PATH shims are sh scripts)")
	}
	realGo, err := exec.LookPath("go")
	if err != nil {
		return Report{}, err
	}
	if err := os.MkdirAll(opts.RunDir, 0o700); err != nil {
		return Report{}, err
	}
	gofastrBin := filepath.Join(opts.RunDir, "bin", "gofastr")
	if err := runQuiet(ctx, opts.RepoRoot, filepath.Join(opts.RunDir, "build-gofastr.log"), nil, realGo, "build", "-o", gofastrBin, "./cmd/gofastr"); err != nil {
		return Report{}, fmt.Errorf("build gofastr: %w", err)
	}
	report := Report{Model: opts.Model}
	for i := 1; i <= opts.Runs; i++ {
		t := runTrial(ctx, opts, i, gofastrBin, realGo)
		report.Trials = append(report.Trials, t)
		if err := writeJSON(filepath.Join(trialDir(opts.RunDir, i), "grade.json"), t); err != nil {
			return report, err
		}
	}
	err = writeReport(opts.RunDir, &report)
	return report, err
}

// Regrade re-runs grading over an existing run directory without
// launching agents.
func Regrade(ctx context.Context, runDir string) (Report, error) {
	var prior Report
	if err := readJSON(filepath.Join(runDir, "results.json"), &prior); err != nil {
		return Report{}, err
	}
	realGo, err := exec.LookPath("go")
	if err != nil {
		return Report{}, err
	}
	report := Report{Model: prior.Model}
	for _, p := range prior.Trials {
		t := gradeTrial(ctx, trialDir(runDir, p.Index), p.Index, realGo)
		t.Duration = p.Duration
		t.Issues = append(t.Issues, p.Issues...)
		report.Trials = append(report.Trials, t)
	}
	err = writeReport(runDir, &report)
	return report, err
}

func trialDir(runDir string, i int) string {
	return filepath.Join(runDir, fmt.Sprintf("trial-%02d", i))
}

func runTrial(ctx context.Context, opts Options, i int, gofastrBin, realGo string) Trial {
	dir := trialDir(opts.RunDir, i)
	fail := func(step string, err error) Trial {
		return Trial{Index: i, Issues: []string{step + ": " + err.Error()}, Verdict: Verdict{Failures: []string{"technical: " + step}}}
	}
	// Reset through an os.Root so a symlink an earlier trial's agent left
	// in the run dir cannot redirect the removal outside it.
	root, err := os.OpenRoot(opts.RunDir)
	if err != nil {
		return fail("open run dir", err)
	}
	defer root.Close()
	name := filepath.Base(dir)
	if err := root.RemoveAll(name); err != nil {
		return fail("reset trial dir", err)
	}
	if err := root.Mkdir(name, 0o700); err != nil {
		return fail("create trial dir", err)
	}
	workspace := filepath.Join(dir, "app")
	modEnv := []string{"GOWORK=off"}
	steps := []struct {
		name, cwd string
		env       []string
		argv      []string
	}{
		{"scaffold", dir, nil, []string{gofastrBin, "init", "app", "--module=" + module, "--no-entity"}},
		{"pin framework", workspace, modEnv, []string{realGo, "mod", "edit", "-require=github.com/DonaldMurillo/gofastr@v0.0.0", "-replace=github.com/DonaldMurillo/gofastr=" + opts.RepoRoot}},
		{"go mod tidy", workspace, modEnv, []string{realGo, "mod", "tidy"}},
	}
	for _, s := range steps {
		if err := runQuiet(ctx, s.cwd, filepath.Join(dir, "setup.log"), s.env, s.argv[0], s.argv[1:]...); err != nil {
			return fail(s.name, err)
		}
	}
	// Each trial serves on its own port, named in the task, so the agent
	// never has to hunt for its server and never meets another process
	// that happens to hold :8080 on the operator's machine.
	port, err := freePort()
	if err != nil {
		return fail("pick trial port", err)
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	if err := fileperm.WriteOwnerOnly(filepath.Join(workspace, "EVAL_TASK.md"), []byte(renderTask(addr))); err != nil {
		return fail("write task", err)
	}
	shimDir := filepath.Join(dir, "shims")
	if err := installShims(shimDir, gofastrBin, realGo, filepath.Join(dir, "cli.log"), addr); err != nil {
		return fail("install shims", err)
	}

	started := time.Now()
	agentErr := runAgent(ctx, opts, workspace, shimDir, dir, addr)
	duration := time.Since(started).Seconds()

	t := gradeTrial(ctx, dir, i, realGo)
	t.Duration = duration
	if agentErr != nil {
		t.Issues = append(t.Issues, "agent: "+agentErr.Error())
	}
	return t
}

func runAgent(ctx context.Context, opts Options, workspace, shimDir, dir, addr string) error {
	transcript, err := os.OpenFile(filepath.Join(dir, "transcript.jsonl"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer transcript.Close()
	stderr, err := os.OpenFile(filepath.Join(dir, "agent.stderr"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer stderr.Close()

	runCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, opts.ClaudeBin,
		"-p", "--model", opts.Model,
		"--no-session-persistence", "--output-format", "stream-json", "--verbose",
		"--setting-sources", "project",
		"--permission-mode", "bypassPermissions",
		builderPrompt)
	cmd.Dir = workspace
	cmd.Env = agentEnv(shimDir, addr)
	cmd.Stdout, cmd.Stderr = transcript, stderr
	ownProcessGroup(cmd)
	err = cmd.Run()
	// A dev server the agent backgrounded and never stopped outlives the
	// agent; reap the whole group before the probe takes a port.
	killGroup(cmd)
	if runCtx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("timed out after %s", opts.Timeout)
	}
	return err
}

// agentEnv is the operator's environment minus credentials (the Claude
// key excepted), minus the variables that mark a nested Claude Code
// session, with the shims first on PATH. PORT carries the trial address,
// so a `go run .` bypass serves where the task says the app lives, and
// isolation is off so nothing remaps that port.
func agentEnv(shimDir, addr string) []string {
	var env []string
	for _, entry := range os.Environ() {
		name, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		upper := strings.ToUpper(name)
		switch {
		case upper == "ANTHROPIC_API_KEY":
		case childenv.LooksCredentialBearing(upper):
			continue
		case upper == "CLAUDECODE", strings.HasPrefix(upper, "CLAUDE_CODE_"), upper == callerEnv, upper == "GOWORK", upper == "GOFLAGS",
			upper == "PORT", strings.HasPrefix(upper, "GOFASTR_ISOLATION"):
			continue
		case upper == "PATH":
			entry = "PATH=" + shimDir + string(os.PathListSeparator) + value
		}
		env = append(env, entry)
	}
	env = append(env, "GOWORK=off", "GOFLAGS=-buildvcs=false", "PORT="+addr, "GOFASTR_ISOLATION=off")
	sort.Strings(env)
	return env
}

func gradeTrial(ctx context.Context, dir string, i int, realGo string) Trial {
	t := Trial{Index: i}
	workspace := filepath.Join(dir, "app")
	calls, err := readShimLog(filepath.Join(dir, "cli.log"))
	if err != nil {
		t.Issues = append(t.Issues, "read shim log: "+err.Error())
	}
	summary, err := readTranscript(filepath.Join(dir, "transcript.jsonl"))
	if err != nil {
		t.Issues = append(t.Issues, "read transcript: "+err.Error())
	}
	t.Signals = collectSignals(calls, summary.Events, workspace, module)
	t.CostUSD, t.Final = summary.CostUSD, summary.Final
	t.Verdict = judge(t.Signals, probeTask(ctx, dir, workspace, realGo))
	if len(t.Issues) > 0 {
		t.Verdict.Pass, t.Verdict.TaskDone = false, false
	}
	return t
}

// probeTask builds the finished workspace with the real toolchain, boots
// it on a free port, and checks the three requested changes over HTTP.
func probeTask(ctx context.Context, dir, workspace, realGo string) []string {
	bin := filepath.Join(dir, "probe-bin")
	if err := runQuiet(ctx, workspace, filepath.Join(dir, "probe-build.log"), []string{"GOWORK=off"}, realGo, "build", "-o", bin, "."); err != nil {
		return []string{"task: app does not build: " + err.Error()}
	}
	port, err := freePort()
	if err != nil {
		return []string{"task: no free port: " + err.Error()}
	}
	logFile, err := os.OpenFile(filepath.Join(dir, "probe-server.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return []string{"task: " + err.Error()}
	}
	defer logFile.Close()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	server := exec.CommandContext(ctx, bin)
	server.Dir = workspace
	server.Env = append(childenv.Allowlisted(), fmt.Sprintf("PORT=127.0.0.1:%d", port), "GOFASTR_ISOLATION=0")
	server.Stdout, server.Stderr = logFile, logFile
	ownProcessGroup(server)
	if err := server.Start(); err != nil {
		return []string{"task: app does not start: " + err.Error()}
	}
	defer func() {
		killGroup(server)
		_ = server.Wait()
	}()

	client := &http.Client{Timeout: 5 * time.Second}
	get := func(path string) (int, string) {
		resp, err := client.Get(base + path)
		if err != nil {
			return 0, ""
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		return resp.StatusCode, string(body)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		if code, _ := get("/"); code == http.StatusOK {
			break
		}
		if time.Now().After(deadline) {
			return []string{"task: app never served / on the probe port"}
		}
		time.Sleep(250 * time.Millisecond)
	}

	var failures []string
	_, home := get("/")
	if !strings.Contains(home, "Harbor Supply") {
		failures = append(failures, `task: home page lacks the "Harbor Supply" heading`)
	}
	if !strings.Contains(home, `href="/about"`) {
		failures = append(failures, "task: home page has no link to /about")
	}
	code, about := get("/about")
	if code != http.StatusOK || !strings.Contains(about, "About Harbor Supply") {
		failures = append(failures, fmt.Sprintf(`task: /about returned %d without "About Harbor Supply"`, code))
	}
	return failures
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// runQuiet runs a setup command with output appended to logPath.
func runQuiet(ctx context.Context, cwd, logPath string, env []string, program string, args ...string) error {
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintf(f, "$ %s %s\n", program, strings.Join(args, " "))
	cmdCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, program, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout, cmd.Stderr = f, f
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w (log: %s)", err, logPath)
	}
	return nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return fileperm.WriteOwnerOnly(path, append(data, '\n'))
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
