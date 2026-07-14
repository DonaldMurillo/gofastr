package evalrunner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type protocolSnapshot struct {
	Suite             *Suite   `json:"suite"`
	SelectedVariants  []string `json:"selected_variants"`
	SelectedScenarios []string `json:"selected_scenarios"`
	RunsPerCell       int      `json:"runs_per_cell"`
	JudgeRuns         int      `json:"judge_runs"`
	MobileJudgeRuns   int      `json:"mobile_judge_runs"`
}

func evaluatorProtocolSnapshot(suite *Suite, variants []Variant, scenarios []Scenario, opts Options) ([]byte, error) {
	snapshot := protocolSnapshot{
		Suite: suite, RunsPerCell: opts.RunsPerCell, JudgeRuns: opts.JudgeRuns, MobileJudgeRuns: opts.MobileJudgeRuns,
	}
	for _, variant := range variants {
		snapshot.SelectedVariants = append(snapshot.SelectedVariants, variant.ID)
	}
	for _, scenario := range scenarios {
		snapshot.SelectedScenarios = append(snapshot.SelectedScenarios, scenario.ID)
	}
	return json.MarshalIndent(snapshot, "", "  ")
}

func evaluatorProtocolFingerprint(suite *Suite, snapshot []byte) (string, error) {
	h := sha256.New()
	hashBytes(h, "effective-protocol", snapshot)
	for _, item := range []struct {
		label string
		path  string
	}{
		{"rubric", suite.Judge.Rubric},
		{"judge-schema", suite.Judge.Schema},
	} {
		if err := hashFile(h, item.label, item.path); err != nil {
			return "", err
		}
	}
	var sources []string
	if err := filepath.WalkDir(suite.SuiteDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type().IsRegular() && strings.EqualFold(filepath.Ext(path), ".go") {
			sources = append(sources, path)
		}
		return nil
	}); err != nil {
		return "", err
	}
	sort.Strings(sources)
	for _, path := range sources {
		rel, err := filepath.Rel(suite.SuiteDir, path)
		if err != nil {
			return "", err
		}
		if err := hashFile(h, filepath.ToSlash(rel), path); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

var frameworkFingerprintInputs = []string{
	"go.mod", "go.sum", "battery", "cmd/gofastr", "codegen", "core", "core-ui", "framework", "internal", "kiln", "sqlite",
}

func frameworkInputFingerprint(repoRoot, gofastrBin string) (string, error) {
	h := sha256.New()
	if err := hashFile(h, "built-gofastr", gofastrBin); err != nil {
		return "", err
	}
	var files []string
	for _, rel := range frameworkFingerprintInputs {
		root := filepath.Join(repoRoot, rel)
		info, err := os.Stat(root)
		if err != nil {
			return "", fmt.Errorf("stat %s: %w", rel, err)
		}
		if info.Mode().IsRegular() {
			files = append(files, root)
			continue
		}
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type().IsRegular() {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("walk %s: %w", rel, err)
		}
	}
	sort.Strings(files)
	for _, path := range files {
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return "", err
		}
		if err := hashFile(h, filepath.ToSlash(rel), path); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func verifyFrameworkIntegrity(repoRoot, gofastrBin, expected string) error {
	current, err := frameworkInputFingerprint(repoRoot, gofastrBin)
	if err != nil {
		return fmt.Errorf("fingerprint current framework input: %w", err)
	}
	if current != expected {
		return fmt.Errorf("framework input changed during evaluation: expected=%s current=%s", expected, current)
	}
	return nil
}

func candidateBuilderFingerprint(frameworkFingerprint string, variant Variant, scenario Scenario, provenance AgentProvenance) (string, error) {
	h := sha256.New()
	hashBytes(h, "framework", []byte(frameworkFingerprint))
	hashBytes(h, "framework-variant", []byte(variant.ID))
	hashBytes(h, "task", []byte(taskMarkdown(scenario)))
	hashBytes(h, "builder-prompt", []byte(builderPrompt()))
	provenanceText := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s", provenance.Backend, provenance.Program, provenance.Version, strings.Join(provenance.PrefixArgs, "\x00"), provenance.Model)
	hashBytes(h, "builder-provenance", []byte(provenanceText))
	return hex.EncodeToString(h.Sum(nil)), nil
}

var generatedGuidanceRoots = []string{
	"AGENTS.md",
	"CLAUDE.md",
	"agents",
	filepath.Join(".claude", "skills", "gofastr-host"),
}

// generatedGuidanceFingerprint captures the normal AI-onboarding surface
// emitted by `gofastr init`. The evaluation never replaces this surface with a
// treatment prompt, and a candidate fails if its builder mutates it.
func generatedGuidanceFingerprint(workspace string) (string, error) {
	var files []string
	for _, rel := range generatedGuidanceRoots {
		root := filepath.Join(workspace, rel)
		info, err := os.Stat(root)
		if err != nil {
			return "", fmt.Errorf("generated guidance %s: %w", rel, err)
		}
		if info.Mode().IsRegular() {
			files = append(files, root)
			continue
		}
		if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type().IsRegular() {
				files = append(files, path)
			}
			return nil
		}); err != nil {
			return "", err
		}
	}
	sort.Strings(files)
	h := sha256.New()
	for _, path := range files {
		rel, err := filepath.Rel(workspace, path)
		if err != nil {
			return "", err
		}
		if err := hashFile(h, filepath.ToSlash(rel), path); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func workspaceSourceFingerprint(workspace string) (string, error) {
	var files []string
	err := filepath.WalkDir(workspace, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == ".cache") {
			return filepath.SkipDir
		}
		if entry.Type().IsRegular() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	h := sha256.New()
	for _, path := range files {
		rel, err := filepath.Rel(workspace, path)
		if err != nil {
			return "", err
		}
		if err := hashFile(h, filepath.ToSlash(rel), path); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func validateReusableWorkspace(mapping CandidateMapping, variant Variant, scenario Scenario) (CandidateResult, error) {
	var prior CandidateResult
	if err := readJSON(filepath.Join(mapping.ResultDir, "result.json"), &prior); err != nil {
		return prior, fmt.Errorf("read prior result: %w", err)
	}
	if prior.BuilderFingerprint == "" || prior.BuilderFingerprint != mapping.BuilderFingerprint {
		return prior, fmt.Errorf("builder fingerprint mismatch: prior=%q current=%q", prior.BuilderFingerprint, mapping.BuilderFingerprint)
	}
	if prior.WorkspaceFingerprint == "" {
		return prior, fmt.Errorf("prior result has no workspace source fingerprint")
	}
	workspaceFingerprint, err := workspaceSourceFingerprint(mapping.Workspace)
	if err != nil {
		return prior, fmt.Errorf("fingerprint reusable workspace: %w", err)
	}
	if workspaceFingerprint != prior.WorkspaceFingerprint {
		return prior, fmt.Errorf("workspace source fingerprint mismatch: prior=%q current=%q", prior.WorkspaceFingerprint, workspaceFingerprint)
	}
	checks := []struct {
		name string
		path string
		want []byte
	}{
		{"EVAL_TASK.md", filepath.Join(mapping.Workspace, "EVAL_TASK.md"), []byte(taskMarkdown(scenario))},
	}
	if _, err := os.Stat(filepath.Join(mapping.Workspace, "main.go")); err != nil {
		return prior, fmt.Errorf("missing main.go: %w", err)
	}
	for _, check := range checks {
		got, err := os.ReadFile(check.path)
		if err != nil {
			return prior, fmt.Errorf("read %s: %w", check.name, err)
		}
		if string(got) != string(check.want) {
			return prior, fmt.Errorf("%s content does not match the fingerprinted input", check.name)
		}
	}
	guidanceFingerprint, err := generatedGuidanceFingerprint(mapping.Workspace)
	if err != nil {
		return prior, err
	}
	if prior.GuidanceFingerprint == "" || prior.GuidanceFingerprint != guidanceFingerprint {
		return prior, fmt.Errorf("generated guidance fingerprint mismatch: prior=%q current=%q", prior.GuidanceFingerprint, guidanceFingerprint)
	}
	return prior, nil
}

func hashFile(h hash.Hash, label, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", label, err)
	}
	defer f.Close()
	hashBytes(h, "path", []byte(strings.ToLower(filepath.ToSlash(label))))
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash %s: %w", label, err)
	}
	h.Write([]byte{0})
	return nil
}

func hashBytes(h hash.Hash, label string, value []byte) {
	h.Write([]byte(label))
	h.Write([]byte{0})
	h.Write(value)
	h.Write([]byte{0})
}
