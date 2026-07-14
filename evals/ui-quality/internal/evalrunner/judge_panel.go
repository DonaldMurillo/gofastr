package evalrunner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	judgeLensHolistic = "holistic"
	judgeLensMobile   = "mobile"
)

func runJudgePanel(ctx context.Context, suite *Suite, mapping CandidateMapping, scenario Scenario, blindDir, rubric string, agentEnv []string, lens string, shots []ScreenshotResult, runs int) ([]Judgment, []string) {
	if runs <= 0 {
		return nil, []string{fmt.Sprintf("%s judge panel has no configured runs", lens)}
	}
	if len(shots) == 0 {
		return nil, []string{fmt.Sprintf("%s judge panel has no screenshots", lens)}
	}
	sourceImages := make([]string, 0, len(shots))
	for _, shot := range shots {
		sourceImages = append(sourceImages, shot.ImagePath)
	}
	agentCfg := suite.Agents.Judge
	if lens == judgeLensMobile {
		agentCfg = suite.Agents.MobileJudge
	}
	var judgments []Judgment
	var issues []string
	for judgeRun := 1; judgeRun <= runs; judgeRun++ {
		judgeWorkspace, judgeDir := judgeIsolationPaths(blindDir, mapping.BlindID, lens, judgeRun)
		images, err := prepareJudgeWorkspace(judgeWorkspace, filepath.Join(blindDir, "judge.schema.json"), sourceImages)
		if err != nil {
			issues = append(issues, fmt.Sprintf("%s judge %d workspace: %v", lens, judgeRun, err))
			continue
		}
		if err := os.RemoveAll(judgeDir); err != nil {
			issues = append(issues, fmt.Sprintf("%s judge %d reset artifact dir: %v", lens, judgeRun, err))
			continue
		}
		if err := os.MkdirAll(judgeDir, 0o755); err != nil {
			issues = append(issues, fmt.Sprintf("%s judge %d artifact dir: %v", lens, judgeRun, err))
			continue
		}
		judgeOutput := filepath.Join(judgeDir, "judgment.json")
		var judgment Judgment
		var rejections []string
		accepted := false
		rejectionLog := filepath.Join(judgeDir, "rejections.log")
		_ = os.Remove(judgeOutput)
		_ = os.Remove(rejectionLog)
		for attempt := 1; attempt <= maxJudgeAttempts; attempt++ {
			attemptOutput := filepath.Join(judgeDir, fmt.Sprintf("attempt-%02d.json", attempt))
			inv := agentRequest{
				Config: agentCfg, Workspace: judgeWorkspace, OutputPath: attemptOutput,
				SchemaPath: filepath.Join(judgeWorkspace, "judge.schema.json"), Images: images,
				Env: agentEnv, Prompt: judgePrompt(mapping.BlindID, scenario, rubric, lens, shots),
				LogPath: filepath.Join(judgeDir, fmt.Sprintf("attempt-%02d.log", attempt)), Judge: true,
			}
			if err := runAgent(ctx, inv); err != nil {
				rejections = append(rejections, fmt.Sprintf("attempt %d execution: %v", attempt, err))
				continue
			}
			judgment = Judgment{}
			if err := readJSON(attemptOutput, &judgment); err != nil {
				rejections = append(rejections, fmt.Sprintf("attempt %d output: %v", attempt, err))
				continue
			}
			if err := validateJudgment(judgment, mapping.BlindID); err != nil {
				rejections = append(rejections, fmt.Sprintf("attempt %d semantic validation: %v", attempt, err))
				continue
			}
			if err := copyFile(attemptOutput, judgeOutput); err != nil {
				rejections = append(rejections, fmt.Sprintf("attempt %d preserve output: %v", attempt, err))
				continue
			}
			accepted = true
			break
		}
		if len(rejections) > 0 {
			_ = os.WriteFile(rejectionLog, []byte(strings.Join(rejections, "\n")+"\n"), 0o644)
		}
		if !accepted {
			issues = append(issues, fmt.Sprintf("%s judge %d rejected after %d attempts: %s", lens, judgeRun, maxJudgeAttempts, strings.Join(rejections, "; ")))
			continue
		}
		judgments = append(judgments, judgment)
	}
	return judgments, issues
}

func judgeIsolationPaths(blindDir, blindID, lens string, judgeRun int) (string, string) {
	runDir := filepath.Dir(filepath.Dir(blindDir))
	runName := fmt.Sprintf("run-%02d", judgeRun)
	workspace := filepath.Join(runDir, "judge-workspaces", blindID, lens, runName)
	artifacts := filepath.Join(runDir, "judge-artifacts", blindID, lens, runName)
	return workspace, artifacts
}

func resetCandidateJudgeState(blindDir, blindID string) error {
	runDir := filepath.Dir(filepath.Dir(blindDir))
	for _, root := range []string{"judge-workspaces", "judge-artifacts"} {
		if err := os.RemoveAll(filepath.Join(runDir, root, blindID)); err != nil {
			return fmt.Errorf("reset %s: %w", root, err)
		}
	}
	return nil
}

func prepareJudgeWorkspace(workspace, schemaSource string, sourceImages []string) ([]string, error) {
	if err := os.RemoveAll(workspace); err != nil {
		return nil, fmt.Errorf("reset workspace: %w", err)
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("# Isolated blind judge\nJudge only the images attached to the prompt. Do not inspect parent directories, candidate source, manifests, or other judge artifacts.\n"), 0o644); err != nil {
		return nil, err
	}
	if err := copyFile(schemaSource, filepath.Join(workspace, "judge.schema.json")); err != nil {
		return nil, err
	}
	images := make([]string, 0, len(sourceImages))
	for i, source := range sourceImages {
		destination := filepath.Join(workspace, fmt.Sprintf("evidence-%02d.png", i+1))
		if err := os.Link(source, destination); err != nil {
			if err := copyFile(source, destination); err != nil {
				return nil, fmt.Errorf("prepare evidence %d: %w", i+1, err)
			}
		}
		images = append(images, destination)
	}
	if err := runCommand(context.Background(), workspace, "", nil, "git", "init", "-q"); err != nil {
		return nil, err
	}
	return images, nil
}

func mobileScreenshots(shots []ScreenshotResult) []ScreenshotResult {
	var mobile []ScreenshotResult
	for _, shot := range shots {
		if shot.ViewportWidth <= 480 {
			mobile = append(mobile, shot)
		}
	}
	return mobile
}

func shadcnConsensus(judgments []Judgment) bool {
	if len(judgments) == 0 {
		return false
	}
	var passes int
	for _, judgment := range judgments {
		if judgment.ShadcnLevel {
			passes++
		}
	}
	return passes >= len(judgments)/2+1
}
