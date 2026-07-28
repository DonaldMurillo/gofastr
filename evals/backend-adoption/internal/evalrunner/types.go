package evalrunner

import "time"

type PhaseResult struct {
	AgentOK       bool      `json:"agent_ok"`
	AgentError    string    `json:"agent_error,omitempty"`
	Duration      float64   `json:"duration_seconds"`
	Tokens        int64     `json:"tokens"`
	BuildOK       bool      `json:"build_ok"`
	TestOK        bool      `json:"test_ok"`
	Score         int       `json:"score"`
	Maximum       int       `json:"maximum"`
	Checks        []Check   `json:"checks"`
	SourceLines   int       `json:"source_lines"`
	TestLines     int       `json:"test_lines"`
	DirectDeps    int       `json:"direct_dependencies"`
	GradedAt      time.Time `json:"graded_at"`
	GradeError    string    `json:"grade_error,omitempty"`
	ServerLogPath string    `json:"server_log_path,omitempty"`
}

type TrialResult struct {
	ID           string       `json:"id"`
	Framework    string       `json:"framework"`
	Repetition   int          `json:"repetition"`
	Workspace    string       `json:"workspace"`
	BuilderLog   string       `json:"builder_log"`
	CodexVersion string       `json:"codex_version"`
	Model        string       `json:"model"`
	Initial      PhaseResult  `json:"initial"`
	Maintenance  *PhaseResult `json:"maintenance,omitempty"`
}

type Check struct {
	ID       string `json:"id"`
	Points   int    `json:"points"`
	Maximum  int    `json:"maximum"`
	Passed   bool   `json:"passed"`
	Evidence string `json:"evidence"`
}

type Aggregate struct {
	RunID        string        `json:"run_id"`
	StartedAt    time.Time     `json:"started_at"`
	CompletedAt  time.Time     `json:"completed_at"`
	CodexVersion string        `json:"codex_version"`
	Model        string        `json:"model"`
	Runs         int           `json:"runs"`
	Trials       []TrialResult `json:"trials"`
}
