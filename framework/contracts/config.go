package contracts

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
)

// ConfigFileNames are the files [LoadConfig] looks for, in order. The
// first two are dedicated; the third is the blueprint, which may carry a
// top-level `contracts:` block so a small project needs one file rather
// than two.
var ConfigFileNames = []string{
	"gofastr.contracts.yml",
	"gofastr.contracts.yaml",
	"gofastr.yml",
	"gofastr.yaml",
}

// CoverageConfig governs the testing capability's thresholds. Zero values
// are the strict setting throughout: every demand on, and a line-coverage
// floor that is only enforced once a number is set (there is no honest
// default percentage: a floor nobody chose is a floor nobody meets).
type CoverageConfig struct {
	// Minimum is the line-coverage percentage floor. Zero disables the
	// check; the generated config comments this in at 90.
	Minimum float64
	// MinimumSet distinguishes "not configured" from "configured to 0".
	MinimumSet bool
	// Routes demands every discovered route be exercised by a test.
	Routes bool
	// Permissions demands every declared permission be exercised.
	Permissions bool
	// Entities demands every entity's CRUD surface be exercised.
	Entities bool
	// Profile is the path to the `go test -coverprofile` output, relative
	// to the project root. Empty means the default location.
	Profile string
}

// LayerRule is one named tier of the dependency graph.
type LayerRule struct {
	// Name identifies the layer in diagnostics ("core", "domain", "ui").
	Name string
	// Packages are import-path globs, matched against the *suffix* of an
	// import path so `core/**` matches
	// `github.com/you/app/core/render` without repeating the module path.
	Packages []string
}

// ForbidRule is one banned import edge. `From` and `To` are import-path
// globs matched the same way as LayerRule.Packages.
type ForbidRule struct {
	From   string
	To     string
	Reason string
}

// ArchitectureConfig describes the dependency direction the architecture
// analyzer enforces. Layers are ordered: a package in layer N may import
// its own layer and anything below it, never above. That single rule
// covers "core must not import framework" and "domain must not import UI"
// without listing every pair.
type ArchitectureConfig struct {
	Layers []LayerRule
	Forbid []ForbidRule
}

// Configured reports whether the project described a dependency shape. No
// layers means the analyzer has nothing to enforce and stays quiet.
// Inventing a layering for someone else's package tree would be noise.
func (a ArchitectureConfig) Configured() bool {
	return len(a.Layers) > 0 || len(a.Forbid) > 0
}

// Config is the resolved configuration for a verify run. The zero value
// (via [DefaultConfig]) enforces every rule in the catalog at its declared
// severity: nothing is quieter than declared unless someone writes it
// down. Most fields exist to turn something down; severity may also be
// raised, which is a real choice a team is entitled to make.
type Config struct {
	// Path is the file this came from, "" when defaults.
	Path string
	// Exempt are path globs no analyzer looks at.
	Exempt []string
	// FailOn is the severity floor that makes a run fail. Default
	// SeverityError; `strict: true` lowers it to SeverityWarn.
	FailOn Severity

	Coverage     CoverageConfig
	Architecture ArchitectureConfig

	capSeverity  map[Capability]Severity
	capExempt    map[Capability][]string
	ruleSeverity map[string]Severity
	ruleExempt   map[string][]string
}

// DefaultConfig is strict: nothing exempted, nothing downgraded, every
// coverage demand on.
func DefaultConfig() *Config {
	return &Config{
		FailOn: SeverityError,
		Coverage: CoverageConfig{
			Routes: true, Permissions: true, Entities: true,
		},
		capSeverity:  map[Capability]Severity{},
		capExempt:    map[Capability][]string{},
		ruleSeverity: map[string]Severity{},
		ruleExempt:   map[string][]string{},
	}
}

// LoadConfig resolves configuration for a project root. An explicit path
// must exist; otherwise the first of [ConfigFileNames] present in root is
// used, and defaults apply when none is. A file that exists but is
// malformed is an error: falling back to defaults there would silently
// re-enable rules the author believed they had turned off.
func LoadConfig(root, explicit string) (*Config, error) {
	if explicit != "" {
		body, err := os.ReadFile(explicit)
		if err != nil {
			return nil, fmt.Errorf("read contracts config %q: %w", explicit, err)
		}
		return parseConfig(explicit, body)
	}
	for _, name := range ConfigFileNames {
		path := filepath.Join(root, name)
		body, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		cfg, err := parseConfig(path, body)
		if err != nil {
			return nil, err
		}
		// A gofastr.yml without a `contracts:` block is a blueprint, not a
		// contracts config. Keep looking rather than claiming it.
		if cfg == nil {
			continue
		}
		return cfg, nil
	}
	return DefaultConfig(), nil
}

// parseConfig returns (nil, nil) when the document parses but holds no
// contracts block, the "this is a blueprint, not my config" case.
func parseConfig(path string, body []byte) (*Config, error) {
	doc, err := coreyaml.Parse(string(body))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	root := doc
	if doc != nil && doc.Kind == coreyaml.Map {
		if block, ok := doc.Map["contracts"]; ok {
			root = block
		} else if isBlueprintName(path) {
			return nil, nil
		}
	}
	cfg := DefaultConfig()
	cfg.Path = path
	if root == nil || root.Kind != coreyaml.Map {
		return cfg, nil
	}
	if err := cfg.applyNode(path, root); err != nil {
		return nil, err
	}
	return cfg, nil
}

func isBlueprintName(path string) bool {
	base := filepath.Base(path)
	return base == "gofastr.yml" || base == "gofastr.yaml"
}

func (c *Config) applyNode(path string, root *coreyaml.Node) error {
	for _, key := range sortedKeys(root.Map) {
		node := root.Map[key]
		var err error
		switch key {
		case "exempt", "exclude":
			c.Exempt, err = stringList(path, node)
		case "strict":
			var on bool
			if on, err = boolValue(path, node); err == nil && on {
				// Strict makes warnings fail. It cannot make them pass:
				// there is no setting that raises the floor above error.
				c.FailOn = SeverityWarn
			}
		case "fail-on", "fail_on":
			var s string
			if s, err = stringValue(path, node); err == nil {
				c.FailOn, err = ParseSeverity(s)
				if err == nil && c.FailOn == SeverityOff {
					err = fmt.Errorf("fail-on: off would make every run pass: remove the key or set rules to off individually")
				}
			}
		case "coverage":
			err = c.applyCoverage(path, node)
		case "architecture", "layers":
			err = c.applyArchitecture(path, node)
		case "capabilities":
			err = c.applyCapabilities(path, node)
		case "rules":
			err = c.applyRules(path, node)
		default:
			// A capability name at the top level is the short form:
			//   contracts:
			//     performance: warn
			cap, capErr := ParseCapability(key)
			if capErr != nil {
				return fmt.Errorf("%s:%d: unknown contracts key %q", path, node.Line, key)
			}
			err = c.applyCapability(path, cap, node)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) applyCapabilities(path string, node *coreyaml.Node) error {
	if node.Kind != coreyaml.Map {
		return fmt.Errorf("%s:%d: capabilities must be a map", path, node.Line)
	}
	for _, key := range sortedKeys(node.Map) {
		cap, err := ParseCapability(key)
		if err != nil {
			return fmt.Errorf("%s:%d: %w", path, node.Map[key].Line, err)
		}
		if err := c.applyCapability(path, cap, node.Map[key]); err != nil {
			return err
		}
	}
	return nil
}

// applyCapability accepts three spellings, because all three are things
// people write and rejecting two of them teaches nothing:
//
//	performance: warn                 # severity shorthand
//	performance: false                # off
//	performance: {severity: warn, exempt: [gen/**]}
func (c *Config) applyCapability(path string, cap Capability, node *coreyaml.Node) error {
	switch node.Kind {
	case coreyaml.Scalar:
		sev, err := severityFromScalar(path, node)
		if err != nil {
			return err
		}
		c.capSeverity[cap] = sev
		return nil
	case coreyaml.Map:
		for _, key := range sortedKeys(node.Map) {
			child := node.Map[key]
			switch key {
			case "enabled":
				on, err := boolValue(path, child)
				if err != nil {
					return err
				}
				if !on {
					c.capSeverity[cap] = SeverityOff
				}
			case "severity":
				s, err := stringValue(path, child)
				if err != nil {
					return err
				}
				sev, err := ParseSeverity(s)
				if err != nil {
					return fmt.Errorf("%s:%d: %s: %w", path, child.Line, cap, err)
				}
				c.capSeverity[cap] = sev
			case "exempt", "exclude":
				list, err := stringList(path, child)
				if err != nil {
					return err
				}
				c.capExempt[cap] = list
			default:
				// Capability-scoped settings that belong to one capability
				// only are routed to their own section.
				if cap == CapTesting {
					if err := c.applyCoverageKey(path, key, child); err == nil {
						continue
					}
				}
				return fmt.Errorf("%s:%d: unknown key %q under capability %q", path, child.Line, key, cap)
			}
		}
		return nil
	default:
		return fmt.Errorf("%s:%d: capability %q must be a severity or a map", path, node.Line, cap)
	}
}

func (c *Config) applyRules(path string, node *coreyaml.Node) error {
	if node.Kind != coreyaml.Map {
		return fmt.Errorf("%s:%d: rules must be a map of rule ID to severity", path, node.Line)
	}
	for _, key := range sortedKeys(node.Map) {
		child := node.Map[key]
		rule, ok := LookupRule(key)
		if !ok {
			msg := fmt.Sprintf("%s:%d: unknown rule %q", path, child.Line, key)
			if near := SuggestRules(key); len(near) > 0 {
				msg += ": did you mean " + strings.Join(near, ", ") + "?"
			}
			return fmt.Errorf("%s\nRun `gofastr verify --list` for the full catalog.", msg)
		}
		switch child.Kind {
		case coreyaml.Scalar:
			sev, err := severityFromScalar(path, child)
			if err != nil {
				return err
			}
			c.ruleSeverity[rule.ID] = sev
		case coreyaml.Map:
			for _, sub := range sortedKeys(child.Map) {
				val := child.Map[sub]
				switch sub {
				case "severity":
					s, err := stringValue(path, val)
					if err != nil {
						return err
					}
					sev, err := ParseSeverity(s)
					if err != nil {
						return fmt.Errorf("%s:%d: rule %s: %w", path, val.Line, rule.ID, err)
					}
					c.ruleSeverity[rule.ID] = sev
				case "enabled":
					on, err := boolValue(path, val)
					if err != nil {
						return err
					}
					if !on {
						c.ruleSeverity[rule.ID] = SeverityOff
					}
				case "exempt", "exclude":
					list, err := stringList(path, val)
					if err != nil {
						return err
					}
					c.ruleExempt[rule.ID] = list
				default:
					return fmt.Errorf("%s:%d: unknown key %q under rule %s", path, val.Line, sub, rule.ID)
				}
			}
		default:
			return fmt.Errorf("%s:%d: rule %s must be a severity or a map", path, child.Line, rule.ID)
		}
	}
	return nil
}

func (c *Config) applyCoverage(path string, node *coreyaml.Node) error {
	if node.Kind != coreyaml.Map {
		return fmt.Errorf("%s:%d: coverage must be a map", path, node.Line)
	}
	for _, key := range sortedKeys(node.Map) {
		if err := c.applyCoverageKey(path, key, node.Map[key]); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) applyCoverageKey(path, key string, node *coreyaml.Node) error {
	switch key {
	case "minimum", "min", "line-minimum":
		v, err := floatValue(path, node)
		if err != nil {
			return err
		}
		if v < 0 || v > 100 {
			return fmt.Errorf("%s:%d: coverage minimum %v is not a percentage", path, node.Line, v)
		}
		c.Coverage.Minimum, c.Coverage.MinimumSet = v, true
	case "profile":
		v, err := stringValue(path, node)
		if err != nil {
			return err
		}
		c.Coverage.Profile = v
	case "routes":
		v, err := boolValue(path, node)
		if err != nil {
			return err
		}
		c.Coverage.Routes = v
	case "permissions":
		v, err := boolValue(path, node)
		if err != nil {
			return err
		}
		c.Coverage.Permissions = v
	case "entities":
		v, err := boolValue(path, node)
		if err != nil {
			return err
		}
		c.Coverage.Entities = v
	default:
		return fmt.Errorf("%s:%d: unknown coverage key %q", path, node.Line, key)
	}
	return nil
}

func (c *Config) applyArchitecture(path string, node *coreyaml.Node) error {
	if node.Kind == coreyaml.List {
		layers, err := parseLayers(path, node)
		if err != nil {
			return err
		}
		c.Architecture.Layers = layers
		return nil
	}
	if node.Kind != coreyaml.Map {
		return fmt.Errorf("%s:%d: architecture must be a map", path, node.Line)
	}
	for _, key := range sortedKeys(node.Map) {
		child := node.Map[key]
		switch key {
		case "layers":
			layers, err := parseLayers(path, child)
			if err != nil {
				return err
			}
			c.Architecture.Layers = layers
		case "forbid":
			if child.Kind != coreyaml.List {
				return fmt.Errorf("%s:%d: architecture.forbid must be a list", path, child.Line)
			}
			for _, item := range child.List {
				if item.Kind != coreyaml.Map {
					return fmt.Errorf("%s:%d: each forbid entry must be a map with from/to", path, item.Line)
				}
				var f ForbidRule
				var err error
				if f.From, err = mapString(path, item, "from"); err != nil {
					return err
				}
				if f.To, err = mapString(path, item, "to"); err != nil {
					return err
				}
				f.Reason, _ = mapString(path, item, "reason")
				c.Architecture.Forbid = append(c.Architecture.Forbid, f)
			}
		default:
			return fmt.Errorf("%s:%d: unknown architecture key %q", path, child.Line, key)
		}
	}
	return nil
}

func parseLayers(path string, node *coreyaml.Node) ([]LayerRule, error) {
	if node.Kind != coreyaml.List {
		return nil, fmt.Errorf("%s:%d: layers must be an ordered list (top layer first)", path, node.Line)
	}
	var out []LayerRule
	for _, item := range node.List {
		if item.Kind != coreyaml.Map {
			return nil, fmt.Errorf("%s:%d: each layer needs a name and packages", path, item.Line)
		}
		name, err := mapString(path, item, "name")
		if err != nil {
			return nil, err
		}
		pkgsNode, ok := item.Map["packages"]
		if !ok {
			return nil, fmt.Errorf("%s:%d: layer %q has no packages", path, item.Line, name)
		}
		pkgs, err := stringList(path, pkgsNode)
		if err != nil {
			return nil, err
		}
		out = append(out, LayerRule{Name: name, Packages: pkgs})
	}
	return out, nil
}

// ------------------------------------------------------------------
// Resolution
// ------------------------------------------------------------------

// SeverityFor is the effective severity of a rule after configuration.
// Rule overrides beat capability overrides, which beat the rule's declared
// default: most specific wins.
//
// Configuration may move a severity in either direction. "Strict by
// default" is a statement about the *default*, not a ceiling: a team that
// wants a warning to be an error is making a real, reviewable choice.
func (c *Config) SeverityFor(r Rule) Severity {
	if c == nil {
		return r.Severity
	}
	if s, ok := c.ruleSeverity[r.ID]; ok {
		return s
	}
	if s, ok := c.capSeverity[r.Capability]; ok {
		return s
	}
	return r.Severity
}

// Enabled reports whether a rule can fire at all in this run.
func (c *Config) Enabled(r Rule) bool { return c.SeverityFor(r) != SeverityOff }

// ExemptPath reports whether a path is globally exempt.
func (c *Config) ExemptPath(rel string) bool {
	if c == nil {
		return false
	}
	return matchAnyGlob(c.Exempt, rel)
}

// ExemptFor reports whether a specific rule is exempt at a path, taking
// global, capability, and rule-level exemptions together.
func (c *Config) ExemptFor(r Rule, rel string) bool {
	if c == nil || rel == "" {
		return false
	}
	return matchAnyGlob(c.Exempt, rel) ||
		matchAnyGlob(c.capExempt[r.Capability], rel) ||
		matchAnyGlob(c.ruleExempt[r.ID], rel)
}

// Relaxations lists every severity override and path exemption this
// config applies, so a report states plainly what was changed rather than
// quietly honouring it. The name reflects the common case; an escalation
// is listed too, because the footer's job is "what did config change",
// not "what did it weaken".
func (c *Config) Relaxations() []string {
	if c == nil {
		return nil
	}
	var out []string
	for capName, sev := range c.capSeverity {
		out = append(out, fmt.Sprintf("capability %s → %s", capName, sev))
	}
	for id, sev := range c.ruleSeverity {
		out = append(out, fmt.Sprintf("rule %s → %s", id, sev))
	}
	for _, g := range c.Exempt {
		out = append(out, "exempt path "+g)
	}
	// Scoped exemptions are relaxations too, a path where a rule cannot
	// fire, and omitting them left the footer's account incomplete for
	// exactly the two forms a reader is least likely to know about.
	for capName, globs := range c.capExempt {
		for _, g := range globs {
			out = append(out, fmt.Sprintf("capability %s exempt at %s", capName, g))
		}
	}
	for id, globs := range c.ruleExempt {
		for _, g := range globs {
			out = append(out, fmt.Sprintf("rule %s exempt at %s", id, g))
		}
	}
	sort.Strings(out)
	return out
}

// ------------------------------------------------------------------
// YAML scalar helpers
// ------------------------------------------------------------------

func sortedKeys(m map[string]*coreyaml.Node) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func severityFromScalar(path string, node *coreyaml.Node) (Severity, error) {
	// `false` is a legitimate spelling of off, and the YAML parser hands
	// it back as a bool, not a string.
	if b, ok := node.Value.(bool); ok {
		if b {
			return SeverityError, nil
		}
		return SeverityOff, nil
	}
	s, err := stringValue(path, node)
	if err != nil {
		return SeverityError, err
	}
	sev, err := ParseSeverity(s)
	if err != nil {
		return SeverityError, fmt.Errorf("%s:%d: %w", path, node.Line, err)
	}
	return sev, nil
}

func stringValue(path string, node *coreyaml.Node) (string, error) {
	if node == nil || node.Kind != coreyaml.Scalar {
		line := 0
		if node != nil {
			line = node.Line
		}
		return "", fmt.Errorf("%s:%d: expected a scalar value", path, line)
	}
	switch v := node.Value.(type) {
	case string:
		return v, nil
	case bool:
		return strconv.FormatBool(v), nil
	case int:
		return strconv.Itoa(v), nil
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64), nil
	case nil:
		return "", nil
	default:
		return fmt.Sprint(v), nil
	}
}

func boolValue(path string, node *coreyaml.Node) (bool, error) {
	if node != nil && node.Kind == coreyaml.Scalar {
		if b, ok := node.Value.(bool); ok {
			return b, nil
		}
	}
	s, err := stringValue(path, node)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "yes", "on", "1":
		return true, nil
	case "false", "no", "off", "0":
		return false, nil
	}
	return false, fmt.Errorf("%s:%d: expected true or false, got %q", path, node.Line, s)
}

func floatValue(path string, node *coreyaml.Node) (float64, error) {
	if node != nil && node.Kind == coreyaml.Scalar {
		switch v := node.Value.(type) {
		case int:
			return float64(v), nil
		case float64:
			return v, nil
		}
	}
	s, err := stringValue(path, node)
	if err != nil {
		return 0, err
	}
	f, convErr := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(s), "%"), 64)
	if convErr != nil {
		return 0, fmt.Errorf("%s:%d: expected a number, got %q", path, node.Line, s)
	}
	return f, nil
}

func stringList(path string, node *coreyaml.Node) ([]string, error) {
	if node == nil {
		return nil, nil
	}
	switch node.Kind {
	case coreyaml.Scalar:
		s, err := stringValue(path, node)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(s) == "" {
			return nil, nil
		}
		return []string{s}, nil
	case coreyaml.List:
		out := make([]string, 0, len(node.List))
		for _, item := range node.List {
			s, err := stringValue(path, item)
			if err != nil {
				return nil, err
			}
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s:%d: expected a string or a list of strings", path, node.Line)
	}
}

func mapString(path string, node *coreyaml.Node, key string) (string, error) {
	child, ok := node.Map[key]
	if !ok {
		return "", fmt.Errorf("%s:%d: missing %q", path, node.Line, key)
	}
	return stringValue(path, child)
}
