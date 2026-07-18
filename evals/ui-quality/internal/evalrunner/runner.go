package evalrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

func Run(ctx context.Context, suite *Suite, opts Options) (Summary, string, error) {
	if opts.RunsPerCell <= 0 {
		opts.RunsPerCell = 1
	}
	if opts.JudgeRuns <= 0 {
		opts.JudgeRuns = suite.Judge.Runs
	}
	if opts.MobileJudgeRuns <= 0 {
		opts.MobileJudgeRuns = suite.Judge.MobileRuns
	}
	if opts.RunID == "" {
		opts.RunID = time.Now().UTC().Format("20060102T150405.000000000Z")
	}
	applyAgentOptions(&suite.Agents.Builder, opts.BuilderBackend, opts.BuilderProgram, opts.BuilderPrefixArgs, opts.BuilderModel)
	applyAgentOptions(&suite.Agents.Judge, opts.JudgeBackend, opts.JudgeProgram, opts.JudgePrefixArgs, opts.JudgeModel)
	applyAgentOptions(&suite.Agents.MobileJudge, opts.MobileJudgeBackend, opts.MobileJudgeProgram, opts.MobileJudgePrefixArgs, opts.MobileJudgeModel)
	for _, role := range []struct {
		name string
		cfg  *AgentConfig
		mins int
	}{
		{"builder", &suite.Agents.Builder, 45},
		{"judge", &suite.Agents.Judge, 12},
		{"mobile_judge", &suite.Agents.MobileJudge, 12},
	} {
		if err := normalizeAgentConfig(role.name, role.cfg, role.mins); err != nil {
			return Summary{}, "", err
		}
	}
	variants, err := selectVariants(suite.Variants, opts.Variants)
	if err != nil {
		return Summary{}, "", err
	}
	scenarios, err := selectScenarios(suite.Scenarios, opts.Scenarios)
	if err != nil {
		return Summary{}, "", err
	}
	runDir, err := resolveRunDirectory(suite.ArtifactDir, opts.RunID)
	if err != nil {
		return Summary{}, "", err
	}
	if opts.DryRun && opts.ReuseWorkspaces {
		return Summary{}, runDir, errors.New("dry-run and reuse-workspaces cannot be combined")
	}
	if err := prepareRunDirectory(runDir, opts.ReuseWorkspaces); err != nil {
		return Summary{}, "", err
	}
	releaseRunLock, err := acquireRunLock(runDir)
	if err != nil {
		return Summary{}, runDir, err
	}
	defer releaseRunLock()
	if opts.ReuseWorkspaces {
		if err := invalidateReuseAggregates(runDir); err != nil {
			return Summary{}, runDir, err
		}
	}
	protocolSnapshotBytes, err := evaluatorProtocolSnapshot(suite, variants, scenarios, opts)
	if err != nil {
		return Summary{}, runDir, fmt.Errorf("snapshot evaluator protocol: %w", err)
	}
	protocolFingerprint, err := evaluatorProtocolFingerprint(suite, protocolSnapshotBytes)
	if err != nil {
		return Summary{}, runDir, fmt.Errorf("fingerprint evaluator protocol: %w", err)
	}
	protocolDir := filepath.Join(runDir, "protocol")
	if err := os.RemoveAll(protocolDir); err != nil {
		return Summary{}, runDir, fmt.Errorf("reset protocol snapshot: %w", err)
	}
	if err := os.MkdirAll(protocolDir, 0o755); err != nil {
		return Summary{}, runDir, err
	}
	if err := os.WriteFile(filepath.Join(protocolDir, "effective-suite.json"), append(protocolSnapshotBytes, '\n'), 0o644); err != nil {
		return Summary{}, runDir, err
	}
	if err := copyFile(suite.Judge.Rubric, filepath.Join(protocolDir, "rubric.md")); err != nil {
		return Summary{}, runDir, err
	}
	if err := copyFile(suite.Judge.Schema, filepath.Join(protocolDir, "judge.schema.json")); err != nil {
		return Summary{}, runDir, err
	}

	versions := make(map[string]string)
	resolveProvenance := func(cfg AgentConfig) (AgentProvenance, error) {
		version := "not-invoked-dry-run"
		key := cfg.Backend + "\x00" + cfg.Program + "\x00" + strings.Join(cfg.PrefixArgs, "\x00")
		if !opts.DryRun {
			var ok bool
			version, ok = versions[key]
			if !ok {
				var versionErr error
				version, versionErr = agentVersion(ctx, cfg)
				if versionErr != nil {
					return AgentProvenance{}, versionErr
				}
				versions[key] = version
			}
		}
		return AgentProvenance{Backend: cfg.Backend, Program: cfg.Program, Version: version, PrefixArgs: append([]string(nil), cfg.PrefixArgs...), Model: cfg.Model}, nil
	}
	builderProvenance, err := resolveProvenance(suite.Agents.Builder)
	if err != nil {
		return Summary{}, runDir, err
	}
	holisticJudgeProvenance, err := resolveProvenance(suite.Agents.Judge)
	if err != nil {
		return Summary{}, runDir, err
	}
	mobileJudgeProvenance, err := resolveProvenance(suite.Agents.MobileJudge)
	if err != nil {
		return Summary{}, runDir, err
	}
	manifest := RunManifest{
		Suite: suite.Name, RunID: opts.RunID, StartedAt: time.Now().UTC(), RepoRoot: suite.RepoRoot,
		ProtocolFingerprint:     protocolFingerprint,
		ReusedWorkspaces:        opts.ReuseWorkspaces,
		BuilderProvenance:       builderProvenance,
		HolisticJudgeProvenance: holisticJudgeProvenance, MobileJudgeProvenance: mobileJudgeProvenance,
	}
	for _, variant := range variants {
		manifest.Frameworks = append(manifest.Frameworks, FrameworkRecord{VariantID: variant.ID, Root: variant.FrameworkRoot})
	}
	if opts.DryRun {
		for _, variant := range variants {
			for _, scenario := range scenarios {
				for rep := 1; rep <= opts.RunsPerCell; rep++ {
					blindID := candidateID(opts.RunID, variant.ID, scenario.ID, rep)
					manifest.Candidates = append(manifest.Candidates, CandidateMapping{BlindID: blindID, VariantID: variant.ID, ScenarioID: scenario.ID, Repetition: rep, BuilderProvenance: builderProvenance, FrameworkRoot: variant.FrameworkRoot, HistoricalOnlyReason: variant.HistoricalOnlyReason})
				}
			}
		}
		if err := writeJSON(filepath.Join(runDir, "manifest.json"), manifest); err != nil {
			return Summary{}, runDir, err
		}
		return Summary{Suite: suite.Name, RunID: opts.RunID}, runDir, nil
	}
	codexHome := ""
	if suite.Agents.Builder.Backend == "codex" || suite.Agents.Judge.Backend == "codex" || suite.Agents.MobileJudge.Backend == "codex" {
		codexHome, err = resolveCodexHome(opts.CodexHomeSource)
		if err != nil {
			return Summary{}, runDir, err
		}
	}
	builderEnv := agentEnvironment(suite.Agents.Builder, codexHome)
	judgeEnv := agentEnvironment(suite.Agents.Judge, codexHome)
	mobileJudgeEnv := agentEnvironment(suite.Agents.MobileJudge, codexHome)

	toolsDir := filepath.Join(runDir, "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		return Summary{}, runDir, err
	}
	type frameworkTool struct {
		bin         string
		fingerprint string
	}
	frameworkTools := make(map[string]frameworkTool, len(variants))
	for i, variant := range variants {
		variantToolsDir := filepath.Join(toolsDir, variant.ID)
		if err := os.MkdirAll(variantToolsDir, 0o755); err != nil {
			return Summary{}, runDir, err
		}
		gofastrBin := filepath.Join(variantToolsDir, executableName("gofastr"))
		buildCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		err = runCommand(buildCtx, variant.FrameworkRoot, filepath.Join(variantToolsDir, "build-gofastr.log"), nil, "go", "build", "-o", gofastrBin, "./cmd/gofastr")
		cancel()
		if err != nil {
			return Summary{}, runDir, fmt.Errorf("build gofastr tool for %s: %w", variant.ID, err)
		}
		frameworkFingerprint, fingerprintErr := frameworkInputFingerprint(variant.FrameworkRoot, gofastrBin)
		if fingerprintErr != nil {
			return Summary{}, runDir, fmt.Errorf("fingerprint framework input for %s: %w", variant.ID, fingerprintErr)
		}
		frameworkTools[variant.ID] = frameworkTool{bin: gofastrBin, fingerprint: frameworkFingerprint}
		manifest.Frameworks[i].Fingerprint = frameworkFingerprint
	}

	rubricBytes, err := os.ReadFile(suite.Judge.Rubric)
	if err != nil {
		return Summary{}, runDir, err
	}
	var candidates []CandidateResult
	for _, variant := range variants {
		tool := frameworkTools[variant.ID]
		for _, scenario := range scenarios {
			for rep := 1; rep <= opts.RunsPerCell; rep++ {
				blindID := candidateID(opts.RunID, variant.ID, scenario.ID, rep)
				workspaceParent := filepath.Join(runDir, "workspaces", blindID)
				workspace := filepath.Join(workspaceParent, "app")
				resultDir := filepath.Join(runDir, "results", variant.ID, scenario.ID, fmt.Sprintf("run-%02d", rep))
				blindDir := filepath.Join(runDir, "blind", blindID)
				fingerprint, fingerprintErr := candidateBuilderFingerprint(tool.fingerprint, variant, scenario, builderProvenance)
				if fingerprintErr != nil {
					return Summary{}, runDir, fmt.Errorf("fingerprint candidate %s: %w", blindID, fingerprintErr)
				}
				mapping := CandidateMapping{BlindID: blindID, VariantID: variant.ID, ScenarioID: scenario.ID, Repetition: rep, Workspace: workspace, ResultDir: resultDir, BuilderFingerprint: fingerprint, BuilderProvenance: builderProvenance, FrameworkRoot: variant.FrameworkRoot, FrameworkFingerprint: tool.fingerprint, HistoricalOnlyReason: variant.HistoricalOnlyReason}
				candidate := executeCandidate(ctx, suite, opts, tool.bin, variant, scenario, rep, mapping, blindDir, string(rubricBytes), builderEnv, judgeEnv, mobileJudgeEnv)
				candidate.ProtocolFingerprint = protocolFingerprint
				candidate.HolisticJudgeProvenance = holisticJudgeProvenance
				candidate.MobileJudgeProvenance = mobileJudgeProvenance
				mapping.BuilderFingerprint = candidate.BuilderFingerprint
				mapping.WorkspaceFingerprint = candidate.WorkspaceFingerprint
				mapping.BuilderProvenance = candidate.BuilderProvenance
				manifest.Candidates = append(manifest.Candidates, mapping)
				candidates = append(candidates, candidate)
				if err := writeJSON(filepath.Join(resultDir, "result.json"), candidate); err != nil {
					return Summary{}, runDir, fmt.Errorf("write candidate %s result: %w", blindID, err)
				}
				if integrityErr := verifyFrameworkIntegrity(variant.FrameworkRoot, tool.bin, tool.fingerprint); integrityErr != nil {
					candidate.TechnicalIssues = append(candidate.TechnicalIssues, "framework integrity after candidate: "+integrityErr.Error())
					candidate.TechnicalPassed = false
					candidates[len(candidates)-1] = candidate
					if writeErr := writeJSON(filepath.Join(resultDir, "result.json"), candidate); writeErr != nil {
						return Summary{}, runDir, fmt.Errorf("candidate %s invalidated the framework and its result could not be updated: %v; integrity error: %w", blindID, writeErr, integrityErr)
					}
					_ = writeJSON(filepath.Join(runDir, "manifest.json"), manifest)
					return Summary{}, runDir, fmt.Errorf("candidate %s invalidated the framework under test: %w", blindID, integrityErr)
				}
			}
		}
	}
	if err := writeJSON(filepath.Join(runDir, "manifest.json"), manifest); err != nil {
		return Summary{}, runDir, err
	}
	competitiveReasons := competitionExclusions(suite, variants, scenarios, opts, candidates)
	summary := summarize(suite, opts.RunID, candidates, competitiveReasons)
	if err := writeJSON(filepath.Join(runDir, "summary.json"), summary); err != nil {
		return Summary{}, runDir, err
	}
	if err := os.WriteFile(filepath.Join(runDir, "leaderboard.md"), []byte(leaderboardMarkdown(summary)), 0o644); err != nil {
		return Summary{}, runDir, err
	}
	return summary, runDir, nil
}

var safeRunID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func resolveRunDirectory(artifactDir, runID string) (string, error) {
	if len(runID) == 0 || len(runID) > 128 || !safeRunID.MatchString(runID) {
		return "", fmt.Errorf("invalid run-id %q: use 1-128 ASCII letters, digits, dot, underscore, or hyphen, starting with a letter or digit", runID)
	}
	root, err := filepath.Abs(artifactDir)
	if err != nil {
		return "", err
	}
	runDir, err := filepath.Abs(filepath.Join(root, runID))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, runDir)
	if err != nil {
		return "", err
	}
	if rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("run-id %q resolves outside artifact directory", runID)
	}
	return runDir, nil
}

func prepareRunDirectory(runDir string, reuse bool) error {
	info, err := os.Lstat(runDir)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if reuse {
			return fmt.Errorf("reuse run directory does not exist: %s", runDir)
		}
		if err := os.MkdirAll(filepath.Dir(runDir), 0o755); err != nil {
			return err
		}
		if err := os.Mkdir(runDir, 0o755); err != nil {
			if os.IsExist(err) {
				return fmt.Errorf("run directory already exists: %s (choose a new run-id or use --reuse-workspaces)", runDir)
			}
			return err
		}
		return nil
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("run directory must not be a symbolic link or junction: %s", runDir)
	}
	if !info.IsDir() {
		return fmt.Errorf("run path is not a directory: %s", runDir)
	}
	if !reuse {
		return fmt.Errorf("run directory already exists: %s (choose a new run-id or use --reuse-workspaces)", runDir)
	}
	return nil
}

func invalidateReuseAggregates(runDir string) error {
	// Invalidate published aggregate state only after this process owns the run
	// lock. Candidate result.json files remain because reuse validation needs
	// their builder/source fingerprints.
	for _, name := range []string{"manifest.json", "summary.json", "leaderboard.md"} {
		if err := os.Remove(filepath.Join(runDir, name)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("invalidate %s: %w", name, err)
		}
	}
	return nil
}

func acquireRunLock(runDir string) (func(), error) {
	lockPath := filepath.Join(runDir, ".eval.lock")
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("evaluation run is already locked: %s (remove .eval.lock only after confirming no evaluator is running)", runDir)
		}
		return nil, err
	}
	_, writeErr := fmt.Fprintf(lock, "pid=%d\nstarted_at=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano))
	closeErr := lock.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(lockPath)
		if writeErr != nil {
			return nil, writeErr
		}
		return nil, closeErr
	}
	return func() { _ = os.Remove(lockPath) }, nil
}

func competitionExclusions(suite *Suite, variants []Variant, scenarios []Scenario, opts Options, candidates []CandidateResult) []string {
	var reasons []string
	eligibleConfigured := make(map[string]bool)
	for _, variant := range suite.Variants {
		if strings.TrimSpace(variant.HistoricalOnlyReason) == "" {
			eligibleConfigured[variant.ID] = true
		}
	}
	if len(eligibleConfigured) < 2 {
		reasons = append(reasons, fmt.Sprintf("configured %d promotion-eligible framework variant; at least 2 are required for a comparison", len(eligibleConfigured)))
	}
	eligibleSelected := make(map[string]bool)
	for _, variant := range variants {
		if eligibleConfigured[variant.ID] {
			eligibleSelected[variant.ID] = true
		}
	}
	if len(eligibleSelected) != len(eligibleConfigured) {
		reasons = append(reasons, fmt.Sprintf("selected %d of %d promotion-eligible variants", len(eligibleSelected), len(eligibleConfigured)))
	}
	if len(scenarios) != len(suite.Scenarios) {
		reasons = append(reasons, fmt.Sprintf("selected %d of %d scenarios", len(scenarios), len(suite.Scenarios)))
	}
	if opts.JudgeRuns < suite.Judge.Runs {
		reasons = append(reasons, fmt.Sprintf("holistic panel has %d of %d required judges", opts.JudgeRuns, suite.Judge.Runs))
	}
	if opts.MobileJudgeRuns < suite.Judge.MobileRuns {
		reasons = append(reasons, fmt.Sprintf("mobile panel has %d of %d required judges", opts.MobileJudgeRuns, suite.Judge.MobileRuns))
	}
	expectedCells := len(eligibleSelected) * len(scenarios) * opts.RunsPerCell
	var eligibleCandidateCount int
	for _, candidate := range candidates {
		if eligibleSelected[candidate.VariantID] {
			eligibleCandidateCount++
		}
	}
	if eligibleCandidateCount != expectedCells {
		reasons = append(reasons, fmt.Sprintf("promotion-eligible matrix produced %d of %d expected candidates", eligibleCandidateCount, expectedCells))
	}
	seen := make(map[string]int, len(candidates))
	for _, candidate := range candidates {
		key := fmt.Sprintf("%s|%s|%d", candidate.VariantID, candidate.ScenarioID, candidate.Repetition)
		seen[key]++
	}
	for _, variant := range variants {
		if !eligibleSelected[variant.ID] {
			continue
		}
		for _, scenario := range scenarios {
			for rep := 1; rep <= opts.RunsPerCell; rep++ {
				key := fmt.Sprintf("%s|%s|%d", variant.ID, scenario.ID, rep)
				if seen[key] != 1 {
					reasons = append(reasons, fmt.Sprintf("matrix cell %s/%s/run-%02d appears %d times", variant.ID, scenario.ID, rep, seen[key]))
				}
			}
		}
	}
	return reasons
}

func executeCandidate(ctx context.Context, suite *Suite, opts Options, gofastrBin string, variant Variant, scenario Scenario, rep int, mapping CandidateMapping, blindDir, rubric string, builderEnv, judgeEnv, mobileJudgeEnv []string) CandidateResult {
	result := CandidateResult{
		BlindID: mapping.BlindID, VariantID: variant.ID, ScenarioID: scenario.ID, Repetition: rep,
		BuilderFingerprint: mapping.BuilderFingerprint, BuilderProvenance: mapping.BuilderProvenance, BuilderModel: suite.Agents.Builder.Model,
		FrameworkRoot: mapping.FrameworkRoot, FrameworkFingerprint: mapping.FrameworkFingerprint,
		HolisticJudgeModel: suite.Agents.Judge.Model, MobileJudgeModel: suite.Agents.MobileJudge.Model,
		ReusedWorkspace:      opts.ReuseWorkspaces,
		HistoricalOnlyReason: variant.HistoricalOnlyReason,
	}
	// A reuse run may fail before reaching runJudgePanel. Clear the candidate's
	// previous judge state up front so stale successful judgments can never sit
	// beside a newly technical-failing result under the same run ID.
	if err := resetCandidateJudgeState(blindDir, mapping.BlindID); err != nil {
		result.TechnicalIssues = append(result.TechnicalIssues, err.Error())
		return result
	}
	if !opts.ReuseWorkspaces {
		for _, path := range []string{filepath.Dir(mapping.Workspace), mapping.ResultDir, blindDir} {
			if err := os.RemoveAll(path); err != nil {
				result.TechnicalIssues = append(result.TechnicalIssues, fmt.Sprintf("reset %s: %v", path, err))
				return result
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(mapping.Workspace), 0o755); err != nil {
		result.TechnicalIssues = append(result.TechnicalIssues, err.Error())
		return result
	}
	if err := os.MkdirAll(mapping.ResultDir, 0o755); err != nil {
		result.TechnicalIssues = append(result.TechnicalIssues, err.Error())
		return result
	}

	if opts.ReuseWorkspaces {
		if err := verifyFrameworkIntegrity(variant.FrameworkRoot, gofastrBin, mapping.FrameworkFingerprint); err != nil {
			result.TechnicalIssues = append(result.TechnicalIssues, "framework integrity before reuse: "+err.Error())
			return result
		}
		prior, err := validateReusableWorkspace(mapping, variant, scenario)
		if err != nil {
			result.TechnicalIssues = append(result.TechnicalIssues, "reuse workspace: "+err.Error())
			return result
		}
		result.BuilderDuration, result.BuilderTokens = prior.BuilderDuration, prior.BuilderTokens
		result.WorkspaceFingerprint = prior.WorkspaceFingerprint
		result.BuilderProvenance = prior.BuilderProvenance
		result.GuidanceFingerprint = prior.GuidanceFingerprint
		result.BuilderCLICalls = prior.BuilderCLICalls
		result.BuilderUsedDevServer = prior.BuilderUsedDevServer
		result.BuilderDocsCalls = prior.BuilderDocsCalls
		result.BuilderDocsSearches = append([]string(nil), prior.BuilderDocsSearches...)
		result.BuilderDocsTopics = append([]string(nil), prior.BuilderDocsTopics...)
		result.BuilderUsedCapabilityMap = prior.BuilderUsedCapabilityMap
		result.BuilderUsedMCP = prior.BuilderUsedMCP
	} else {
		prepCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		err := runCommand(prepCtx, filepath.Dir(mapping.Workspace), filepath.Join(mapping.ResultDir, "scaffold.log"), nil,
			gofastrBin, "init", "app", "--module=eval.local/"+mapping.BlindID, "--no-entity")
		cancel()
		if err != nil {
			result.TechnicalIssues = append(result.TechnicalIssues, "scaffold: "+err.Error())
			return result
		}
		if err := os.WriteFile(filepath.Join(mapping.Workspace, "EVAL_TASK.md"), []byte(taskMarkdown(scenario)), 0o644); err != nil {
			result.TechnicalIssues = append(result.TechnicalIssues, "task: "+err.Error())
			return result
		}
		modCtx, modCancel := context.WithTimeout(ctx, time.Minute)
		err = runCommand(modCtx, mapping.Workspace, filepath.Join(mapping.ResultDir, "go-mod-edit.log"), inheritedEnvironment("GOWORK=off"),
			"go", "mod", "edit",
			"-require=github.com/DonaldMurillo/gofastr@v0.0.0",
			"-replace=github.com/DonaldMurillo/gofastr="+variant.FrameworkRoot)
		modCancel()
		if err != nil {
			result.TechnicalIssues = append(result.TechnicalIssues, "pin local framework: "+err.Error())
			return result
		}
		tidyCtx, tidyCancel := context.WithTimeout(ctx, 5*time.Minute)
		err = runCommand(tidyCtx, mapping.Workspace, filepath.Join(mapping.ResultDir, "go-mod-tidy.log"), inheritedEnvironment("GOWORK=off"),
			"go", "mod", "tidy")
		tidyCancel()
		if err != nil {
			result.TechnicalIssues = append(result.TechnicalIssues, "resolve scaffold dependencies: "+err.Error())
			return result
		}
		guidanceFingerprint, guidanceErr := generatedGuidanceFingerprint(mapping.Workspace)
		if guidanceErr != nil {
			result.TechnicalIssues = append(result.TechnicalIssues, "fingerprint generated guidance: "+guidanceErr.Error())
			return result
		}
		result.GuidanceFingerprint = guidanceFingerprint

		// The snapshot's own CLI goes first on the builder's PATH via a
		// logging shim: the generated guidance tells agents to run
		// `gofastr docs` / `gofastr dev`, and a globally installed gofastr
		// would leak a different framework version into the treatment. The
		// shim log doubles as the non-deterministic dev-loop funnel signal.
		cliLog := filepath.Join(mapping.ResultDir, "cli-invocations.log")
		shimDir := filepath.Join(mapping.ResultDir, "cli")
		if err := installCLIShim(shimDir, gofastrBin, cliLog); err != nil {
			result.TechnicalIssues = append(result.TechnicalIssues, "install CLI shim: "+err.Error())
			return result
		}
		buildOutput := filepath.Join(mapping.ResultDir, "builder-final.md")
		inv := agentRequest{
			Config: suite.Agents.Builder,
			Env: append(append([]string(nil), builderEnv...),
				"GOCACHE="+filepath.Join(mapping.Workspace, ".cache", "go-build"),
				"GOFLAGS=-buildvcs=false",
				"PATH="+shimDir+string(os.PathListSeparator)+os.Getenv("PATH")),
			Workspace: mapping.Workspace, OutputPath: buildOutput, Prompt: builderPrompt(), LogPath: filepath.Join(mapping.ResultDir, "builder.log"),
		}
		builderStarted := time.Now()
		builderErr := runAgent(ctx, inv)
		result.BuilderDuration = time.Since(builderStarted).Seconds()
		_, result.BuilderTokens = builderMetricsFromArtifacts(mapping.ResultDir)
		result.BuilderCLICalls, result.BuilderUsedDevServer = cliInvocationStats(cliLog)
		docsStats := cliDocsInvocationStats(cliLog)
		result.BuilderDocsCalls = docsStats.Calls
		result.BuilderDocsSearches = docsStats.Searches
		result.BuilderDocsTopics = docsStats.Topics
		result.BuilderUsedCapabilityMap = docsStats.UsedCapabilityMap
		result.BuilderUsedMCP = builderUsedMCP(inv.LogPath, buildOutput)
		integrityErr := verifyFrameworkIntegrity(variant.FrameworkRoot, gofastrBin, mapping.FrameworkFingerprint)
		if integrityErr != nil {
			result.TechnicalIssues = append(result.TechnicalIssues, "framework integrity after builder: "+integrityErr.Error())
		}
		if builderErr != nil {
			result.TechnicalIssues = append(result.TechnicalIssues, "builder agent: "+builderErr.Error())
			return result
		}
		if integrityErr != nil {
			return result
		}
		afterGuidanceFingerprint, guidanceErr := generatedGuidanceFingerprint(mapping.Workspace)
		if guidanceErr != nil {
			result.TechnicalIssues = append(result.TechnicalIssues, "verify generated guidance: "+guidanceErr.Error())
			return result
		}
		if afterGuidanceFingerprint != result.GuidanceFingerprint {
			result.TechnicalIssues = append(result.TechnicalIssues, fmt.Sprintf("builder modified generated framework guidance: before=%s after=%s", result.GuidanceFingerprint, afterGuidanceFingerprint))
			return result
		}
		workspaceFingerprint, fingerprintErr := workspaceSourceFingerprint(mapping.Workspace)
		if fingerprintErr != nil {
			result.TechnicalIssues = append(result.TechnicalIssues, "fingerprint built workspace: "+fingerprintErr.Error())
			return result
		}
		result.WorkspaceFingerprint = workspaceFingerprint
	}

	candidateHome := filepath.Join(mapping.Workspace, ".cache", "home")
	if err := prepareCandidateHome(candidateHome); err != nil {
		result.TechnicalIssues = append(result.TechnicalIssues, "create isolated candidate home: "+err.Error())
		return result
	}
	candidateEnv := candidateEnvironment(candidateHome,
		"GOWORK=off",
		"GOCACHE="+filepath.Join(mapping.Workspace, ".cache", "go-build"),
		"GOFLAGS=-buildvcs=false",
	)
	testCtx, testCancel := context.WithTimeout(ctx, 10*time.Minute)
	testErr := runCommand(testCtx, mapping.Workspace, filepath.Join(mapping.ResultDir, "go-test.log"), candidateEnv, "go", "test", "./...")
	testCancel()
	result.TestsSucceeded = testErr == nil
	if testErr != nil {
		result.TechnicalIssues = append(result.TechnicalIssues, "go test: "+testErr.Error())
	}
	appBin := filepath.Join(mapping.ResultDir, executableName("candidate-app"))
	compileCtx, compileCancel := context.WithTimeout(ctx, 10*time.Minute)
	buildErr := runCommand(compileCtx, mapping.Workspace, filepath.Join(mapping.ResultDir, "go-build.log"), candidateEnv, "go", "build", "-o", appBin, ".")
	compileCancel()
	result.BuildSucceeded = buildErr == nil
	if buildErr != nil {
		result.TechnicalIssues = append(result.TechnicalIssues, "go build: "+buildErr.Error())
		return result
	}
	postGateFingerprint, fingerprintErr := workspaceSourceFingerprint(mapping.Workspace)
	if fingerprintErr != nil {
		result.TechnicalIssues = append(result.TechnicalIssues, "fingerprint workspace after gates: "+fingerprintErr.Error())
		return result
	}
	if postGateFingerprint != result.WorkspaceFingerprint {
		result.TechnicalIssues = append(result.TechnicalIssues, fmt.Sprintf("workspace source changed during go test/build: before=%s after=%s", result.WorkspaceFingerprint, postGateFingerprint))
		return result
	}

	port, err := freePort()
	if err != nil {
		result.TechnicalIssues = append(result.TechnicalIssues, "allocate port: "+err.Error())
		return result
	}
	serverLog, err := os.Create(filepath.Join(mapping.ResultDir, "server.log"))
	if err != nil {
		result.TechnicalIssues = append(result.TechnicalIssues, "server log: "+err.Error())
		return result
	}
	server := exec.CommandContext(ctx, appBin)
	configureCommandCancellation(server)
	server.Dir = mapping.Workspace
	server.Env = environmentWithOverrides(candidateEnv, fmt.Sprintf("PORT=127.0.0.1:%d", port))
	server.Stdout, server.Stderr = serverLog, serverLog
	serverCleanup, err := startOwnedCommand(server)
	if err != nil {
		_ = serverLog.Close()
		result.TechnicalIssues = append(result.TechnicalIssues, "start server: "+err.Error())
		return result
	}
	serverExited := make(chan struct{})
	var serverWaitErr error
	go func() {
		serverWaitErr = server.Wait()
		close(serverExited)
	}()
	defer func() {
		serverCleanup()
		_ = server.Process.Kill()
		<-serverExited
		_ = serverLog.Close()
	}()
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	healthCtx, healthCancel := context.WithTimeout(ctx, 2*time.Minute)
	err = waitForOwnedHealth(healthCtx, baseURL+"/healthz", serverExited, func() error { return serverWaitErr })
	healthCancel()
	result.ServerSucceeded = err == nil
	if err != nil {
		result.TechnicalIssues = append(result.TechnicalIssues, "server health: "+err.Error())
		return result
	}
	// Agent-surface probe on the served candidate (prod-style boot): did
	// the MCP contract survive the builder, and did the dev-only log
	// tools stay off outside the dev loop?
	result.CandidateMCPTools, result.CandidateMCPIntrospection, result.CandidateMCPLogToolsProd = probeCandidateMCP(ctx, baseURL)

	if err := os.MkdirAll(blindDir, 0o755); err != nil {
		result.TechnicalIssues = append(result.TechnicalIssues, "blind dir: "+err.Error())
		return result
	}
	_ = runCommand(ctx, blindDir, "", nil, "git", "init", "-q")
	_ = os.WriteFile(filepath.Join(blindDir, "AGENTS.md"), []byte("# Blind visual judge workspace\nJudge only the supplied screenshots. Do not inspect parent directories or source workspaces.\n"), 0o644)
	_ = copyFile(suite.Judge.Schema, filepath.Join(blindDir, "judge.schema.json"))
	shots, captureIssues := captureCandidate(ctx, baseURL, blindDir, scenario, suite.Viewports)
	result.Screenshots = shots
	result.TechnicalIssues = append(result.TechnicalIssues, captureIssues...)
	expectedShots := len(scenario.Pages) * len(suite.Viewports) * 2
	result.TechnicalPassed = result.BuildSucceeded && result.TestsSucceeded && result.ServerSucceeded && len(shots) == expectedShots && len(result.TechnicalIssues) == 0
	if len(shots) != expectedShots {
		result.TechnicalIssues = append(result.TechnicalIssues, fmt.Sprintf("expected %d screenshots, captured %d", expectedShots, len(shots)))
		result.TechnicalPassed = false
	}
	if !result.TechnicalPassed {
		return result
	}

	var judgeIssues []string
	result.Judgments, judgeIssues = runJudgePanel(ctx, suite, mapping, scenario, blindDir, rubric, judgeEnv, judgeLensHolistic, shots, opts.JudgeRuns)
	result.TechnicalIssues = append(result.TechnicalIssues, judgeIssues...)
	result.MobileJudgments, judgeIssues = runJudgePanel(ctx, suite, mapping, scenario, blindDir, rubric, mobileJudgeEnv, judgeLensMobile, mobileScreenshots(shots), opts.MobileJudgeRuns)
	result.TechnicalIssues = append(result.TechnicalIssues, judgeIssues...)
	if len(result.Judgments) != opts.JudgeRuns || len(result.MobileJudgments) != opts.MobileJudgeRuns {
		result.TechnicalPassed = false
	}
	for _, panel := range [][]Judgment{result.Judgments, result.MobileJudgments} {
		for _, judgment := range panel {
			result.Strongest = append(result.Strongest, judgment.StrongestVisible...)
			result.Weakest = append(result.Weakest, judgment.WeakestVisible...)
			if strings.TrimSpace(judgment.NextIteration) != "" {
				result.NextIterations = append(result.NextIterations, judgment.NextIteration)
			}
		}
	}
	result.Dimensions, result.Overall, result.MinimumDimension = aggregateJudgments(result.Judgments)
	result.MobileDimensions, result.MobileOverall, result.MobileMinimumDimension = aggregateJudgments(result.MobileJudgments)
	result.HolisticShadcnConsensus = shadcnConsensus(result.Judgments)
	result.MobileShadcnConsensus = shadcnConsensus(result.MobileJudgments)
	result.QualityPassed = candidateMeetsQualityBar(suite, result, opts.JudgeRuns, opts.MobileJudgeRuns)
	return result
}

func taskMarkdown(scenario Scenario) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Product task: %s\n\n%s\n\n", scenario.Title, scenario.Brief)
	b.WriteString("## Required routes\n\n")
	for _, page := range scenario.Pages {
		fmt.Fprintf(&b, "- `%s` — %s\n", page.Path, page.Name)
	}
	b.WriteString("\nUse local deterministic content. Every route must return a complete SSR page and be reachable without authentication.\n")
	return b.String()
}

func candidateID(runID, variantID, scenarioID string, rep int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%d", runID, variantID, scenarioID, rep)))
	return "candidate-" + hex.EncodeToString(sum[:])[:12]
}

func selectVariants(all []Variant, selected map[string]bool) ([]Variant, error) {
	if len(selected) == 0 {
		return all, nil
	}
	var out []Variant
	seen := map[string]bool{}
	for _, item := range all {
		if selected[item.ID] {
			out = append(out, item)
			seen[item.ID] = true
		}
	}
	for id := range selected {
		if !seen[id] {
			return nil, fmt.Errorf("unknown variant %q", id)
		}
	}
	return out, nil
}

func selectScenarios(all []Scenario, selected map[string]bool) ([]Scenario, error) {
	if len(selected) == 0 {
		return all, nil
	}
	var out []Scenario
	seen := map[string]bool{}
	for _, item := range all {
		if selected[item.ID] {
			out = append(out, item)
			seen[item.ID] = true
		}
	}
	for id := range selected {
		if !seen[id] {
			return nil, fmt.Errorf("unknown scenario %q", id)
		}
	}
	return out, nil
}

func writeJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(b, '\n'), 0o644)
}

func readJSON(path string, value any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, value); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func SortedIDs(set map[string]bool) []string {
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func applyAgentOptions(cfg *AgentConfig, backend, program string, prefixArgs []string, model string) {
	if strings.TrimSpace(backend) != "" {
		backend = strings.ToLower(strings.TrimSpace(backend))
		changed := backend != cfg.Backend
		cfg.Backend = backend
		if changed {
			cfg.Program = backend
			cfg.Effort = ""
			switch backend {
			case "omp":
				cfg.Model = "glm-5.2"
			case "claude":
				cfg.Model = "opus"
			default:
				// codex has no baked-in model alias: catalogs vary per
				// install and model IDs are provenance. Clear the previous
				// backend's model so validation demands an explicit
				// --*-model instead of launching `codex --model opus`.
				cfg.Model = ""
			}
		}
	}
	if strings.TrimSpace(program) != "" {
		cfg.Program = program
	}
	if prefixArgs != nil {
		cfg.PrefixArgs = append([]string(nil), prefixArgs...)
	}
	if strings.TrimSpace(model) != "" {
		cfg.Model = model
	}
}
