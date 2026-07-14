package evalrunner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var safeConfigID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type Suite struct {
	Name        string      `json:"name"`
	RepoRoot    string      `json:"repo_root"`
	ArtifactDir string      `json:"artifact_dir"`
	Agents      AgentRoles  `json:"agents"`
	Variants    []Variant   `json:"variants"`
	Scenarios   []Scenario  `json:"scenarios"`
	Viewports   []Viewport  `json:"viewports"`
	Judge       JudgeConfig `json:"judge"`
	SuitePath   string      `json:"-"`
	SuiteDir    string      `json:"-"`
}

type AgentRoles struct {
	Builder     AgentConfig `json:"builder"`
	Judge       AgentConfig `json:"judge"`
	MobileJudge AgentConfig `json:"mobile_judge"`
}

type AgentConfig struct {
	Backend        string   `json:"backend"`
	Program        string   `json:"program"`
	PrefixArgs     []string `json:"prefix_args,omitempty"`
	Model          string   `json:"model"`
	Effort         string   `json:"effort,omitempty"`
	TimeoutMinutes int      `json:"timeout_minutes"`
}

type Variant struct {
	ID                   string `json:"id"`
	FrameworkRoot        string `json:"framework_root,omitempty"`
	HistoricalOnlyReason string `json:"historical_only_reason,omitempty"`
}

type Scenario struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Brief string `json:"brief"`
	Pages []Page `json:"pages"`
}

type Page struct {
	Name          string `json:"name"`
	Path          string `json:"path"`
	ReadySelector string `json:"ready_selector"`
}

type Viewport struct {
	ID     string `json:"id"`
	Width  int64  `json:"width"`
	Height int64  `json:"height"`
	Scheme string `json:"scheme"`
}

type JudgeConfig struct {
	Rubric                 string  `json:"rubric"`
	Schema                 string  `json:"schema"`
	Runs                   int     `json:"runs"`
	MobileRuns             int     `json:"mobile_runs"`
	MinimumOverall         float64 `json:"minimum_overall"`
	MinimumDimension       float64 `json:"minimum_dimension"`
	MinimumMobileOverall   float64 `json:"minimum_mobile_overall"`
	MinimumMobileDimension float64 `json:"minimum_mobile_dimension"`
	RequireShadcnConsensus bool    `json:"require_shadcn_consensus"`
}

func LoadSuite(path string) (*Suite, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	var s Suite
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("parse suite: %w", err)
	}
	s.SuitePath = abs
	s.SuiteDir = filepath.Dir(abs)
	s.RepoRoot = resolvePath(s.SuiteDir, s.RepoRoot)
	s.ArtifactDir = resolvePath(s.SuiteDir, s.ArtifactDir)
	for i := range s.Variants {
		if strings.TrimSpace(s.Variants[i].FrameworkRoot) == "" {
			s.Variants[i].FrameworkRoot = s.RepoRoot
		} else {
			s.Variants[i].FrameworkRoot = resolvePath(s.SuiteDir, s.Variants[i].FrameworkRoot)
		}
	}
	s.Judge.Rubric = resolvePath(s.SuiteDir, s.Judge.Rubric)
	s.Judge.Schema = resolvePath(s.SuiteDir, s.Judge.Schema)
	if err := s.setDefaultsAndValidate(); err != nil {
		return nil, err
	}
	return &s, nil
}

func resolvePath(base, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(base, path))
}

func (s *Suite) setDefaultsAndValidate() error {
	if s.Name == "" {
		return errors.New("suite name is required")
	}
	for _, role := range []struct {
		name string
		cfg  *AgentConfig
		mins int
	}{
		{"builder", &s.Agents.Builder, 45},
		{"judge", &s.Agents.Judge, 12},
		{"mobile_judge", &s.Agents.MobileJudge, 12},
	} {
		if err := normalizeAgentConfig(role.name, role.cfg, role.mins); err != nil {
			return err
		}
	}
	if s.Judge.Runs <= 0 {
		s.Judge.Runs = 3
	}
	if s.Judge.MobileRuns <= 0 {
		s.Judge.MobileRuns = s.Judge.Runs
	}
	if s.Judge.MinimumOverall <= 0 {
		s.Judge.MinimumOverall = 8.5
	}
	if s.Judge.MinimumDimension <= 0 {
		s.Judge.MinimumDimension = 7.5
	}
	if s.Judge.MinimumMobileOverall <= 0 {
		s.Judge.MinimumMobileOverall = s.Judge.MinimumOverall
	}
	if s.Judge.MinimumMobileDimension <= 0 {
		s.Judge.MinimumMobileDimension = s.Judge.MinimumDimension
	}
	for _, p := range []struct {
		name string
		path string
	}{
		{"repo_root", s.RepoRoot},
		{"judge rubric", s.Judge.Rubric},
		{"judge schema", s.Judge.Schema},
	} {
		if _, err := os.Stat(p.path); err != nil {
			return fmt.Errorf("%s %q: %w", p.name, p.path, err)
		}
	}
	if len(s.Variants) == 0 {
		return errors.New("at least one framework variant is required")
	}
	if len(s.Scenarios) == 0 {
		return errors.New("at least one scenario is required")
	}
	if len(s.Viewports) < 2 {
		return errors.New("at least two viewports are required")
	}
	seen := map[string]string{}
	for _, v := range s.Variants {
		if err := uniqueID(seen, "variant", v.ID); err != nil {
			return err
		}
		if !safeConfigID.MatchString(v.ID) {
			return fmt.Errorf("variant id %q must be one safe path segment", v.ID)
		}
		if _, err := os.Stat(v.FrameworkRoot); err != nil {
			return fmt.Errorf("variant %q framework_root: %w", v.ID, err)
		}
	}
	seen = map[string]string{}
	for _, scenario := range s.Scenarios {
		if err := uniqueID(seen, "scenario", scenario.ID); err != nil {
			return err
		}
		if !safeConfigID.MatchString(scenario.ID) {
			return fmt.Errorf("scenario id %q must be one safe path segment", scenario.ID)
		}
		if strings.TrimSpace(scenario.Brief) == "" || len(scenario.Pages) == 0 {
			return fmt.Errorf("scenario %q requires a brief and at least one page", scenario.ID)
		}
		for _, p := range scenario.Pages {
			if p.Name == "" || !strings.HasPrefix(p.Path, "/") || p.ReadySelector == "" {
				return fmt.Errorf("scenario %q has invalid page %+v", scenario.ID, p)
			}
		}
	}
	seen = map[string]string{}
	for _, vp := range s.Viewports {
		if err := uniqueID(seen, "viewport", vp.ID); err != nil {
			return err
		}
		if vp.Width < 320 || vp.Height < 480 {
			return fmt.Errorf("viewport %q is too small: %dx%d", vp.ID, vp.Width, vp.Height)
		}
		if vp.Scheme != "light" && vp.Scheme != "dark" {
			return fmt.Errorf("viewport %q scheme must be light or dark", vp.ID)
		}
	}
	return nil
}

func normalizeAgentConfig(role string, cfg *AgentConfig, defaultTimeout int) error {
	cfg.Backend = strings.ToLower(strings.TrimSpace(cfg.Backend))
	if cfg.Backend == "" {
		cfg.Backend = "codex"
	}
	if cfg.Program == "" {
		cfg.Program = cfg.Backend
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return fmt.Errorf("agents.%s.model must be an explicit model ID (set it in the suite, or pass the role's --*-model flag alongside a backend switch)", role)
	}
	switch cfg.Backend {
	case "codex":
	case "omp":
		if role != "builder" {
			return fmt.Errorf("agents.%s backend omp cannot judge screenshot evidence: the configured GLM-5.2 catalog reports images=no; use codex or claude for judges", role)
		}
		if cfg.Effort == "" {
			cfg.Effort = "xhigh"
		}
	case "claude":
		if cfg.Effort == "" {
			cfg.Effort = "high"
		}
	default:
		return fmt.Errorf("agents.%s.backend %q is unsupported (want codex, omp, or claude)", role, cfg.Backend)
	}
	if cfg.TimeoutMinutes <= 0 {
		cfg.TimeoutMinutes = defaultTimeout
	}
	return nil
}

func uniqueID(seen map[string]string, kind, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%s id is required", kind)
	}
	if prior, ok := seen[id]; ok {
		return fmt.Errorf("duplicate %s id %q (already used by %s)", kind, id, prior)
	}
	seen[id] = kind
	return nil
}
