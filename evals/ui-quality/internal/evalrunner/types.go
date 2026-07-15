package evalrunner

import "time"

type Options struct {
	RunID                 string
	Variants              map[string]bool
	Scenarios             map[string]bool
	RunsPerCell           int
	JudgeRuns             int
	MobileJudgeRuns       int
	BuilderBackend        string
	BuilderProgram        string
	BuilderPrefixArgs     []string
	JudgeBackend          string
	JudgeProgram          string
	JudgePrefixArgs       []string
	MobileJudgeBackend    string
	MobileJudgeProgram    string
	MobileJudgePrefixArgs []string
	CodexHomeSource       string
	BuilderModel          string
	JudgeModel            string
	MobileJudgeModel      string
	DryRun                bool
	ReuseWorkspaces       bool
}

type RunManifest struct {
	Suite                   string             `json:"suite"`
	RunID                   string             `json:"run_id"`
	StartedAt               time.Time          `json:"started_at"`
	RepoRoot                string             `json:"repo_root"`
	Frameworks              []FrameworkRecord  `json:"frameworks,omitempty"`
	ProtocolFingerprint     string             `json:"protocol_fingerprint"`
	ReusedWorkspaces        bool               `json:"reused_workspaces"`
	BuilderProvenance       AgentProvenance    `json:"builder_provenance"`
	HolisticJudgeProvenance AgentProvenance    `json:"holistic_judge_provenance"`
	MobileJudgeProvenance   AgentProvenance    `json:"mobile_judge_provenance"`
	Candidates              []CandidateMapping `json:"candidates"`
}

type FrameworkRecord struct {
	VariantID   string `json:"variant_id"`
	Root        string `json:"root"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

type AgentProvenance struct {
	Backend    string   `json:"backend"`
	Program    string   `json:"program"`
	Version    string   `json:"version"`
	PrefixArgs []string `json:"prefix_args"`
	Model      string   `json:"model"`
}

type CandidateMapping struct {
	BlindID              string          `json:"blind_id"`
	VariantID            string          `json:"variant_id"`
	ScenarioID           string          `json:"scenario_id"`
	Repetition           int             `json:"repetition"`
	Workspace            string          `json:"workspace"`
	ResultDir            string          `json:"result_dir"`
	BuilderFingerprint   string          `json:"builder_fingerprint,omitempty"`
	WorkspaceFingerprint string          `json:"workspace_fingerprint,omitempty"`
	BuilderProvenance    AgentProvenance `json:"builder_provenance"`
	FrameworkRoot        string          `json:"framework_root"`
	FrameworkFingerprint string          `json:"framework_fingerprint,omitempty"`
	HistoricalOnlyReason string          `json:"historical_only_reason,omitempty"`
}

type CandidateResult struct {
	BlindID                 string          `json:"blind_id"`
	VariantID               string          `json:"variant_id"`
	ScenarioID              string          `json:"scenario_id"`
	Repetition              int             `json:"repetition"`
	BuilderFingerprint      string          `json:"builder_fingerprint"`
	FrameworkRoot           string          `json:"framework_root"`
	FrameworkFingerprint    string          `json:"framework_fingerprint"`
	GuidanceFingerprint     string          `json:"scaffold_guidance_fingerprint"`
	ProtocolFingerprint     string          `json:"protocol_fingerprint"`
	WorkspaceFingerprint    string          `json:"workspace_fingerprint"`
	BuilderProvenance       AgentProvenance `json:"builder_provenance"`
	BuilderModel            string          `json:"builder_model"`
	HolisticJudgeModel      string          `json:"holistic_judge_model"`
	MobileJudgeModel        string          `json:"mobile_judge_model"`
	HolisticJudgeProvenance AgentProvenance `json:"holistic_judge_provenance"`
	MobileJudgeProvenance   AgentProvenance `json:"mobile_judge_provenance"`
	BuilderDuration         float64         `json:"builder_duration_seconds,omitempty"`
	BuilderTokens           int64           `json:"builder_tokens,omitempty"`
	// BuilderCLICalls / BuilderUsedDevServer come from the PATH shim's
	// invocation log — a non-deterministic funnel signal: the harness
	// prompt names no command, so a `gofastr dev` here means the builder
	// discovered the hot-reload loop from the generated guidance alone.
	BuilderCLICalls         int                `json:"builder_cli_calls"`
	BuilderUsedDevServer    bool               `json:"builder_used_dev_server"`
	BuildSucceeded          bool               `json:"build_succeeded"`
	TestsSucceeded          bool               `json:"tests_succeeded"`
	ServerSucceeded         bool               `json:"server_succeeded"`
	TechnicalPassed         bool               `json:"technical_passed"`
	TechnicalIssues         []string           `json:"technical_issues,omitempty"`
	Screenshots             []ScreenshotResult `json:"screenshots,omitempty"`
	Judgments               []Judgment         `json:"judgments,omitempty"`
	MobileJudgments         []Judgment         `json:"mobile_judgments,omitempty"`
	Dimensions              Dimensions         `json:"dimensions"`
	Overall                 float64            `json:"overall"`
	MinimumDimension        float64            `json:"minimum_dimension"`
	MobileDimensions        Dimensions         `json:"mobile_dimensions"`
	MobileOverall           float64            `json:"mobile_overall"`
	MobileMinimumDimension  float64            `json:"mobile_minimum_dimension"`
	HolisticShadcnConsensus bool               `json:"holistic_shadcn_consensus"`
	MobileShadcnConsensus   bool               `json:"mobile_shadcn_consensus"`
	QualityPassed           bool               `json:"quality_passed"`
	ReusedWorkspace         bool               `json:"reused_workspace,omitempty"`
	Strongest               []string           `json:"strongest,omitempty"`
	Weakest                 []string           `json:"weakest,omitempty"`
	NextIterations          []string           `json:"next_iterations,omitempty"`
	HistoricalOnlyReason    string             `json:"historical_only_reason,omitempty"`
}

type ScreenshotResult struct {
	Page               string              `json:"page"`
	Path               string              `json:"path"`
	Viewport           string              `json:"viewport"`
	Scheme             string              `json:"scheme"`
	Kind               string              `json:"kind"`
	ImagePath          string              `json:"image_path"`
	ImageWidth         int                 `json:"image_width"`
	ImageHeight        int                 `json:"image_height"`
	ViewportWidth      int64               `json:"viewport_width"`
	ViewportHeight     int64               `json:"viewport_height"`
	DeviceScaleFactor  float64             `json:"device_scale_factor"`
	TouchPoints        int                 `json:"touch_points"`
	Orientation        string              `json:"orientation"`
	UserAgent          string              `json:"user_agent"`
	DocumentHeight     float64             `json:"document_height"`
	HTTPStatus         int                 `json:"http_status"`
	OverflowX          bool                `json:"overflow_x"`
	BoundsViolations   []BoundsViolation   `json:"bounds_violations,omitempty"`
	ContrastViolations []ContrastViolation `json:"contrast_violations,omitempty"`
	Bytes              int                 `json:"bytes"`
}

type BoundsViolation struct {
	Element       string  `json:"element"`
	Left          float64 `json:"left"`
	Right         float64 `json:"right"`
	ViewportWidth float64 `json:"viewport_width"`
}

type ContrastViolation struct {
	Element    string  `json:"element"`
	Foreground string  `json:"foreground"`
	Background string  `json:"background"`
	Ratio      float64 `json:"ratio"`
	Minimum    float64 `json:"minimum"`
}

type Judgment struct {
	CandidateID          string     `json:"candidate_id"`
	Dimensions           Dimensions `json:"dimensions"`
	StrongestVisible     []string   `json:"strongest_visible_decisions"`
	WeakestVisible       []string   `json:"weakest_visible_decisions"`
	ShadcnLevel          bool       `json:"shadcn_level"`
	ShadcnLevelRationale string     `json:"shadcn_level_rationale"`
	NextIteration        string     `json:"next_iteration"`
}

type Dimensions struct {
	Hierarchy          float64 `json:"hierarchy"`
	Composition        float64 `json:"composition"`
	Typography         float64 `json:"typography"`
	ProductSpecificity float64 `json:"product_specificity"`
	Density            float64 `json:"density"`
	ComponentPolish    float64 `json:"component_polish"`
	ResponsiveIntent   float64 `json:"responsive_intent"`
	ThemeCoherence     float64 `json:"theme_coherence"`
}

type VariantSummary struct {
	VariantID            string  `json:"variant_id"`
	PromotionEligible    bool    `json:"promotion_eligible"`
	Candidates           int     `json:"candidates"`
	MeanBuilderMinutes   float64 `json:"mean_builder_minutes,omitempty"`
	MeanBuilderTokens    int64   `json:"mean_builder_tokens,omitempty"`
	TechnicalPassRate    float64 `json:"technical_pass_rate"`
	QualityPassRate      float64 `json:"quality_pass_rate"`
	AllTechnicalPassed   bool    `json:"all_technical_passed"`
	AllQualityPassed     bool    `json:"all_quality_passed"`
	MeanOverall          float64 `json:"mean_overall"`
	WorstOverall         float64 `json:"worst_overall"`
	MeanMinimumDimension float64 `json:"mean_minimum_dimension"`
	MeanMobileOverall    float64 `json:"mean_mobile_overall"`
	WorstMobileOverall   float64 `json:"worst_mobile_overall"`
	RankScore            float64 `json:"rank_score"`
}

type Summary struct {
	Suite                 string            `json:"suite"`
	RunID                 string            `json:"run_id"`
	Competitive           bool              `json:"competitive"`
	NonCompetitiveReasons []string          `json:"noncompetitive_reasons,omitempty"`
	QualityBar            QualityBar        `json:"quality_bar"`
	Variants              []VariantSummary  `json:"variants"`
	Winner                string            `json:"winner,omitempty"`
	TiedLeaders           []string          `json:"tied_leaders,omitempty"`
	TiedLeadersMeetBar    bool              `json:"tied_leaders_meet_bar"`
	WinnerMeetsBar        bool              `json:"winner_meets_bar"`
	Candidates            []CandidateResult `json:"candidates"`
}

type QualityBar struct {
	MinimumOverall         float64 `json:"minimum_overall"`
	MinimumDimension       float64 `json:"minimum_dimension"`
	MinimumMobileOverall   float64 `json:"minimum_mobile_overall"`
	MinimumMobileDimension float64 `json:"minimum_mobile_dimension"`
	TechnicalPass          bool    `json:"technical_pass_required"`
	ShadcnConsensus        bool    `json:"shadcn_consensus_required"`
}
