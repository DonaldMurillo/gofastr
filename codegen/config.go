package codegen

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
)

// Discovery is the result of looking for a codegen configuration file.
type Discovery struct {
	Path       string
	ProjectDir string
	Config     Config
	Found      bool

	// Discovered marks a config found by walking the tree rather than
	// named by the caller. A discovered config's `command` extensions
	// name a binary chosen by whoever wrote that file — a cloned repo, a
	// vendored tree, a teammate's branch — so running them takes an
	// explicit opt-in. See CheckCommandExtensions.
	Discovered bool
}

// DiscoverConfig finds the project codegen config. Dedicated codegen config
// files win; gofastr.yml is only considered when it has a codegen section.
func DiscoverConfig(projectDir string) (Discovery, error) {
	dirs, err := searchDirs(projectDir)
	if err != nil {
		return Discovery{}, err
	}
	for _, dir := range dirs {
		for _, name := range []string{"gofastr.codegen.yml", "gofastr.codegen.yaml"} {
			path := filepath.Join(dir, name)
			if _, err := os.Stat(path); err == nil {
				cfg, err := LoadConfig(path)
				if err != nil {
					return Discovery{}, err
				}
				return Discovery{Path: path, ProjectDir: dir, Config: cfg, Found: true, Discovered: true}, nil
			} else if !os.IsNotExist(err) {
				return Discovery{}, err
			}
		}
	}
	for _, dir := range dirs {
		for _, name := range []string{"gofastr.yml", "gofastr.yaml"} {
			path := filepath.Join(dir, name)
			has, err := HasCodegenSection(path)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return Discovery{}, err
			}
			if !has {
				continue
			}
			cfg, err := LoadConfig(path)
			if err != nil {
				return Discovery{}, err
			}
			return Discovery{Path: path, ProjectDir: dir, Config: cfg, Found: true, Discovered: true}, nil
		}
	}
	return Discovery{}, nil
}

// searchDirs lists the directories config discovery may look in: the
// project directory and each ancestor UP TO AND INCLUDING the module or
// repo root, never past it.
//
// The walk used to run to `/`. A discovered config may declare a
// `command` extension that generate executes, so an unbounded walk meant
// a file planted in any shared ancestor — a workspace parent, /tmp,
// $HOME — ran as the developer on a bare `gofastr generate`, silently.
// The module/repo root is where "the project" ends by every other
// definition the framework uses (framework/isolation resolves the same
// markers), and nothing above it is the project's to configure.
//
// When no marker is found anywhere the walk yields the starting
// directory alone: fail closed, since an unmarked tree gives no evidence
// about how far "the project" extends.
func searchDirs(projectDir string) ([]string, error) {
	start, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for {
		dirs = append(dirs, start)
		if isProjectRoot(start) {
			return dirs, nil
		}
		parent := filepath.Dir(start)
		if parent == start {
			break
		}
		start = parent
	}
	// No marker anywhere up the chain — trust only where we started.
	return dirs[:1], nil
}

// projectRootMarkers are the files that mean "this directory is the top
// of a project".
var projectRootMarkers = []string{"go.work", "go.mod", ".git"}

func isProjectRoot(dir string) bool {
	for _, m := range projectRootMarkers {
		if _, err := os.Stat(filepath.Join(dir, m)); err == nil {
			return true
		}
	}
	return false
}

// ErrCommandExtensionNotAllowed is returned by CheckCommandExtensions
// for a discovered config that declares a command extension without the
// opt-in.
var ErrCommandExtensionNotAllowed = errors.New("codegen: discovered config declares a command extension")

// commandOptInEnv is the explicit opt-in for running command extensions
// from a config nobody named on the command line.
const commandOptInEnv = "GOFASTR_CODEGEN_ALLOW_COMMANDS"

// CheckCommandExtensions refuses to run `command` extensions that came
// from a DISCOVERED config unless the operator opted in.
//
// A config passed with --config is an act of intent; one found by
// walking the tree is not. Callers run this before executing any
// generator and surface the error rather than the command.
func CheckCommandExtensions(d Discovery) error {
	if !d.Found || !d.Discovered {
		return nil
	}
	var named []string
	for _, ext := range d.Config.Codegen.Extensions {
		if len(ext.Command) > 0 {
			named = append(named, ext.Name+": "+strings.Join(ext.Command, " "))
		}
	}
	if len(named) == 0 {
		return nil
	}
	if v := os.Getenv(commandOptInEnv); v != "" {
		if b, err := strconv.ParseBool(v); err == nil && b {
			return nil
		}
	}
	return fmt.Errorf("%w: %s runs\n  %s\nRun it with --config %s, or set %s=1 to allow commands from a discovered config",
		ErrCommandExtensionNotAllowed, d.Path, strings.Join(named, "\n  "), d.Path, commandOptInEnv)
}

// HasCodegenSection reports whether path has a top-level codegen key.
func HasCodegenSection(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	node, err := coreyaml.Parse(string(data))
	if err != nil {
		return false, err
	}
	return node.Kind == coreyaml.Map && node.Map["codegen"] != nil, nil
}

// LoadConfig reads and validates a YAML codegen configuration file.
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	node, err := coreyaml.Parse(string(data))
	if err != nil {
		return Config{}, err
	}
	cfg, err := DecodeConfig(node)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// DecodeConfig decodes a parsed YAML node into Config.
func DecodeConfig(node *coreyaml.Node) (Config, error) {
	root, err := expectMap(node, "config")
	if err != nil {
		return Config{}, err
	}
	version, err := optionalIntValue(root["version"], "version")
	if err != nil {
		return Config{}, err
	}
	cfg := Config{Version: version}
	if root["version"] == nil {
		cfg.Version = 1
	}
	if cfg.Version != 1 {
		return Config{}, fmt.Errorf("config.version %d is not supported", cfg.Version)
	}
	if root["codegen"] == nil {
		return Config{}, fmt.Errorf("codegen section is required")
	}
	cg, err := decodeCodegen(root["codegen"])
	if err != nil {
		return Config{}, err
	}
	cfg.Codegen = cg
	return cfg, nil
}

func decodeCodegen(node *coreyaml.Node) (CodegenConfig, error) {
	m, err := expectMap(node, "codegen")
	if err != nil {
		return CodegenConfig{}, err
	}
	allowed := map[string]bool{"output": true, "clean": true, "generators": true, "extensions": true}
	if err := rejectUnknownKeys(m, allowed, "codegen"); err != nil {
		return CodegenConfig{}, err
	}
	output, err := optionalStringValue(m["output"], "codegen.output")
	if err != nil {
		return CodegenConfig{}, err
	}
	cfg := CodegenConfig{Output: output}
	if cfg.Output == "" {
		cfg.Output = "gen"
	}
	if m["clean"] != nil {
		v, err := requiredBoolValue(m["clean"], "codegen.clean")
		if err != nil {
			return CodegenConfig{}, err
		}
		cfg.Clean = &v
	}
	gens, err := decodeGenerators(m["generators"])
	if err != nil {
		return CodegenConfig{}, err
	}
	cfg.Generators = gens
	exts, err := decodeExtensions(m["extensions"])
	if err != nil {
		return CodegenConfig{}, err
	}
	cfg.Extensions = exts
	if err := validateConfig(cfg, true); err != nil {
		return CodegenConfig{}, err
	}
	return cfg, nil
}

func decodeGenerators(node *coreyaml.Node) ([]GeneratorConfig, error) {
	if node == nil {
		return nil, nil
	}
	list, err := expectList(node, "codegen.generators")
	if err != nil {
		return nil, err
	}
	out := make([]GeneratorConfig, 0, len(list))
	for i, item := range list {
		m, err := expectMap(item, fmt.Sprintf("codegen.generators[%d]", i))
		if err != nil {
			return nil, err
		}
		label := fmt.Sprintf("codegen.generators[%d]", i)
		allowed := map[string]bool{"id": true, "name": true, "extension": true, "source": true, "output": true, "config": true}
		if err := rejectUnknownKeys(m, allowed, label); err != nil {
			return nil, err
		}
		src, err := decodeSource(m["source"], label+".source")
		if err != nil {
			return nil, err
		}
		id, err := optionalStringValue(m["id"], label+".id")
		if err != nil {
			return nil, err
		}
		name, err := optionalStringValue(m["name"], label+".name")
		if err != nil {
			return nil, err
		}
		extension, err := optionalStringValue(m["extension"], label+".extension")
		if err != nil {
			return nil, err
		}
		output, err := optionalStringValue(m["output"], label+".output")
		if err != nil {
			return nil, err
		}
		out = append(out, GeneratorConfig{
			ID:        id,
			Name:      name,
			Extension: extension,
			Source:    src,
			Output:    output,
			Config:    mapValue(m["config"]),
		})
	}
	return out, nil
}

func decodeSource(node *coreyaml.Node, label string) (SourceConfig, error) {
	if node == nil {
		return SourceConfig{}, nil
	}
	m, err := expectMap(node, label)
	if err != nil {
		return SourceConfig{}, err
	}
	if err := rejectUnknownKeys(m, map[string]bool{"type": true, "path": true, "config": true}, label); err != nil {
		return SourceConfig{}, err
	}
	sourceType, err := optionalStringValue(m["type"], label+".type")
	if err != nil {
		return SourceConfig{}, err
	}
	path, err := optionalStringValue(m["path"], label+".path")
	if err != nil {
		return SourceConfig{}, err
	}
	return SourceConfig{
		Type:   sourceType,
		Path:   path,
		Config: mapValue(m["config"]),
	}, nil
}

func decodeExtensions(node *coreyaml.Node) ([]ExtensionConfig, error) {
	if node == nil {
		return nil, nil
	}
	list, err := expectList(node, "codegen.extensions")
	if err != nil {
		return nil, err
	}
	out := make([]ExtensionConfig, 0, len(list))
	for i, item := range list {
		m, err := expectMap(item, fmt.Sprintf("codegen.extensions[%d]", i))
		if err != nil {
			return nil, err
		}
		label := fmt.Sprintf("codegen.extensions[%d]", i)
		if err := rejectUnknownKeys(m, map[string]bool{"name": true, "command": true, "config": true}, label); err != nil {
			return nil, err
		}
		command, err := requiredStringListValue(m["command"], label+".command")
		if err != nil {
			return nil, err
		}
		name, err := optionalStringValue(m["name"], label+".name")
		if err != nil {
			return nil, err
		}
		out = append(out, ExtensionConfig{
			Name:    name,
			Command: command,
			Config:  mapValue(m["config"]),
		})
	}
	return out, nil
}

func normalizeConfig(cfg Config) (Config, error) {
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Version != 1 {
		return Config{}, fmt.Errorf("config.version %d is not supported", cfg.Version)
	}
	if cfg.Codegen.Output == "" {
		cfg.Codegen.Output = "gen"
	}
	if err := validateConfig(cfg.Codegen, false); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validateConfig(cfg CodegenConfig, requireExtensionCommands bool) error {
	if strings.TrimSpace(cfg.Output) == "" {
		return fmt.Errorf("codegen.output must not be empty")
	}
	seenGeneratorIDs := map[string]bool{}
	for i, gen := range cfg.Generators {
		if gen.Name == "" {
			return fmt.Errorf("codegen.generators[%d].name is required", i)
		}
		if gen.ID != "" {
			if seenGeneratorIDs[gen.ID] {
				return fmt.Errorf("duplicate generator id %q", gen.ID)
			}
			seenGeneratorIDs[gen.ID] = true
		}
		if gen.Extension != "" && gen.Source.Type == "" {
			return fmt.Errorf("codegen.generators[%d].source.type is required for extension generator %q", i, gen.Name)
		}
	}
	seenExtensions := map[string]bool{}
	for i, ext := range cfg.Extensions {
		if ext.Name == "" {
			return fmt.Errorf("codegen.extensions[%d].name is required", i)
		}
		if seenExtensions[ext.Name] {
			return fmt.Errorf("duplicate extension %q", ext.Name)
		}
		seenExtensions[ext.Name] = true
		if requireExtensionCommands && len(ext.Command) == 0 {
			return fmt.Errorf("codegen.extensions[%d].command is required", i)
		}
	}
	return nil
}

func expectMap(node *coreyaml.Node, label string) (map[string]*coreyaml.Node, error) {
	if node == nil || node.Kind != coreyaml.Map {
		return nil, fmt.Errorf("%s must be a map", label)
	}
	return node.Map, nil
}

func expectList(node *coreyaml.Node, label string) ([]*coreyaml.Node, error) {
	if node == nil || node.Kind != coreyaml.List {
		return nil, fmt.Errorf("%s must be a list", label)
	}
	return node.List, nil
}

func rejectUnknownKeys(m map[string]*coreyaml.Node, allowed map[string]bool, label string) error {
	for key := range m {
		if allowed[key] || strings.HasPrefix(key, "x_") || strings.HasPrefix(key, "x-") {
			continue
		}
		return fmt.Errorf("unknown key %q in %s", key, label)
	}
	return nil
}

func optionalStringValue(node *coreyaml.Node, label string) (string, error) {
	if node == nil {
		return "", nil
	}
	if node.Kind != coreyaml.Scalar {
		return "", fmt.Errorf("%s must be a string", label)
	}
	value, ok := node.Value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", label)
	}
	return value, nil
}

func requiredBoolValue(node *coreyaml.Node, label string) (bool, error) {
	if node == nil || node.Kind != coreyaml.Scalar {
		return false, fmt.Errorf("%s must be a boolean", label)
	}
	v, ok := node.Value.(bool)
	if !ok {
		return false, fmt.Errorf("%s must be a boolean", label)
	}
	return v, nil
}

func optionalIntValue(node *coreyaml.Node, label string) (int, error) {
	if node == nil {
		return 0, nil
	}
	if node.Kind != coreyaml.Scalar {
		return 0, fmt.Errorf("%s must be an integer", label)
	}
	switch v := node.Value.(type) {
	case int64:
		return int(v), nil
	default:
		return 0, fmt.Errorf("%s must be an integer", label)
	}
}

func requiredStringListValue(node *coreyaml.Node, label string) ([]string, error) {
	if node == nil || node.Kind != coreyaml.List {
		return nil, fmt.Errorf("%s must be a list of strings", label)
	}
	out := make([]string, 0, len(node.List))
	for i, item := range node.List {
		if item.Kind != coreyaml.Scalar {
			return nil, fmt.Errorf("%s[%d] must be a string", label, i)
		}
		value, ok := item.Value.(string)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be a string", label, i)
		}
		out = append(out, value)
	}
	return out, nil
}

func mapValue(node *coreyaml.Node) map[string]any {
	if node == nil || node.Kind != coreyaml.Map {
		return nil
	}
	out := map[string]any{}
	for key, child := range node.Map {
		out[key] = anyValue(child)
	}
	return out
}

func anyValue(node *coreyaml.Node) any {
	if node == nil {
		return nil
	}
	switch node.Kind {
	case coreyaml.Scalar:
		return node.Value
	case coreyaml.List:
		out := make([]any, 0, len(node.List))
		for _, item := range node.List {
			out = append(out, anyValue(item))
		}
		return out
	case coreyaml.Map:
		return mapValue(node)
	default:
		return nil
	}
}
