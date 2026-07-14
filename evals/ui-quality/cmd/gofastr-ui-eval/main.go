package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/evals/ui-quality/internal/evalrunner"
)

type stringSet map[string]bool

type stringList []string

func (s stringSet) String() string { return strings.Join(evalrunner.SortedIDs(s), ",") }
func (s stringSet) Set(value string) error {
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			s[item] = true
		}
	}
	return nil
}

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func main() {
	defaultSuite := filepath.Join("evals", "ui-quality", "suite.json")
	variants := stringSet{}
	scenarios := stringSet{}
	var builderPrefix, judgePrefix, mobileJudgePrefix stringList
	var (
		suitePath        = flag.String("suite", defaultSuite, "path to suite.json")
		runs             = flag.Int("runs", 1, "independent builder runs per variant/scenario cell")
		judgeRuns        = flag.Int("judge-runs", 0, "visual judges per candidate (default from suite)")
		mobileJudgeRuns  = flag.Int("mobile-judge-runs", 0, "mobile-only visual judges per candidate (default from suite)")
		runID            = flag.String("run-id", "", "stable run identifier (default UTC timestamp)")
		builderAgent     = flag.String("builder-agent", "", "builder backend: codex, omp, or claude")
		judgeAgent       = flag.String("judge-agent", "", "holistic judge backend: codex or claude")
		mobileJudgeAgent = flag.String("mobile-judge-agent", "", "mobile judge backend: codex or claude")
		builderBin       = flag.String("builder-bin", "", "override the builder executable")
		judgeBin         = flag.String("judge-bin", "", "override the holistic judge executable")
		mobileJudgeBin   = flag.String("mobile-judge-bin", "", "override the mobile judge executable")
		codexHome        = flag.String("codex-home-source", "", "Codex home containing auth.json (default CODEX_HOME or ~/.codex)")
		builderModel     = flag.String("builder-model", "", "pin the builder model")
		judgeModel       = flag.String("judge-model", "", "pin the visual judge model")
		mobileJudgeModel = flag.String("mobile-judge-model", "", "pin a different model for the mobile-only judge panel")
		dryRun           = flag.Bool("dry-run", false, "validate and write the candidate manifest without launching agents")
		smoke            = flag.Bool("smoke", false, "run all variants against only the first scenario with one judge")
		reuse            = flag.Bool("reuse-workspaces", false, "skip builders and rerun gates/judges from workspaces for the explicit run-id")
	)
	flag.Var(variants, "variant", "framework variant id; repeat or comma-separate")
	flag.Var(scenarios, "scenario", "scenario id; repeat or comma-separate")
	flag.Var(&builderPrefix, "builder-prefix-arg", "argument inserted before builder backend flags; repeat for wrappers")
	flag.Var(&judgePrefix, "judge-prefix-arg", "argument inserted before holistic judge backend flags; repeat for wrappers")
	flag.Var(&mobileJudgePrefix, "mobile-judge-prefix-arg", "argument inserted before mobile judge backend flags; repeat for wrappers")
	flag.Parse()
	if *reuse && *runID == "" {
		fatal(fmt.Errorf("--reuse-workspaces requires --run-id"))
	}

	suite, err := evalrunner.LoadSuite(*suitePath)
	if err != nil {
		fatal(err)
	}
	if *smoke {
		if len(scenarios) == 0 {
			scenarios[suite.Scenarios[0].ID] = true
		}
		if *judgeRuns == 0 {
			*judgeRuns = 1
		}
		if *mobileJudgeRuns == 0 {
			*mobileJudgeRuns = 1
		}
	}
	summary, artifactDir, err := evalrunner.Run(context.Background(), suite, evalrunner.Options{
		RunID: *runID, Variants: variants, Scenarios: scenarios, RunsPerCell: *runs,
		JudgeRuns: *judgeRuns, MobileJudgeRuns: *mobileJudgeRuns,
		BuilderBackend: *builderAgent, BuilderProgram: *builderBin, BuilderPrefixArgs: builderPrefix, BuilderModel: *builderModel,
		JudgeBackend: *judgeAgent, JudgeProgram: *judgeBin, JudgePrefixArgs: judgePrefix, JudgeModel: *judgeModel,
		MobileJudgeBackend: *mobileJudgeAgent, MobileJudgeProgram: *mobileJudgeBin, MobileJudgePrefixArgs: mobileJudgePrefix, MobileJudgeModel: *mobileJudgeModel,
		CodexHomeSource: *codexHome, DryRun: *dryRun, ReuseWorkspaces: *reuse,
	})
	if err != nil {
		fatal(err)
	}
	if *dryRun {
		fmt.Printf("dry-run manifest written to %s\n", artifactDir)
		return
	}
	fmt.Printf("artifacts: %s\n", artifactDir)
	fmt.Println(outcomeText(summary))
}

func outcomeText(summary evalrunner.Summary) string {
	if summary.WinnerMeetsBar {
		return fmt.Sprintf("winner: %s (meets configured quality bar)", summary.Winner)
	}
	if summary.TiedLeadersMeetBar {
		return fmt.Sprintf("no unique winner: %s are exactly tied and meet the quality bar", strings.Join(summary.TiedLeaders, ", "))
	}
	if len(summary.TiedLeaders) > 0 && !summary.Competitive {
		return fmt.Sprintf("diagnostic leaders: %s are exactly tied (noncompetitive run; cannot establish a passing winner)", strings.Join(summary.TiedLeaders, ", "))
	}
	if len(summary.TiedLeaders) > 0 {
		return fmt.Sprintf("no unique leader: %s are exactly tied", strings.Join(summary.TiedLeaders, ", "))
	}
	if !summary.Competitive && summary.Winner != "" {
		return fmt.Sprintf("diagnostic leader: %s (noncompetitive run; cannot establish a passing winner)", summary.Winner)
	}
	if summary.Winner != "" {
		return fmt.Sprintf("provisional leader: %s (no variant meets configured quality bar)", summary.Winner)
	}
	for _, variant := range summary.Variants {
		if variant.PromotionEligible && variant.TechnicalPassRate > 0 {
			return "no provisional leader: no promotion-eligible candidate clears the configured quality bar"
		}
	}
	return "no provisional leader: no promotion-eligible candidate produced a technical pass"
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "gofastr-ui-eval:", err)
	os.Exit(1)
}
