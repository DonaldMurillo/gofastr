package evalrunner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const ginVersion = "v1.11.0"

// Pinned modules the non-GoFastr lanes get in go.mod. Shared with the
// resolvability test: a version that stops resolving (as happened when the
// sqlite driver swap carried the old mattn version onto a path that isn't a
// module) makes every baseline lane, and so the whole eval, unrunnable.
const (
	sqliteRequirement = "modernc.org/sqlite@v1.55.0"
	cryptoRequirement = "golang.org/x/crypto@v0.52.0"
)

// baselineRequirements are every module@version a baseline lane writes into
// its workspace go.mod.
func baselineRequirements() []string {
	return []string{
		"github.com/gin-gonic/gin@" + ginVersion,
		sqliteRequirement,
		cryptoRequirement,
	}
}

type Config struct {
	RepoRoot    string
	ArtifactDir string
	Codex       string
	CodexHome   string
	Model       string
	Runs        int
	Concurrency int
	Timeout     time.Duration
	Frameworks  []string
}

func Run(ctx context.Context, cfg Config) (*Aggregate, error) {
	if err := normalizeConfig(&cfg); err != nil {
		return nil, err
	}
	version, err := commandVersion(ctx, cfg.Codex)
	if err != nil {
		return nil, err
	}
	runID := time.Now().UTC().Format("20060102T150405Z")
	runDir := filepath.Join(cfg.ArtifactDir, runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, err
	}
	cliPath := filepath.Join(runDir, "tools", executableName("gofastr"))
	if contains(cfg.Frameworks, "gofastr") {
		if err := os.MkdirAll(filepath.Dir(cliPath), 0o755); err != nil {
			return nil, err
		}
		if output, buildErr := commandOutput(ctx, cfg.RepoRoot, "go", "build", "-o", cliPath, "./cmd/gofastr"); buildErr != nil {
			return nil, fmt.Errorf("build GoFastr CLI: %w\n%s", buildErr, output)
		}
	}

	aggregate := &Aggregate{
		RunID: runID, StartedAt: time.Now(), CodexVersion: version,
		Model: displayModel(cfg.Model), Runs: cfg.Runs,
	}
	type job struct {
		framework  string
		repetition int
	}
	jobs := make(chan job)
	results := make(chan TrialResult)
	errs := make(chan error, cfg.Concurrency)
	var workers sync.WaitGroup
	for range cfg.Concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for current := range jobs {
				result, runErr := runTrial(ctx, cfg, runDir, cliPath, version,
					current.framework, current.repetition)
				if runErr != nil {
					errs <- runErr
					continue
				}
				results <- result
			}
		}()
	}
	go func() {
		for repetition := 1; repetition <= cfg.Runs; repetition++ {
			for _, framework := range cfg.Frameworks {
				jobs <- job{framework: framework, repetition: repetition}
			}
		}
		close(jobs)
		workers.Wait()
		close(results)
		close(errs)
	}()

	for result := range results {
		aggregate.Trials = append(aggregate.Trials, result)
		_ = writeJSON(filepath.Join(runDir, "results.partial.json"), aggregate)
	}
	var runErrors []string
	for runErr := range errs {
		runErrors = append(runErrors, runErr.Error())
	}
	sort.Slice(aggregate.Trials, func(i, j int) bool {
		if aggregate.Trials[i].Repetition != aggregate.Trials[j].Repetition {
			return aggregate.Trials[i].Repetition < aggregate.Trials[j].Repetition
		}
		return aggregate.Trials[i].Framework < aggregate.Trials[j].Framework
	})
	aggregate.CompletedAt = time.Now()
	if err := writeJSON(filepath.Join(runDir, "results.json"), aggregate); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(runDir, "RESULTS.md"), []byte(RenderMarkdown(aggregate)), 0o644); err != nil {
		return nil, err
	}
	if len(runErrors) > 0 {
		return aggregate, fmt.Errorf("%d trial(s) failed before producing results: %s",
			len(runErrors), strings.Join(runErrors, "; "))
	}
	return aggregate, nil
}

// RegradeMaintenance reruns deterministic maintenance probes against the
// already-built workspaces in a completed run without spending more agent
// tokens. Agent telemetry from the original run is preserved.
func RegradeMaintenance(ctx context.Context, runDir string) (*Aggregate, error) {
	data, err := os.ReadFile(filepath.Join(runDir, "results.json"))
	if err != nil {
		return nil, err
	}
	var aggregate Aggregate
	if err := json.Unmarshal(data, &aggregate); err != nil {
		return nil, fmt.Errorf("parse results: %w", err)
	}
	for index := range aggregate.Trials {
		trial := &aggregate.Trials[index]
		if trial.Maintenance == nil {
			continue
		}
		cellDir := filepath.Dir(trial.Workspace)
		original := *trial.Maintenance
		regraded := Grade(ctx, trial.Workspace, cellDir, true)
		regraded.AgentOK = original.AgentOK
		regraded.AgentError = original.AgentError
		regraded.Duration = original.Duration
		regraded.Tokens = original.Tokens
		trial.Maintenance = &regraded
		if err := writeJSON(filepath.Join(cellDir, "maintenance-grade.json"), regraded); err != nil {
			return nil, err
		}
		if err := writeJSON(filepath.Join(cellDir, "trial.json"), trial); err != nil {
			return nil, err
		}
	}
	aggregate.CompletedAt = time.Now()
	if err := writeJSON(filepath.Join(runDir, "results.json"), &aggregate); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(runDir, "RESULTS.md"), []byte(RenderMarkdown(&aggregate)), 0o644); err != nil {
		return nil, err
	}
	return &aggregate, nil
}

func runTrial(ctx context.Context, cfg Config, runDir, cliPath, codexVersion, framework string, repetition int) (TrialResult, error) {
	id := fmt.Sprintf("%s-run-%d", framework, repetition)
	cellDir := filepath.Join(runDir, "trials", id)
	workspace := filepath.Join(cellDir, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return TrialResult{}, err
	}
	if err := setupWorkspace(ctx, cfg, framework, workspace, cliPath); err != nil {
		return TrialResult{}, fmt.Errorf("%s setup: %w", id, err)
	}
	if err := copyFile(filepath.Join(cfg.RepoRoot, "evals", "backend-adoption", "TASK.md"),
		filepath.Join(workspace, "EVAL_TASK.md")); err != nil {
		return TrialResult{}, err
	}

	trial := TrialResult{
		ID: id, Framework: framework, Repetition: repetition, Workspace: workspace,
		CodexVersion: codexVersion, Model: displayModel(cfg.Model),
	}
	initialLog := filepath.Join(cellDir, "initial-builder.log")
	trial.BuilderLog = initialLog
	started := time.Now()
	agentErr := runCodex(ctx, cfg, workspace, initialLog,
		filepath.Join(cellDir, "initial-final.md"), framework, false)
	initial := Grade(ctx, workspace, cellDir, false)
	initial.Duration = time.Since(started).Seconds()
	initial.AgentOK = agentErr == nil
	if agentErr != nil {
		initial.AgentError = agentErr.Error()
	}
	if log, err := os.ReadFile(initialLog); err == nil {
		initial.Tokens = ParseTokenUsage(log)
	}
	trial.Initial = initial
	if err := writeJSON(filepath.Join(cellDir, "initial-grade.json"), initial); err != nil {
		return TrialResult{}, err
	}

	if initial.BuildOK {
		if err := copyFile(filepath.Join(cfg.RepoRoot, "evals", "backend-adoption", "MAINTENANCE_TASK.md"),
			filepath.Join(workspace, "EVAL_TASK.md")); err != nil {
			return TrialResult{}, err
		}
		maintenanceLog := filepath.Join(cellDir, "maintenance-builder.log")
		maintenanceStarted := time.Now()
		maintenanceErr := runCodex(ctx, cfg, workspace, maintenanceLog,
			filepath.Join(cellDir, "maintenance-final.md"), framework, true)
		maintenance := Grade(ctx, workspace, cellDir, true)
		maintenance.Duration = time.Since(maintenanceStarted).Seconds()
		maintenance.AgentOK = maintenanceErr == nil
		if maintenanceErr != nil {
			maintenance.AgentError = maintenanceErr.Error()
		}
		if log, err := os.ReadFile(maintenanceLog); err == nil {
			maintenance.Tokens = ParseTokenUsage(log)
		}
		trial.Maintenance = &maintenance
		if err := writeJSON(filepath.Join(cellDir, "maintenance-grade.json"), maintenance); err != nil {
			return TrialResult{}, err
		}
	}
	if err := writeJSON(filepath.Join(cellDir, "trial.json"), trial); err != nil {
		return TrialResult{}, err
	}
	// Candidate-local build caches are reproducible but hundreds of megabytes
	// per cell. Preserve source, logs, binaries, databases, and grades while
	// keeping completed benchmark artifacts practical to retain.
	if err := os.RemoveAll(filepath.Join(workspace, ".cache")); err != nil {
		return TrialResult{}, fmt.Errorf("remove generated candidate cache: %w", err)
	}
	return trial, nil
}

func setupWorkspace(ctx context.Context, cfg Config, framework, workspace, cliPath string) error {
	module := "eval.local/" + strings.ReplaceAll(filepath.Base(workspace), "_", "-")
	switch framework {
	case "gofastr":
		output, err := commandOutput(ctx, workspace, cliPath, "init", ".", "--module="+module, "--no-entity")
		if err != nil {
			return fmt.Errorf("gofastr init: %w\n%s", err, output)
		}
		if output, err = commandOutput(ctx, workspace, "go", "mod", "edit",
			"-require=github.com/DonaldMurillo/gofastr@v0.0.0",
			"-replace=github.com/DonaldMurillo/gofastr="+cfg.RepoRoot); err != nil {
			return fmt.Errorf("wire local GoFastr: %w\n%s", err, output)
		}
		if output, err = commandOutput(ctx, workspace, "go", "mod", "tidy"); err != nil {
			return fmt.Errorf("resolve GoFastr scaffold: %w\n%s", err, output)
		}
		frameworkText := `# Framework constraint

Use GoFastr from the local replace directive. The generated project guidance,
CLI, embedded docs, framework declarations, auth battery, entity CRUD, OpenAPI,
MCP, and UI packages are all available. You may edit or replace scaffolded app
files as needed, but do not replace GoFastr with another web framework.
`
		return os.WriteFile(filepath.Join(workspace, "FRAMEWORK.md"), []byte(frameworkText), 0o644)
	case "gin":
		if output, err := commandOutput(ctx, workspace, "go", "mod", "init", module); err != nil {
			return fmt.Errorf("go mod init: %w\n%s", err, output)
		}
		if output, err := commandOutput(ctx, workspace, "go", "mod", "edit",
			"-require=github.com/gin-gonic/gin@"+ginVersion,
			"-require="+sqliteRequirement,
			"-require="+cryptoRequirement); err != nil {
			return fmt.Errorf("add Gin requirement: %w\n%s", err, output)
		}
		if err := writeNeutralGuidance(workspace); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(workspace, "FRAMEWORK.md"), []byte(`# Framework constraint

Use Gin (`+"`github.com/gin-gonic/gin`"+`) as the HTTP router. You may use Go
standard-library packages and focused libraries for SQLite and password
hashing. Do not replace Gin with another web framework.
`), 0o644)
	case "stdlib":
		if output, err := commandOutput(ctx, workspace, "go", "mod", "init", module); err != nil {
			return fmt.Errorf("go mod init: %w\n%s", err, output)
		}
		if output, err := commandOutput(ctx, workspace, "go", "mod", "edit",
			"-require="+sqliteRequirement,
			"-require="+cryptoRequirement); err != nil {
			return fmt.Errorf("add focused requirements: %w\n%s", err, output)
		}
		if err := writeNeutralGuidance(workspace); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(workspace, "FRAMEWORK.md"), []byte(`# Framework constraint

Use the Go standard library's `+"`net/http`"+` router and handlers. Focused
libraries for SQLite and password hashing are allowed. Do not add a Go web
framework.
`), 0o644)
	default:
		return fmt.Errorf("unsupported framework %q", framework)
	}
}

func writeNeutralGuidance(workspace string) error {
	return os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte(`# Evaluation workspace

Read `+"`FRAMEWORK.md`"+` and `+"`EVAL_TASK.md`"+` completely before coding.
Keep the app local and self-contained. Do not inspect parent directories,
sibling workspaces, grader files, or other candidates. Use idiomatic Go,
parameterized SQL, and focused tests. Run gofmt, go test ./..., and go build
./... before finishing.
`), 0o644)
}

func runCodex(ctx context.Context, cfg Config, workspace, logPath, finalPath, framework string, maintenance bool) error {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return err
	}
	logFile, err := os.Create(logPath)
	if err != nil {
		return err
	}
	defer logFile.Close()
	args := []string{
		"--ask-for-approval", "never",
		"--sandbox", "workspace-write",
		"exec", "--ephemeral", "--skip-git-repo-check",
		"--cd", workspace, "--output-last-message", finalPath,
	}
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}
	args = append(args, "-")
	runCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, cfg.Codex, args...)
	configureCommandCancellation(cmd)
	cmd.Dir = workspace
	cmd.Env = codexEnvironment(
		cfg.CodexHome,
		filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(logPath))), "tools"),
		workspace,
	)
	cmd.Stdin = strings.NewReader(agentPrompt(framework, maintenance))
	cmd.Stdout, cmd.Stderr = logFile, logFile
	err = cmd.Run()
	if runCtx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("Codex timed out after %s", cfg.Timeout)
	}
	if err != nil {
		return fmt.Errorf("Codex failed: %w", err)
	}
	return nil
}

func agentPrompt(framework string, maintenance bool) string {
	phase := "cold-start implementation"
	if maintenance {
		phase = "maintenance change"
	}
	return fmt.Sprintf(`You are the sole implementation agent for a framework adoption evaluation.

This is the %s phase using %s. Read AGENTS.md, FRAMEWORK.md, and EVAL_TASK.md
completely, then implement every requirement in the current workspace.

Integrity rules:
- Do not inspect parent directories, sibling candidates, evaluator source,
  hidden rubrics, or prior candidates.
- Do not use external services, API keys, containers, or a network dependency
  at runtime.
- You may use the Go dependencies already declared or locally available.
- Run gofmt, go test ./..., and go build ./..., fixing failures.
- Stop any local servers before finishing.
- Make the workspace runnable; do not merely describe code.

Work within a %s budget and return a concise final summary.
`, phase, framework, cfgDurationText(25*time.Minute))
}

func cfgDurationText(duration time.Duration) string {
	if duration%time.Minute == 0 {
		return fmt.Sprintf("%d-minute", int(duration/time.Minute))
	}
	return duration.String()
}

func codexEnvironment(codexHome, toolDir, workspace string) []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		upper := strings.ToUpper(name)
		if strings.HasPrefix(upper, "CODEX_") || upper == "PATH" || upper == "GOCACHE" ||
			upper == "GOFLAGS" || looksCredentialBearing(upper) {
			continue
		}
		env = append(env, entry)
	}
	env = append(env,
		"CODEX_HOME="+codexHome,
		"GOCACHE="+filepath.Join(workspace, ".cache", "go-build"),
		"GOFLAGS=-buildvcs=false",
		"PATH="+toolDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	sort.Strings(env)
	return env
}

func looksCredentialBearing(name string) bool {
	if name == "SSH_AUTH_SOCK" || name == "GPG_AGENT_INFO" || name == "DATABASE_URL" {
		return true
	}
	for _, prefix := range []string{"AWS_", "AZURE_", "GCP_", "GOOGLE_", "GH_", "GITHUB_", "NPM_", "DOCKER_", "SLACK_", "STRIPE_"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	for _, fragment := range []string{"API_KEY", "ACCESS_KEY", "AUTH_TOKEN", "CREDENTIAL", "PASSWORD", "PRIVATE_KEY", "SECRET", "SIGNING_KEY"} {
		if strings.Contains(name, fragment) {
			return true
		}
	}
	return false
}

func normalizeConfig(cfg *Config) error {
	if cfg.RepoRoot == "" {
		return errors.New("repo root is required")
	}
	var err error
	cfg.RepoRoot, err = filepath.Abs(cfg.RepoRoot)
	if err != nil {
		return err
	}
	if cfg.ArtifactDir == "" {
		cfg.ArtifactDir = filepath.Join(cfg.RepoRoot, "dist", "backend-adoption")
	}
	if cfg.Codex == "" {
		cfg.Codex = "codex"
	}
	if cfg.CodexHome == "" {
		if configured := os.Getenv("CODEX_HOME"); configured != "" {
			cfg.CodexHome = configured
		} else {
			home, homeErr := os.UserHomeDir()
			if homeErr != nil {
				return homeErr
			}
			cfg.CodexHome = filepath.Join(home, ".codex")
		}
	}
	if _, err := os.Stat(filepath.Join(cfg.CodexHome, "auth.json")); err != nil {
		return fmt.Errorf("Codex authentication missing: %w", err)
	}
	if cfg.Runs <= 0 {
		cfg.Runs = 2
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 2
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 25 * time.Minute
	}
	if len(cfg.Frameworks) == 0 {
		cfg.Frameworks = []string{"gofastr", "gin", "stdlib"}
	}
	seen := map[string]bool{}
	for _, framework := range cfg.Frameworks {
		if !contains([]string{"gofastr", "gin", "stdlib"}, framework) {
			return fmt.Errorf("unsupported framework %q", framework)
		}
		if seen[framework] {
			return fmt.Errorf("duplicate framework %q", framework)
		}
		seen[framework] = true
	}
	return nil
}

func commandVersion(ctx context.Context, program string) (string, error) {
	versionCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(versionCtx, program, "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("resolve Codex version: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func RenderMarkdown(aggregate *Aggregate) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Backend adoption results\n\n")
	fmt.Fprintf(&b, "- Run: `%s`\n", aggregate.RunID)
	fmt.Fprintf(&b, "- Codex: `%s`\n", aggregate.CodexVersion)
	fmt.Fprintf(&b, "- Model: `%s`\n", aggregate.Model)
	fmt.Fprintf(&b, "- Repetitions: %d\n\n", aggregate.Runs)
	b.WriteString("| Framework | Run | Initial score | Initial tokens | Initial time | Maintenance score | Maintenance tokens | Maintenance time | Source LOC after |\n")
	b.WriteString("|---|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, trial := range aggregate.Trials {
		maintenanceScore, maintenanceTokens, maintenanceTime := "—", "—", "—"
		sourceLines := trial.Initial.SourceLines
		if trial.Maintenance != nil {
			maintenanceScore = fmt.Sprintf("%d/%d", trial.Maintenance.Score, trial.Maintenance.Maximum)
			maintenanceTokens = fmt.Sprintf("%d", trial.Maintenance.Tokens)
			maintenanceTime = time.Duration(trial.Maintenance.Duration * float64(time.Second)).Round(time.Second).String()
			sourceLines = trial.Maintenance.SourceLines
		}
		fmt.Fprintf(&b, "| %s | %d | %d/%d | %d | %s | %s | %s | %s | %d |\n",
			trial.Framework, trial.Repetition, trial.Initial.Score, trial.Initial.Maximum,
			trial.Initial.Tokens,
			time.Duration(trial.Initial.Duration*float64(time.Second)).Round(time.Second),
			maintenanceScore, maintenanceTokens, maintenanceTime, sourceLines)
	}
	b.WriteString("\n## Failed checks\n\n")
	for _, trial := range aggregate.Trials {
		for _, phase := range []struct {
			name   string
			result *PhaseResult
		}{
			{"initial", &trial.Initial},
			{"maintenance", trial.Maintenance},
		} {
			if phase.result == nil {
				continue
			}
			for _, check := range phase.result.Checks {
				if !check.Passed {
					fmt.Fprintf(&b, "- `%s` %s: **%s** — %s\n",
						trial.ID, phase.name, check.ID, truncateText(check.Evidence, 220))
				}
			}
		}
	}
	return b.String()
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.Create(destination)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}

func displayModel(model string) string {
	if model == "" {
		return "Codex CLI default"
	}
	return model
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
